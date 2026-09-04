package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func (s *server) registerScheduleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sid}/schedules", s.requireAuth(s.handleScheduleList))
	mux.HandleFunc("POST /api/sessions/{sid}/schedules", s.requireAuth(s.handleScheduleCreate))
	mux.HandleFunc("DELETE /api/sessions/{sid}/schedules/{id}", s.requireAuth(s.handleScheduleCancel))
}

func (s *server) handleScheduleList(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	rows, err := s.schedules.List(r.Context(), sess.id, strings.TrimSpace(r.URL.Query().Get("chat")), r.URL.Query().Get("all") == "1")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": rows})
}

func (s *server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var body struct {
		ChatJID string `json:"chatJid"`
		Text    string `json:"text"`
		RunAt   int64  `json:"runAt"`
		Kind    string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u := currentUserFromReq(r)
	owner, tenant := "", ""
	if u != nil {
		owner, tenant = u.ID, u.TenantID()
	}
	// Follow-ups replace any previous pending follow-up for the same chat.
	if body.Kind == "followup" {
		_ = s.schedules.CancelFollowups(r.Context(), sess.id, body.ChatJID)
	}
	row, err := s.schedules.Create(r.Context(), scheduledMessageRow{
		SessionID: sess.id, ChatJID: body.ChatJID, Text: body.Text,
		RunAt: body.RunAt, Kind: body.Kind, OwnerID: owner, TenantID: tenant,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *server) handleScheduleCancel(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	row, err := s.schedules.Get(r.Context(), r.PathValue("id"))
	if err != nil || row.SessionID != sess.id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such schedule"})
		return
	}
	if err := s.schedules.Cancel(r.Context(), row.ID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startScheduleWorker sweeps due rows once a minute and sends them.
func (s *server) startScheduleWorker(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.runDueSchedules(ctx)
			}
		}
	}()
}

func (s *server) runDueSchedules(ctx context.Context) {
	if s.schedules == nil {
		return
	}
	rows, err := s.schedules.Due(ctx, time.Now().UnixMilli())
	if err != nil {
		s.log.Warn("schedule sweep failed", "err", err)
		return
	}
	for _, row := range rows {
		if err := s.sendScheduled(ctx, row); err != nil {
			s.log.Warn("scheduled send failed", "id", row.ID, "err", err)
			_ = s.schedules.MarkFailed(ctx, row.ID, err.Error())
			continue
		}
		_ = s.schedules.MarkSent(ctx, row.ID)
	}
}

func (s *server) sendScheduled(ctx context.Context, row scheduledMessageRow) error {
	sess, ok := s.sessions.Get(row.SessionID)
	if !ok {
		return errors.New("session not available")
	}
	jid, err := parseChatJID(row.ChatJID)
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msgID := ""
	ts := time.Now().UnixMilli()
	sender := ""
	if sess.mode == "cloud" {
		client, _, cerr := s.cloudClientForSession(sendCtx, sess.id)
		if cerr != nil {
			return cerr
		}
		msgID, err = client.SendText(sendCtx, jid.User, row.Text, false)
		if err != nil {
			return err
		}
		if msgID == "" {
			msgID = fmt.Sprintf("cloud-%d", time.Now().UnixNano())
		}
		sender = strings.TrimSpace(sess.cloudPhoneID)
	} else {
		if sess.client == nil || sess.client.Store.ID == nil {
			return errors.New("not paired")
		}
		resp, serr := sess.client.SendMessage(sendCtx, jid, &waE2E.Message{Conversation: proto.String(row.Text)})
		if serr != nil {
			return serr
		}
		msgID = resp.ID
		if !resp.Timestamp.IsZero() {
			ts = resp.Timestamp.UnixMilli()
		}
		sender = jidOrEmpty(sess)
	}
	if sender == "" {
		sender = sess.id
	}
	msg := MessageRow{
		ID: msgID, SessionID: sess.id, ChatJID: jid.String(), SenderJID: sender,
		FromMe: true, Ts: ts, Kind: "text", Body: row.Text,
	}
	if err := s.messages.Insert(ctx, msg); err != nil {
		s.log.Warn("persist scheduled message failed", "err", err)
	}
	s.broker.emitMessage(msg)
	return nil
}
