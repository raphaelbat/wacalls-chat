package main

// Endpoints faltantes: hold da chamada, transferência da chamada e upload de
// "música de espera". O frontend chama estas rotas — se elas não existirem, o
// servidor devolve 404 page not found (que era o erro visto na UI:
// "Falha ao alternar espera", "Erro 404 page not found" ao transferir,
// "upload 404" na música de espera).

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// -------------------- estado em memória --------------------

type holdState struct {
	OnHold  bool  `json:"onHold"`
	SinceMs int64 `json:"sinceMs,omitempty"`
}

var (
	holdMu        sync.Mutex
	holdState_map = map[string]holdState{} // key = sid+"/"+callID
)

func holdKey(sid, callID string) string { return sid + "/" + callID }

func getHold(sid, callID string) holdState {
	holdMu.Lock()
	defer holdMu.Unlock()
	return holdState_map[holdKey(sid, callID)]
}

func setHold(sid, callID string, on bool) holdState {
	holdMu.Lock()
	defer holdMu.Unlock()
	st := holdState_map[holdKey(sid, callID)]
	st.OnHold = on
	if on {
		st.SinceMs = time.Now().UnixMilli()
	} else {
		st.SinceMs = 0
	}
	holdState_map[holdKey(sid, callID)] = st
	return st
}

// -------------------- registro --------------------

func (s *server) registerCallControlRoutes(mux *http.ServeMux) {
	// Hold — aceitamos POST simples (toggle) e as duas variantes com sub-verbo
	// que o frontend pode estar usando, para nunca cair em 404.
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/hold", s.requireAuth(s.handleCallHold))
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/hold/toggle", s.requireAuth(s.handleCallHold))
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/unhold", s.requireAuth(s.handleCallUnhold))
	mux.HandleFunc("GET  /api/sessions/{sid}/calls/{id}/hold", s.requireAuth(s.handleCallHoldGet))

	// Transferência de chamada (para outro atendente ou fila).
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/transfer", s.requireAuth(s.handleCallTransfer))

	// Música de espera — cobrimos as variantes comuns de URL para o mesmo
	// handler, evitando 404 por divergência entre front e back.
	mux.HandleFunc("GET  /api/hold-music", s.requireAuth(s.handleHoldMusicList))
	mux.HandleFunc("POST /api/hold-music", s.requireAuth(s.handleHoldMusicUploadGlobal))
	mux.HandleFunc("POST /api/hold-music/upload", s.requireAuth(s.handleHoldMusicUploadGlobal))
	mux.HandleFunc("PUT  /api/hold-music", s.requireAuth(s.handleHoldMusicSettingsGlobal))
	mux.HandleFunc("PUT  /api/hold-music/settings", s.requireAuth(s.handleHoldMusicSettingsGlobal))
	mux.HandleFunc("DELETE /api/hold-music", s.requireAuth(s.handleHoldMusicDeleteGlobal))
	mux.HandleFunc("GET    /api/hold-music/global", s.requireAuth(s.handleHoldMusicGetGlobal))
	mux.HandleFunc("POST   /api/hold-music/global", s.requireAuth(s.handleHoldMusicUploadGlobal))
	mux.HandleFunc("DELETE /api/hold-music/global", s.requireAuth(s.handleHoldMusicDeleteGlobal))
	mux.HandleFunc("PUT    /api/hold-music/global/config", s.requireAuth(s.handleHoldMusicSettingsGlobal))

	mux.HandleFunc("POST /api/queues/{id}/hold-music", s.requireAuth(s.handleHoldMusicUploadQueue))
	mux.HandleFunc("POST /api/queues/{id}/hold-music/upload", s.requireAuth(s.handleHoldMusicUploadQueue))
	mux.HandleFunc("PUT  /api/queues/{id}/hold-music", s.requireAuth(s.handleHoldMusicSettingsQueue))
	mux.HandleFunc("PUT  /api/queues/{id}/hold-music/settings", s.requireAuth(s.handleHoldMusicSettingsQueue))
	mux.HandleFunc("DELETE /api/queues/{id}/hold-music", s.requireAuth(s.handleHoldMusicDeleteQueue))
	mux.HandleFunc("GET    /api/hold-music/queue/{id}", s.requireAuth(s.handleHoldMusicGetQueue))
	mux.HandleFunc("POST   /api/hold-music/queue/{id}", s.requireAuth(s.handleHoldMusicUploadQueue))
	mux.HandleFunc("DELETE /api/hold-music/queue/{id}", s.requireAuth(s.handleHoldMusicDeleteQueue))
	mux.HandleFunc("PUT    /api/hold-music/queue/{id}/config", s.requireAuth(s.handleHoldMusicSettingsQueue))

	// Aliases usados pelo frontend atual (/api/holdmusic/...).
	mux.HandleFunc("GET    /api/holdmusic/global", s.requireAuth(s.handleHoldMusicGetGlobal))
	mux.HandleFunc("POST   /api/holdmusic/global", s.requireAuth(s.handleHoldMusicUploadGlobal))
	mux.HandleFunc("DELETE /api/holdmusic/global", s.requireAuth(s.handleHoldMusicDeleteGlobal))
	mux.HandleFunc("PUT    /api/holdmusic/global/config", s.requireAuth(s.handleHoldMusicSettingsGlobal))

	mux.HandleFunc("GET    /api/holdmusic/queue/{id}", s.requireAuth(s.handleHoldMusicGetQueue))
	mux.HandleFunc("POST   /api/holdmusic/queue/{id}", s.requireAuth(s.handleHoldMusicUploadQueue))
	mux.HandleFunc("DELETE /api/holdmusic/queue/{id}", s.requireAuth(s.handleHoldMusicDeleteQueue))
	mux.HandleFunc("PUT    /api/holdmusic/queue/{id}/config", s.requireAuth(s.handleHoldMusicSettingsQueue))

	// Preview / streaming do arquivo. Aceita "global" ou "queue_{id}".
	mux.HandleFunc("GET /api/holdmusic/file/{key}", s.handleHoldMusicFile)
	mux.HandleFunc("GET /api/hold-music/file/{key}", s.handleHoldMusicFile)
	mux.HandleFunc("GET /api/media/hold-music/{key}", s.handleHoldMusicFile)
}

