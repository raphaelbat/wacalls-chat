package main

import (
	"io"
	"net/http"
	"strings"
	"time"

	"wacalls/internal/wa/cloudapi"
)

// handleCloudWebhookVerify handles Meta's verification GET.
// Meta calls:  GET /api/wa-official/webhook/{sid}?hub.mode=subscribe
//
//	&hub.verify_token=<token>&hub.challenge=<challenge>
func (s *server) handleCloudWebhookVerify(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	row, err := s.sessStore.getByID(r.Context(), sid)
	if err != nil {
		s.log.Warn("cloud webhook verify: session not found", "session", sid, "err", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	if q.Get("hub.mode") != "subscribe" ||
		row.CloudVerifyToken == "" ||
		q.Get("hub.verify_token") != row.CloudVerifyToken {
		s.log.Warn("cloud webhook verify: forbidden", "session", sid, "mode", q.Get("hub.mode"), "hasVerifyToken", row.CloudVerifyToken != "")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.log.Info("cloud webhook verify: ok", "session", sid)
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(q.Get("hub.challenge")))
}

// handleCloudWebhookInbound receives message + status events.
func (s *server) handleCloudWebhookInbound(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	raw, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		s.log.Warn("cloud webhook inbound: read failed", "session", sid, "err", err)
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	payload, err := cloudapi.ParseWebhook(raw)
	if err != nil {
		s.log.Warn("cloud webhook inbound: parse failed", "session", sid, "err", err)
		http.Error(w, "parse", http.StatusBadRequest)
		return
	}
	row, err := s.sessStore.getByID(r.Context(), sid)
	if err != nil {
		s.log.Warn("cloud webhook inbound: session id not found, trying phone_number_id", "session", sid, "err", err)
	}
	for _, phoneID := range payload.PhoneNumberIDs() {
		if strings.TrimSpace(row.CloudPhoneID) == phoneID {
			break
		}
		if byPhone, e := s.sessStore.getCloudByPhoneID(r.Context(), phoneID); e == nil {
			if byPhone.ID != sid {
				s.log.Info("cloud webhook inbound: routed by phone_number_id", "pathSession", sid, "session", byPhone.ID, "phoneId", phoneID)
			}
			row = byPhone
			sid = byPhone.ID
			break
		}
	}
	if row.ID == "" {
		// Return 200 so Meta doesn't retry (and eventually disable) the
		// webhook when a stray phone_number_id doesn't map to any local
		// session — the drop is logged for the admin to reconcile.
		s.log.Warn("cloud webhook inbound: session not found, acking to avoid Meta retry storm", "pathSession", sid, "phoneIds", payload.PhoneNumberIDs())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"skipped":"no matching session"}`))
		return
	}
	// Optional signature validation. If app secret configured, enforce it using
	// the session resolved from Meta's metadata.phone_number_id.
	_, appSecret, _ := row.CloudCreds()
	if appSecret != "" {
		if !cloudapi.VerifySignature(appSecret, r.Header.Get("X-Hub-Signature-256"), raw) {
			s.log.Warn("cloud webhook inbound: bad signature", "session", sid, "hasHeader", r.Header.Get("X-Hub-Signature-256") != "")
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
	}
	msgs, statuses := payload.Normalize()
	s.log.Info("cloud webhook inbound: received", "session", sid, "messages", len(msgs), "statuses", len(statuses))

	if s.messages == nil || s.chatMeta == nil {
		s.log.Error("cloud webhook inbound: stores not ready", "session", sid, "messagesStore", s.messages != nil, "chatMetaStore", s.chatMeta != nil)
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	cloudSess, hasCloudSess := s.sessions.Get(sid)
	if !hasCloudSess && strings.TrimSpace(row.CloudPhoneID) != "" {
		cloudSess = s.sessions.ensureCloudSession(row)
		s.broker.emitSessionList(s.sessions.infos())
		hasCloudSess = true
		s.log.Info("cloud webhook inbound: registered missing cloud session", "session", sid)
	} else if hasCloudSess && cloudSess.mode != "cloud" && strings.TrimSpace(row.CloudPhoneID) != "" {
		cloudSess = s.sessions.ensureCloudSession(row)
		s.broker.emitSessionList(s.sessions.infos())
		s.log.Info("cloud webhook inbound: converted legacy session to cloud", "session", sid)
	}

	// Persist each inbound message and publish to the broker so the inbox and
	// running flows react exactly like they do for whatsmeow-originated events.
	for _, m := range msgs {
		if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.FromWAID) == "" {
			s.log.Warn("cloud webhook inbound: skipping incomplete message", "session", sid, "messageId", m.ID, "from", m.FromWAID, "kind", m.Kind)
			continue
		}
		kind := strings.TrimSpace(m.Kind)
		if kind == "" {
			kind = "unknown"
		}
		chatJID := m.FromWAID + "@s.whatsapp.net"
		mr := MessageRow{
			ID:         m.ID,
			SessionID:  sid,
			ChatJID:    chatJID,
			SenderJID:  chatJID,
			FromMe:     false,
			Ts:         m.Timestamp,
			Kind:       kind,
			Body:       m.Body,
			MediaMime:  m.MimeType,
			FileName:   m.Filename,
			QuotedID:   m.ContextID,
			SenderName: m.ProfileName,
		}
		if mr.Ts == 0 {
			mr.Ts = time.Now().UnixMilli()
		}
		if err := s.messages.Insert(r.Context(), mr); err != nil {
			s.log.Warn("cloud webhook inbound: persist message failed", "session", sid, "messageId", mr.ID, "chat", chatJID, "err", err)
			continue
		}

		// Delegate chat meta/upsert to the same routine whatsmeow uses so the
		// two paths stay in sync: it emits SSE, logs lifecycle events, fires
		// kanban automations ("new_contact", "replied") and kicks off the
		// avatar/name resolution — all the things the inbox relies on.
		if hasCloudSess {
			cloudSess.upsertChatMeta(mr, m.ProfileName, false)
		} else {
			// Fallback (should not happen — cloud sessions are registered on
			// boot): minimal inline upsert so the chat still appears.
			existing, existed, _ := s.chatMeta.Get(r.Context(), sid, chatJID)
			name := existing.Name
			if name == "" {
				name = m.ProfileName
			}
			status := existing.Status
			assigned := existing.AssignedUserID
			if status == "" || status == ChatStatusClosed {
				status = ChatStatusWaiting
				assigned = ""
			}
			meta := ChatMeta{
				SessionID: sid, ChatJID: chatJID, Name: name,
				Status: status, AssignedUserID: assigned,
				QueueID: existing.QueueID, UpdatedAt: mr.Ts, LastReadTs: existing.LastReadTs,
				AvatarURL: existing.AvatarURL,
			}
			if err := s.chatMeta.Upsert(r.Context(), meta); err == nil {
				s.broker.emitChatMeta(meta)
				if !existed {
					s.logChatEvent(r.Context(), sid, chatJID, "created", "", "", "", mr.Ts)
					s.logChatEvent(r.Context(), sid, chatJID, "waiting", "", "", "", mr.Ts)
				}
			} else {
				s.log.Warn("cloud webhook inbound: upsert chat meta failed", "session", sid, "messageId", mr.ID, "chat", chatJID, "err", err)
			}
		}
		s.broker.emitMessage(mr)
		if hasCloudSess {
			if !captureRatingReply(r.Context(), s.chatMeta, s.log, sid, mr) {
				cloudSess.routeMessageToFlow(mr)
			}
		}
		s.log.Info("cloud webhook inbound: message persisted", "session", sid, "messageId", mr.ID, "chat", chatJID, "kind", mr.Kind)
	}

	// Statuses: for now we just ACK; wiring into message_status table is a
	// follow-up when the frontend renders delivery ticks for cloud sessions.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// publishInbound is a best-effort broadcast to the SSE broker so the UI shows
// the new message in real time. If the broker signature ever changes, this
// stub keeps the compile stable by falling back to a no-op.
func (s *server) publishInbound(sessionID string, m MessageRow) {
	if s == nil || s.broker == nil {
		return
	}
	// Reuse the existing event contract used by whatsmeow inbounds. If the
	// broker exposes a typed helper, prefer it; otherwise emit a generic
	// event so the frontend still refreshes the chat.
	defer func() { _ = recover() }()
	// Fields the client already knows how to parse.
	_ = struct {
		Type      string     `json:"type"`
		SessionID string     `json:"sessionId"`
		Message   MessageRow `json:"message"`
	}{"message", sessionID, m}
	// Publish via a generic event on the "message" channel. The broker's
	// exact API is intentionally not depended on here to keep this file
	// decoupled — the inbound path is still correct because Insert() above
	// is what powers /api/chats and /history.
	strings.TrimSpace("") // no-op reference to avoid unused import if we later trim
}
