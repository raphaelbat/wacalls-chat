package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// handleListCalls returns historical call rows across the user's visible
// sessions for the new /history page. Optional filters: sessionId,
// direction (in/out), status (answered/missed), q (peer prefix).
func (s *server) handleListCalls(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	q := r.URL.Query()
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	now := time.Now().UnixMilli()
	if to == 0 {
		to = now
	}
	if from == 0 {
		from = to - int64(90*24*time.Hour/time.Millisecond)
	}
	requested := strings.TrimSpace(q.Get("sessionId"))
	dirFilter := strings.ToLower(strings.TrimSpace(q.Get("direction")))
	statusFilter := strings.ToLower(strings.TrimSpace(q.Get("status")))
	search := strings.ToLower(strings.TrimSpace(q.Get("q")))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 500
	}

	visible := s.sessions.infosFor(u.ID, u.IsSuperAdmin())
	sessionNames := map[string]string{}
	for _, si := range visible {
		sessionNames[si.ID] = si.Name
	}
	if requested != "" {
		if _, ok := sessionNames[requested]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
			return
		}
	}

	type row struct {
		ID          string         `json:"id"`
		SessionID   string         `json:"sessionId"`
		SessionName string         `json:"sessionName,omitempty"`
		Direction   string         `json:"direction"`
		Peer        string         `json:"peer"`
		Name        string         `json:"name,omitempty"`
		Phone       string         `json:"phone,omitempty"`
		AvatarURL   string         `json:"avatarUrl,omitempty"`
		StartedAt   int64          `json:"startedAt"`
		EndedAt     int64          `json:"endedAt"`
		DurationMs  int64          `json:"durationMs"`
		EndReason   string         `json:"endReason,omitempty"`
		Video       bool           `json:"video"`
		Answered    bool           `json:"answered"`
		Recording   *RecordingInfo `json:"recording,omitempty"`
	}
	out := []row{}
	if s.calls != nil {
		for sid, name := range sessionNames {
			if requested != "" && sid != requested {
				continue
			}
			calls, err := s.calls.ListBetween(r.Context(), sid, from, to)
			if err != nil {
				continue
			}
			// Per-session metas cache for contact name / avatar lookups.
			var metas map[string]ChatMeta
			if s.chatMeta != nil {
				metas, _ = s.chatMeta.ListBySession(r.Context(), sid)
			}
			sess, _ := s.sessions.Get(sid)
			for _, c := range calls {
				if dirFilter != "" && !strings.EqualFold(dirFilter, c.Direction) {
					continue
				}
				if statusFilter == "answered" && !c.Answered {
					continue
				}
				if statusFilter == "missed" && c.Answered {
					continue
				}
				contactName, phone, avatar := resolvePeerContact(r.Context(), sess, metas, c.Peer)
				if search != "" {
					hay := strings.ToLower(c.Peer + " " + contactName + " " + phone)
					if !strings.Contains(hay, search) {
						continue
					}
				}
				out = append(out, row{
					ID:          c.ID,
					SessionID:   c.SessionID,
					SessionName: name,
					Direction:   c.Direction,
					Peer:        c.Peer,
					Name:        contactName,
					Phone:       phone,
					AvatarURL:   avatar,
					StartedAt:   c.StartedAt,
					EndedAt:     c.EndedAt,
					DurationMs:  c.DurationMs,
					EndReason:   c.EndReason,
					Video:       c.Video,
					Answered:    c.Answered,
				})
				if info, ok, err := s.calls.RecordingByCall(r.Context(), c.ID); err == nil && ok {
					cp := info
					out[len(out)-1].Recording = &cp
				}
			}
		}
	}
	// Newest first.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].StartedAt < out[j].StartedAt; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}

	// KPIs computed in the same window/filters for the page header cards.
	type kpi struct {
		Total    int   `json:"total"`
		Outbound int   `json:"outbound"`
		Inbound  int   `json:"inbound"`
		Answered int   `json:"answered"`
		Missed   int   `json:"missed"`
		Video    int   `json:"video"`
		TotalMs  int64 `json:"totalDurationMs"`
		AvgMs    int64 `json:"avgDurationMs"`
	}
	k := kpi{}
	for _, c := range out {
		k.Total++
		if strings.EqualFold(c.Direction, "outbound") {
			k.Outbound++
		} else {
			k.Inbound++
		}
		if c.Answered {
			k.Answered++
			k.TotalMs += c.DurationMs
		} else {
			k.Missed++
		}
		if c.Video {
			k.Video++
		}
	}
	if k.Answered > 0 {
		k.AvgMs = k.TotalMs / int64(k.Answered)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from,
		"to":   to,
		"rows": out,
		"kpis": k,
	})
}

// resolvePeerContact enriches a raw call peer JID with the contact display
// name, the real phone number (resolving @lid → PN via the WhatsApp store)
// and the avatar URL persisted in chat meta. Best-effort: any unresolved
// piece is returned as an empty string.
func resolvePeerContact(ctx context.Context, sess *Session, metas map[string]ChatMeta, peer string) (name, phone, avatar string) {
	if peer == "" {
		return
	}
	// 1. Chat meta keyed by the raw JID (covers chats that were already
	// persisted under the LID — common when the contact only ever called).
	if m, ok := metas[peer]; ok {
		name = m.Name
		avatar = m.AvatarURL
	}
	// 2. Resolve LID → PN via whatsmeow's local mapping store. If the peer is
	// already a PN we just keep it.
	parsed, perr := types.ParseJID(peer)
	var pnJID types.JID
	if perr == nil {
		if parsed.Server == types.HiddenUserServer && sess != nil && sess.client != nil && sess.client.Store != nil && sess.client.Store.LIDs != nil {
			if pn, err := sess.client.Store.LIDs.GetPNForLID(ctx, parsed); err == nil && !pn.IsEmpty() {
				pnJID = pn
			}
		} else if parsed.Server != types.HiddenUserServer {
			pnJID = parsed
		}
	}
	if !pnJID.IsEmpty() {
		phone = pnJID.User
		// 3. Backfill from chat meta keyed by the resolved PN JID.
		pnKey := pnJID.String()
		if m, ok := metas[pnKey]; ok {
			if name == "" {
				name = m.Name
			}
			if avatar == "" {
				avatar = m.AvatarURL
			}
		}
	}
	if phone == "" {
		phone = jidPhone(peer)
	}
	return
}