// -------------------- hold --------------------

func (s *server) handleCallHold(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	callID := r.PathValue("id")
	var body struct {
		OnHold  *bool  `json:"onHold"`
		Hold    *bool  `json:"hold"`
		On      *bool  `json:"on"`
		QueueID string `json:"queueId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	// Toggle quando nenhum valor foi enviado.
	current := getHold(sess.id, callID)
	target := !current.OnHold
	if body.OnHold != nil {
		target = *body.OnHold
	} else if body.Hold != nil {
		target = *body.Hold
	} else if body.On != nil {
		target = *body.On
	}
	st := setHold(sess.id, callID, target)
	scope := body.QueueID
	if scope == "" {
		scope = "global"
	}
	if target {
		startHoldMusic(sess, callID, scope)
	} else {
		stopHoldMusic(sess.id, callID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "onHold": st.OnHold, "sinceMs": st.SinceMs})
}

func (s *server) handleCallUnhold(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	callID := r.PathValue("id")
	st := setHold(sess.id, callID, false)
	stopHoldMusic(sess.id, callID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "onHold": st.OnHold})
}

func (s *server) handleCallHoldGet(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	st := getHold(sess.id, r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"onHold": st.OnHold, "sinceMs": st.SinceMs})
}

// -------------------- transfer --------------------

func (s *server) handleCallTransfer(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	callID := r.PathValue("id")
	var body struct {
		UserID  string `json:"userId"`
		QueueID string `json:"queueId"`
		AgentID string `json:"agentId"`
		Note    string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.UserID == "" && body.AgentID != "" {
		body.UserID = body.AgentID
	}
	body.UserID = strings.TrimSpace(body.UserID)
	body.QueueID = strings.TrimSpace(body.QueueID)
	if body.UserID == "" && body.QueueID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "informe userId ou queueId"})
		return
	}

	// Descobre a conversa associada à chamada (peer JID) para reaproveitar
	// a mesma lógica de atribuição usada pelo chat.
	var peerJID string
	if sess.reg != nil {
		if ac, ok := sess.reg.get(callID); ok {
			peerJID = ac.peer
		}
	}

	now := time.Now().UnixMilli()
	if peerJID != "" && s.chatMeta != nil {
		status := ChatStatusWaiting
		if body.UserID != "" {
			status = ChatStatusOpen
		}
		if err := s.chatMeta.SetAssignment(r.Context(), sess.id, peerJID, status, body.UserID, body.QueueID, now); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		u := currentUserFromReq(r)
		uid, email := "", ""
		if u != nil {
			uid, email = u.ID, u.Email
		}
		detail := body.UserID
		if body.QueueID != "" {
			if detail != "" {
				detail += " · "
			}
			detail += "queue=" + body.QueueID
		}
		if body.Note != "" {
			detail += " · " + body.Note
		}
		s.logChatEvent(r.Context(), sess.id, peerJID, "transferred", uid, email, detail, now)
		if m, ok, _ := s.chatMeta.Get(r.Context(), sess.id, peerJID); ok {
			s.broker.emitChatMeta(m)
		}
		// Notificação em tempo real para o atendente destino (SSE),
		// para que o toast/badge apareça sem precisar recarregar. Usa o
		// mesmo shape esperado pelo cliente (event-stream.ts).
		if body.UserID != "" && s.broker != nil {
			name := email
			if u != nil && u.Name != "" {
				name = u.Name
			}
			s.broker.broadcast(map[string]any{
				"type":       "call-transfer-request",
				"sessionId":  sess.id,
				"callId":     callID,
				"peer":       peerJID,
				"fromUserId": uid,
				"fromName":   name,
				"targetType": "user",
				"targetId":   body.UserID,
				"note":       body.Note,
				"ts":         now,
			})
		}
	}

	// Coloca a chamada em espera e toca a música até o atendente atender.
	// O destino chama /unhold ao aceitar, encerrando a música.
	setHold(sess.id, callID, true)
	startHoldMusic(sess, callID, body.QueueID)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "userId": body.UserID, "queueId": body.QueueID, "onHold": true})
}

// -------------------- hold music --------------------

const holdMusicDir = "media/hold-music"

type holdMusicMeta struct {
	Scope       string `json:"scope"` // "global" ou queueID
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"contentType,omitempty"`
	Volume      int    `json:"volume"` // legado: percentual 0..200
	FadeInMs    int    `json:"fadeInMs"`
	FadeOutMs   int    `json:"fadeOutMs"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type holdMusicConfig struct {
	Volume    float64 `json:"volume"` // frontend: 0..2
	FadeInMs  int     `json:"fadeInMs"`
	FadeOutMs int     `json:"fadeOutMs"`
}

type holdMusicInfo struct {
	Key         string          `json:"key"`
	Scope       string          `json:"scope"`
	Exists      bool            `json:"exists"`
	SizeBytes   int64           `json:"sizeBytes,omitempty"`
	UpdatedAt   int64           `json:"updatedAt,omitempty"`
	URL         string          `json:"url,omitempty"`
	Filename    string          `json:"filename,omitempty"`
	ContentType string          `json:"contentType,omitempty"`
	Config      holdMusicConfig `json:"config"`
	// Campos legados mantidos para telas antigas.
	Volume    int `json:"volume"`
	FadeInMs  int `json:"fadeInMs"`
	FadeOutMs int `json:"fadeOutMs"`
}

func holdMusicPath(scope string) string {
	return filepath.Join(holdMusicDir, sanitizeHoldMusicScope(scope)+".audio")
}

func holdMusicMetaPath(scope string) string {
	return filepath.Join(holdMusicDir, sanitizeHoldMusicScope(scope)+".json")
}

func sanitizeHoldMusicScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" || scope == "global" {
		return "global"
	}
	scope = strings.TrimPrefix(scope, "queue_")
	repl := strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_", "\x00", "_")
	return repl.Replace(scope)
}

func holdMusicKey(scope string) string {
	scope = sanitizeHoldMusicScope(scope)
	if scope == "global" {
		return "global"
	}
	return "queue_" + scope
}

func readHoldMeta(scope string) holdMusicMeta {
	scope = sanitizeHoldMusicScope(scope)
	m := holdMusicMeta{Scope: scope, Volume: 100, FadeInMs: 800, FadeOutMs: 500}
	if b, err := os.ReadFile(holdMusicMetaPath(scope)); err == nil {
		_ = json.Unmarshal(b, &m)
		if m.Scope == "" {
			m.Scope = scope
		}
	}
	if _, err := os.Stat(holdMusicPath(scope)); err == nil {
		m.URL = "/api/holdmusic/file/" + holdMusicKey(scope)
		if m.Filename == "" {
			m.Filename = holdMusicKey(scope) + ".audio"
		}
	}
	return m
}

func readHoldInfo(scope string) holdMusicInfo {
	scope = sanitizeHoldMusicScope(scope)
	m := readHoldMeta(scope)
	info := holdMusicInfo{
		Key:         holdMusicKey(scope),
		Scope:       scope,
		URL:         m.URL,
		Filename:    m.Filename,
		ContentType: m.ContentType,
		UpdatedAt:   m.UpdatedAt,
		Config: holdMusicConfig{
			Volume:    float64(m.Volume) / 100.0,
			FadeInMs:  m.FadeInMs,
			FadeOutMs: m.FadeOutMs,
		},
		Volume:    m.Volume,
		FadeInMs:  m.FadeInMs,
		FadeOutMs: m.FadeOutMs,
	}
	if st, err := os.Stat(holdMusicPath(scope)); err == nil && !st.IsDir() {
		info.Exists = true
		info.SizeBytes = st.Size()
		if info.UpdatedAt == 0 {
			info.UpdatedAt = st.ModTime().UnixMilli()
		}
		if info.URL == "" {
			info.URL = "/api/holdmusic/file/" + holdMusicKey(scope)
		}
	}
	return info
}

func writeHoldMeta(m holdMusicMeta) error {
	_ = os.MkdirAll(holdMusicDir, 0o755)
	m.Scope = sanitizeHoldMusicScope(m.Scope)
	m.UpdatedAt = time.Now().UnixMilli()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(holdMusicMetaPath(m.Scope), b, 0o644)
}

func (s *server) handleHoldMusicList(w http.ResponseWriter, r *http.Request) {
	out := []holdMusicInfo{readHoldInfo("global")}
	entries, _ := os.ReadDir(holdMusicDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		scope := strings.TrimSuffix(e.Name(), ".json")
		if scope == "global" {
			continue
		}
		out = append(out, readHoldInfo(scope))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *server) handleHoldMusicUploadGlobal(w http.ResponseWriter, r *http.Request) {
	s.doHoldMusicUpload(w, r, "global")
}

func (s *server) handleHoldMusicUploadQueue(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimSpace(r.PathValue("id"))
	if rawID == "" || rawID == "global" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "queue id required"})
		return
	}
	id := sanitizeHoldMusicScope(rawID)
	s.doHoldMusicUpload(w, r, id)
}

func (s *server) doHoldMusicUpload(w http.ResponseWriter, r *http.Request, scope string) {
	_ = os.MkdirAll(holdMusicDir, 0o755)
	// Aceita multipart (campo "file" ou "audio") ou body cru.
	var (
		src         io.Reader
		filename    string
		contentType string
	)
	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart body"})
			return
		}
		for _, field := range []string{"file", "audio", "music", "upload"} {
			if fh, hdr, err := r.FormFile(field); err == nil {
				defer fh.Close()
				src = fh
				if hdr != nil {
					filename = hdr.Filename
					contentType = hdr.Header.Get("Content-Type")
				}
				break
			}
		}
		if src == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file"})
			return
		}
		if filename == "" && r.MultipartForm != nil {
			h := r.MultipartForm
			if files := h.File["file"]; len(files) > 0 {
				filename = files[0].Filename
				contentType = files[0].Header.Get("Content-Type")
			}
		}
	} else {
		src = r.Body
		filename = strings.TrimSpace(r.URL.Query().Get("filename"))
		contentType = r.Header.Get("Content-Type")
	}
	dst, err := os.Create(holdMusicPath(scope))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer dst.Close()
	written, err := io.Copy(dst, src)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if written == 0 {
		_ = os.Remove(holdMusicPath(scope))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "arquivo de áudio vazio"})
		return
	}
	meta := readHoldMeta(scope)
	if filename != "" {
		meta.Filename = filename
	}
	if contentType != "" {
		meta.ContentType = contentType
	} else if meta.ContentType == "" {
		meta.ContentType = "audio/wav"
	}
	_ = writeHoldMeta(meta)
	writeJSON(w, http.StatusOK, readHoldInfo(scope))
}

func (s *server) handleHoldMusicSettingsGlobal(w http.ResponseWriter, r *http.Request) {
	s.doHoldMusicSettings(w, r, "global")
}

func (s *server) handleHoldMusicSettingsQueue(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimSpace(r.PathValue("id"))
	if rawID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "queue id required"})
		return
	}
	id := sanitizeHoldMusicScope(rawID)
	s.doHoldMusicSettings(w, r, id)
}

func (s *server) doHoldMusicSettings(w http.ResponseWriter, r *http.Request, scope string) {
	var body struct {
		Volume    *float64         `json:"volume"`
		FadeInMs  *float64         `json:"fadeInMs"`
		FadeOutMs *float64         `json:"fadeOutMs"`
		Filename  *string          `json:"filename"`
		Config    *holdMusicConfig `json:"config"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	m := readHoldMeta(scope)
	if body.Config != nil {
		if body.Volume == nil {
			body.Volume = &body.Config.Volume
		}
		if body.FadeInMs == nil {
			v := float64(body.Config.FadeInMs)
			body.FadeInMs = &v
		}
		if body.FadeOutMs == nil {
			v := float64(body.Config.FadeOutMs)
			body.FadeOutMs = &v
		}
	}
	if body.Volume != nil {
		v := *body.Volume
		if v <= 2 {
			v *= 100
		}
		m.Volume = clampInt(int(math.Round(v)), 0, 200)
	}
	if body.FadeInMs != nil {
		m.FadeInMs = clampInt(int(math.Round(*body.FadeInMs)), 0, 30000)
	}
	if body.FadeOutMs != nil {
		m.FadeOutMs = clampInt(int(math.Round(*body.FadeOutMs)), 0, 30000)
	}
	if body.Filename != nil {
		m.Filename = *body.Filename
	}
	if err := writeHoldMeta(m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, readHoldInfo(scope))
}

func (s *server) handleHoldMusicDeleteGlobal(w http.ResponseWriter, r *http.Request) {
	s.doHoldMusicDelete(w, r, "global")
}

func (s *server) handleHoldMusicDeleteQueue(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimSpace(r.PathValue("id"))
	if rawID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "queue id required"})
		return
	}
	id := sanitizeHoldMusicScope(rawID)
	s.doHoldMusicDelete(w, r, id)
}

