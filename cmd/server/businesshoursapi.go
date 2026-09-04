package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *server) registerBusinessHoursRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/business-hours", s.requireAuth(s.handleBusinessHoursGet))
	mux.HandleFunc("PUT /api/business-hours", s.requireAdmin(s.handleBusinessHoursSave))
}

func (s *server) handleBusinessHoursGet(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}
	cfg, err := s.businessHours.Get(r.Context(), scope, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *server) handleBusinessHoursSave(w http.ResponseWriter, r *http.Request) {
	var body businessHoursConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(body.ScopeID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing scopeId"})
		return
	}
	cfg, err := s.businessHours.Save(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// resolveBusinessHours picks the queue grid when the queue has its own
// enabled configuration, otherwise falls back to the instance grid.
func (s *server) resolveBusinessHours(ctx context.Context, sessionID, queueID string) (businessHoursConfig, bool) {
	if s.businessHours == nil {
		return businessHoursConfig{}, false
	}
	if strings.TrimSpace(queueID) != "" {
		if cfg, err := s.businessHours.Get(ctx, "queue", queueID); err == nil && cfg.Enabled {
			return cfg, true
		}
	}
	cfg, err := s.businessHours.Get(ctx, "session", sessionID)
	if err != nil || !cfg.Enabled {
		return businessHoursConfig{}, false
	}
	return cfg, true
}

// maybeSendOutOfHours replies with the configured out-of-hours message the
// first time a customer writes inside a closed window.
func (s *server) maybeSendOutOfHours(ctx context.Context, sessionID, chatJID string) {
	if s.businessHours == nil || isGroupChatJID(chatJID) {
		return
	}
	queueID := ""
	if s.chatMeta != nil {
		if meta, found, err := s.chatMeta.Get(ctx, sessionID, chatJID); err == nil && found {
			queueID = meta.QueueID
		}
	}
	cfg, ok := s.resolveBusinessHours(ctx, sessionID, queueID)
	if !ok {
		return
	}
	open, windowKey := cfg.isOpenAt(time.Now())
	if open {
		return
	}
	text := strings.TrimSpace(cfg.Message)
	if text == "" {
		if sess, found := s.sessions.Get(sessionID); found {
			sess.mu.Lock()
			text = strings.TrimSpace(sess.outOfHoursMessage)
			sess.mu.Unlock()
		}
	}
	if text == "" {
		return
	}
	send, err := s.businessHours.ShouldNotify(ctx, sessionID, chatJID, windowKey)
	if err != nil || !send {
		return
	}
	if err := s.sendScheduled(ctx, scheduledMessageRow{SessionID: sessionID, ChatJID: chatJID, Text: text}); err != nil {
		s.log.Warn("out-of-hours reply failed", "session", sessionID, "chat", chatJID, "err", err)
	}
}

var _ = context.Background
