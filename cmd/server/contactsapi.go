package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// contactsListCache stores the fully aggregated, unfiltered contact list per
// user for a short window. The page does heavy work (ListChats + meta + unread
// for every session), so rapid clicks on the Contatos tab — or the user's own
// reload-spam — used to fan out into N concurrent full scans and freeze the
// browser. A 2-second TTL collapses bursts to a single scan while keeping
// data fresh enough for an operator workflow.
type contactsCacheEntry struct {
	at   time.Time
	rows []contactRow
}

var (
	contactsCacheMu  sync.Mutex
	contactsCache    = map[string]contactsCacheEntry{}
	contactsInFlight = map[string]*sync.WaitGroup{}
)

const contactsCacheTTL = 2 * time.Second

// contactRow is the per-contact row returned by GET /api/contacts. Contacts are
// derived from the union of every chat seen across sessions visible to the
// authenticated user (admins see all). Phone is best-effort extracted from the
// JID (digits before "@").
type contactRow struct {
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName"`
	ChatJID     string `json:"chatJid"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	IsGroup     bool   `json:"isGroup"`
	LastTs      int64  `json:"lastTs"`
	LastMessage string `json:"lastMessage,omitempty"`
	Unread      int    `json:"unread"`
}

// jidPhone extracts the dial-able number from a WhatsApp JID. For groups,
// channels and broadcast lists returns an empty string.
func jidPhone(jid string) string {
	if jid == "" {
		return ""
	}
	at := strings.IndexByte(jid, '@')
	if at <= 0 {
		return ""
	}
	suffix := jid[at+1:]
	if suffix == "g.us" || suffix == "newsletter" || suffix == "broadcast" {
		return ""
	}
	user := jid[:at]
	if i := strings.IndexAny(user, ":."); i > 0 {
		user = user[:i]
	}
	// Keep only digits — protects against @lid suffixes that already carry a digit-only user part.
	b := strings.Builder{}
	for _, r := range user {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *server) registerContactsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/contacts", s.requireAuth(s.handleContactsList))
	mux.HandleFunc("POST /api/contacts", s.requireAuth(s.handleContactCreate))
	mux.HandleFunc("PATCH /api/contacts/{sid}/{jid}", s.requireAuth(s.handleContactUpdate))
	mux.HandleFunc("DELETE /api/contacts/{sid}/{jid}", s.requireAuth(s.handleContactDelete))
}

func (s *server) handleContactsList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	kind := r.URL.Query().Get("kind") // "" | "user" | "group"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	rows := s.loadContactRowsCached(r.Context(), u)

	// Filtering and pagination happen on the cached slice so the
	// expensive per-session aggregation runs at most once per TTL.
	filtered := rows
	if kind != "" || q != "" {
		filtered = make([]contactRow, 0, len(rows))
		for _, row := range rows {
			if kind == "user" && row.IsGroup {
				continue
			}
			if kind == "group" && !row.IsGroup {
				continue
			}
			if q != "" {
				hay := strings.ToLower(row.Name + " " + row.Phone + " " + row.ChatJID)
				if !strings.Contains(hay, q) {
					continue
				}
			}
			filtered = append(filtered, row)
		}
	}
	rows = filtered

	total := len(rows)
	end := offset + limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	page := rows[offset:end]

	writeJSON(w, http.StatusOK, map[string]any{
		"contacts": page,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// loadContactRowsCached returns the full, unfiltered contact list for the
// user, coalescing concurrent requests and serving a 2s cached copy on hits.
// Aggregates per-session work in parallel so a user with many connections no
// longer waits for each session sequentially.
func (s *server) loadContactRowsCached(ctx context.Context, u *currentUser) []contactRow {
	key := fmt.Sprintf("%s|%t", u.ID, u.IsSuperAdmin())

	for {
		contactsCacheMu.Lock()
		if e, ok := contactsCache[key]; ok && time.Since(e.at) < contactsCacheTTL {
			rows := e.rows
			contactsCacheMu.Unlock()
			return rows
		}
		if wg, ok := contactsInFlight[key]; ok {
			contactsCacheMu.Unlock()
			wg.Wait()
			continue
		}
		wg := &sync.WaitGroup{}
		wg.Add(1)
		contactsInFlight[key] = wg
		contactsCacheMu.Unlock()

		rows := s.collectContactRows(ctx, u)

		contactsCacheMu.Lock()
		contactsCache[key] = contactsCacheEntry{at: time.Now(), rows: rows}
		delete(contactsInFlight, key)
		contactsCacheMu.Unlock()
		wg.Done()
		return rows
	}
}

func (s *server) collectContactRows(ctx context.Context, u *currentUser) []contactRow {
	sessions := s.sessions.infosFor(u.ID, u.IsSuperAdmin())
	type bucket struct{ rows []contactRow }
	out := make([]bucket, len(sessions))
	var wg sync.WaitGroup
	for i := range sessions {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sinfo := sessions[i]
			chats, err := s.messages.ListChats(ctx, sinfo.ID)
			if err != nil {
				return
			}
			metas := map[string]ChatMeta{}
			if s.chatMeta != nil {
				metas, _ = s.chatMeta.ListBySession(ctx, sinfo.ID)
			}
			unread, _ := s.messages.UnreadCounts(ctx, sinfo.ID)
			local := make([]contactRow, 0, len(chats))
			for _, c := range chats {
				meta := metas[c.ChatJID]
				name := meta.Name
				if name == "" {
					name = c.Name
				}
				isGroup := meta.IsGroup || isGroupChatJID(c.ChatJID)
				phone := jidPhone(c.ChatJID)
				if name == "" {
					name = phone
					if name == "" {
						name = c.ChatJID
					}
				}
				local = append(local, contactRow{
					SessionID:   sinfo.ID,
					SessionName: sinfo.Name,
					ChatJID:     c.ChatJID,
					Name:        name,
					Phone:       phone,
					AvatarURL:   meta.AvatarURL,
					IsGroup:     isGroup,
					LastTs:      c.LastTs,
					LastMessage: c.LastMessage,
					Unread:      unread[c.ChatJID],
				})
			}
			out[i] = bucket{rows: local}
		}(i)
	}
	wg.Wait()
	total := 0
	for _, b := range out {
		total += len(b.rows)
	}
	rows := make([]contactRow, 0, total)
	for _, b := range out {
		rows = append(rows, b.rows...)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].LastTs == rows[j].LastTs {
			return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
		}
		return rows[i].LastTs > rows[j].LastTs
	})
	return rows
}

// userCanAccessSession returns true when the authenticated user owns the
// session or is an admin. Used as the authorization gate for every
// per-contact mutation.
func (s *server) userCanAccessSession(u *currentUser, sessionID string) bool {
	if u == nil || sessionID == "" {
		return false
	}
	for _, si := range s.sessions.infosFor(u.ID, u.IsSuperAdmin()) {
		if si.ID == sessionID {
			return true
		}
	}
	return false
}

// digitsOnly keeps only ASCII digits — used to normalize a user-supplied
// phone number into the WhatsApp JID local part.
func digitsOnly(s string) string {
	b := strings.Builder{}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// saveContactAvatar writes an uploaded image to media/avatars/contact-<hash>.<ext>
// and returns the public URL that the frontend can render. The hash is derived
// from the JID so the same contact replaces its previous file cleanly.
func saveContactAvatar(sessionID, jid string, file io.Reader, contentType string, size int64) (string, error) {
	if size > 4*1024*1024 {
		return "", fmt.Errorf("avatar too large (max 4MB)")
	}
	ext := ".png"
	switch {
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		ext = ".jpg"
	case strings.Contains(contentType, "webp"):
		ext = ".webp"
	case strings.Contains(contentType, "gif"):
		ext = ".gif"
	case strings.Contains(contentType, "png"):
		ext = ".png"
	default:
		return "", fmt.Errorf("unsupported image type")
	}
	dir := filepath.Join("media", "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	h := sha1.Sum([]byte(sessionID + "|" + jid))
	hash := hex.EncodeToString(h[:8])
	// Remove previous file with a different extension to avoid stale leftovers.
	for _, e := range []string{".png", ".jpg", ".webp", ".gif"} {
		_ = os.Remove(filepath.Join(dir, "contact-"+hash+e))
	}
	name := "contact-" + hash + ext
	full := filepath.Join(dir, name)
	dst, err := os.Create(full)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		_ = os.Remove(full)
		return "", err
	}
	dst.Close()
	return fmt.Sprintf("/api/media/avatars/%s?v=%d", name, time.Now().Unix()), nil
}

// contactPayload is the JSON shape accepted by POST/PATCH when no avatar file
// is attached. The multipart form variants accept the same fields plus a
// `file` part for the avatar image.
type contactPayload struct {
	SessionID string `json:"sessionId"`
	Phone     string `json:"phone"`
	Name      string `json:"name"`
}

// handleContactCreate adds a new contact (person only). Creates an empty
// chat row so it shows up in /contacts and is immediately reachable from the
// chat view via deep link.
func (s *server) handleContactCreate(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// Parse multipart (preferred — supports avatar upload) or JSON.
	ct := r.Header.Get("Content-Type")
	var (
		sessionID, phone, name string
		avatarURL              string
	)
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload"})
			return
		}
		sessionID = strings.TrimSpace(r.FormValue("sessionId"))
		phone = strings.TrimSpace(r.FormValue("phone"))
		name = strings.TrimSpace(r.FormValue("name"))
	} else {
		var p contactPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		sessionID, phone, name = strings.TrimSpace(p.SessionID), strings.TrimSpace(p.Phone), strings.TrimSpace(p.Name)
	}
	digits := digitsOnly(phone)
	if sessionID == "" || digits == "" || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId, phone (somente dígitos) e name são obrigatórios"})
		return
	}
	if len(digits) < 8 || len(digits) > 15 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "telefone inválido"})
		return
	}
	if len(name) > 120 {
		name = name[:120]
	}
	if !s.userCanAccessSession(u, sessionID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "sem acesso à conexão"})
		return
	}
	jid := digits + "@s.whatsapp.net"
	// Canonicalize the JID by asking WhatsApp who actually owns this phone
	// number. This handles Brazilian 9-digit prefix normalization and avoids
	// creating chats with a JID that doesn't match incoming messages.
	if sess, ok := s.sessions.Get(sessionID); ok && sess != nil && sess.client != nil && sess.client.IsConnected() {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		if rows, err := sess.client.IsOnWhatsApp(ctx, []string{"+" + digits}); err == nil {
			for _, row := range rows {
				if row.IsIn && !row.JID.IsEmpty() && row.JID.User != "" {
					jid = row.JID.User + "@s.whatsapp.net"
					digits = row.JID.User
					break
				}
			}
		}
		cancel()
	}
	now := time.Now().UnixMilli()

	// Optional avatar (multipart only).
	if r.MultipartForm != nil {
		if file, header, err := r.FormFile("file"); err == nil {
			defer file.Close()
			url, sErr := saveContactAvatar(sessionID, jid, file, header.Header.Get("Content-Type"), header.Size)
			if sErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": sErr.Error()})
				return
			}
			avatarURL = url
		}
	}

	if s.chatMeta == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "chat store indisponível"})
		return
	}
	meta := ChatMeta{
		SessionID: sessionID,
		ChatJID:   jid,
		Name:      name,
		IsGroup:   false,
		Status:    ChatStatusWaiting,
		UpdatedAt: now,
		AvatarURL: avatarURL,
	}
	if err := s.chatMeta.Upsert(r.Context(), meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"sessionId": sessionID,
		"chatJid":   jid,
		"name":      name,
		"phone":     digits,
		"avatarUrl": avatarURL,
	})
}

// handleContactUpdate edits name and/or avatar of an existing contact.
func (s *server) handleContactUpdate(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID := r.PathValue("sid")
	jid := r.PathValue("jid")
	if !s.userCanAccessSession(u, sessionID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "sem acesso à conexão"})
		return
	}
	if s.chatMeta == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "chat store indisponível"})
		return
	}
	var (
		name      string
		hasName   bool
		clearAvi  bool
		avatarURL string
	)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload"})
			return
		}
		if v := r.FormValue("name"); v != "" {
			name = strings.TrimSpace(v)
			hasName = true
		}
		clearAvi = r.FormValue("clearAvatar") == "1"
		if file, header, err := r.FormFile("file"); err == nil {
			defer file.Close()
			url, sErr := saveContactAvatar(sessionID, jid, file, header.Header.Get("Content-Type"), header.Size)
			if sErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": sErr.Error()})
				return
			}
			avatarURL = url
		}
	} else {
		var body struct {
			Name        *string `json:"name"`
			ClearAvatar bool    `json:"clearAvatar"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Name != nil {
			name = strings.TrimSpace(*body.Name)
			hasName = true
		}
		clearAvi = body.ClearAvatar
	}
	now := time.Now().UnixMilli()
	if hasName {
		if name == "" || len(name) > 120 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nome inválido"})
			return
		}
		if err := s.chatMeta.SetName(r.Context(), sessionID, jid, name, now); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if clearAvi {
		if err := s.chatMeta.SetAvatar(r.Context(), sessionID, jid, "", now); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else if avatarURL != "" {
		if err := s.chatMeta.SetAvatar(r.Context(), sessionID, jid, avatarURL, now); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "avatarUrl": avatarURL})
}

// handleContactDelete removes the contact: chat meta + audit + every stored
// message for the conversation. Irreversible.
func (s *server) handleContactDelete(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID := r.PathValue("sid")
	jid := r.PathValue("jid")
	if !s.userCanAccessSession(u, sessionID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "sem acesso à conexão"})
		return
	}
	if s.messages != nil {
		if err := s.messages.DeleteChat(r.Context(), sessionID, jid); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if s.chatMeta != nil {
		if err := s.chatMeta.Delete(r.Context(), sessionID, jid); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