func (s *server) doHoldMusicDelete(w http.ResponseWriter, r *http.Request, scope string) {
	_ = os.Remove(holdMusicPath(scope))
	_ = os.Remove(holdMusicMetaPath(scope))
	w.WriteHeader(http.StatusNoContent)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// evita "imported and not used" quando strconv não é necessário; alguns
// helpers acima podem ser trocados sem quebrar o build.
var _ = strconv.Itoa

// -------------------- handlers extras (/api/holdmusic) --------------------

func (s *server) handleHoldMusicGetGlobal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, readHoldInfo("global"))
}

func (s *server) handleHoldMusicGetQueue(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimSpace(r.PathValue("id"))
	if rawID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "queue id required"})
		return
	}
	id := sanitizeHoldMusicScope(rawID)
	writeJSON(w, http.StatusOK, readHoldInfo(id))
}

// handleHoldMusicFile transmite o arquivo salvo. A key aceita:
//   - "global"           → escopo global
//   - "queue_{id}"       → escopo da fila {id}
//   - "{id}"             → também aceito para compatibilidade
//   - opcional sufixo ".audio" / ".wav" é ignorado
func (s *server) handleHoldMusicFile(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	key = strings.TrimSuffix(key, ".audio")
	key = strings.TrimSuffix(key, ".wav")
	if key == "" {
		http.NotFound(w, r)
		return
	}
	scope := key
	if strings.HasPrefix(key, "queue_") {
		scope = strings.TrimPrefix(key, "queue_")
	}
	scope = sanitizeHoldMusicScope(scope)
	path := holdMusicPath(scope)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	meta := readHoldMeta(scope)
	if meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	} else {
		w.Header().Set("Content-Type", "audio/wav")
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}
