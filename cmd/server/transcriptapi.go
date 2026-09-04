package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
)

func (s *server) registerTranscriptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/transcripts", s.requireAuth(s.handleTranscriptList))
	mux.HandleFunc("GET /api/transcripts/search", s.requireAuth(s.handleTranscriptSearch))
	mux.HandleFunc("POST /api/transcripts", s.requireAuth(s.handleTranscriptCreate))
	mux.HandleFunc("DELETE /api/transcripts/{scope}/{refId}", s.requireAuth(s.handleTranscriptDelete))
}

// handleTranscriptList returns transcripts for a chat (scope=message and any
// call recordings linked to it) or every recording transcript of a session.
func (s *server) handleTranscriptList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sessionID := strings.TrimSpace(q.Get("sessionId"))
	chatJID := strings.TrimSpace(q.Get("chatJid"))
	scope := strings.TrimSpace(q.Get("scope"))

	var (
		rows []transcriptRow
		err  error
	)
	switch {
	case chatJID != "":
		rows, err = s.transcripts.ListByChat(r.Context(), sessionID, chatJID)
	case scope != "":
		rows, err = s.transcripts.ListByScope(r.Context(), sessionID, scope, 0)
	default:
		rows, err = s.transcripts.ListByScope(r.Context(), sessionID, "recording", 0)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"transcripts": rows,
		"enabled":     s.stt.Enabled(),
	})
}

func (s *server) handleTranscriptSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.transcripts.Search(r.Context(),
		strings.TrimSpace(q.Get("sessionId")),
		strings.TrimSpace(q.Get("chatJid")),
		q.Get("q"), 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transcripts": rows})
}

type transcriptCreateBody struct {
	SessionID string `json:"sessionId"`
	Scope     string `json:"scope"`
	RefID     string `json:"refId"`
	ChatJID   string `json:"chatJid"`
	Force     bool   `json:"force"`
}

func (s *server) handleTranscriptCreate(w http.ResponseWriter, r *http.Request) {
	var body transcriptCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.Scope = strings.TrimSpace(strings.ToLower(body.Scope))
	body.RefID = strings.TrimSpace(body.RefID)
	if body.Scope != "message" && body.Scope != "recording" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope inválido"})
		return
	}
	if body.RefID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refId obrigatório"})
		return
	}

	// Cached result unless the caller explicitly asks to redo it.
	if !body.Force {
		if row, err := s.transcripts.Get(r.Context(), body.Scope, body.RefID); err == nil && row.Status == "done" {
			writeJSON(w, http.StatusOK, row)
			return
		}
	}

	if !s.stt.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "transcrição não configurada: defina WACALLS_STT_URL no serviço",
		})
		return
	}

	path, mime, chatJID, sessionID, err := s.resolveTranscribable(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	text, terr := s.stt.TranscribeFile(r.Context(), path, mime)
	row := transcriptRow{
		SessionID: sessionID,
		Scope:     body.Scope,
		RefID:     body.RefID,
		ChatJID:   chatJID,
		Lang:      s.stt.lang,
		Engine:    s.stt.Engine(),
		Text:      text,
		Status:    "done",
	}
	if terr != nil {
		row.Status = "error"
		row.Error = terr.Error()
	} else if strings.TrimSpace(text) == "" {
		row.Status = "empty"
	}
	saved, err := s.transcripts.Upsert(r.Context(), row)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if terr != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": terr.Error(), "transcript": saved})
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// resolveTranscribable maps a scope+refId to the audio file on disk.
func (s *server) resolveTranscribable(ctx context.Context, body transcriptCreateBody) (path, mime, chatJID, sessionID string, err error) {
	sessionID = strings.TrimSpace(body.SessionID)
	chatJID = strings.TrimSpace(body.ChatJID)

	if body.Scope == "recording" {
		info, ok, rerr := s.calls.RecordingByCall(ctx, body.RefID)
		if rerr != nil {
			return "", "", "", "", rerr
		}
		if !ok {
			return "", "", "", "", errors.New("gravação não encontrada para essa ligação")
		}
		if sessionID == "" {
			sess, _, _, _ := s.calls.CallMeta(ctx, body.RefID)
			sessionID = sess
		}
		return localMediaPath(info.Path), info.Mime, chatJID, sessionID, nil
	}

	if sessionID == "" {
		return "", "", "", "", errors.New("sessionId obrigatório")
	}
	msg, ok, merr := s.messages.Get(ctx, sessionID, body.RefID)
	if merr != nil {
		return "", "", "", "", merr
	}
	if !ok {
		return "", "", "", "", errors.New("mensagem não encontrada")
	}
	if msg.Kind != "audio" && !strings.HasPrefix(msg.MediaMime, "audio/") {
		return "", "", "", "", errors.New("a mensagem não contém áudio")
	}
	if msg.MediaURL == "" {
		return "", "", "", "", errors.New("áudio ainda não foi baixado")
	}
	if chatJID == "" {
		chatJID = msg.ChatJID
	}
	return localMediaPath(msg.MediaURL), msg.MediaMime, chatJID, sessionID, nil
}

// localMediaPath converts a served media URL ("/api/media/in/x.ogg") or a
// stored relative path into the on-disk location under media/.
func localMediaPath(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if strings.HasPrefix(p, "/api/media/") {
		return filepath.Join("media", filepath.FromSlash(strings.TrimPrefix(p, "/api/media/")))
	}
	return filepath.FromSlash(strings.TrimPrefix(p, "/"))
}

func (s *server) handleTranscriptDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.transcripts.Delete(r.Context(), r.PathValue("scope"), r.PathValue("refId")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}