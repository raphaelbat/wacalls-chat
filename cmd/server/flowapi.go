package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *server) registerFlowRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/flows", s.requireAuth(s.handleFlowList))
	mux.HandleFunc("GET /api/flows/health", s.requireAuth(s.handleFlowHealth))
	mux.HandleFunc("GET /api/flows/trace", s.requireAuth(s.handleFlowTrace))
	mux.HandleFunc("POST /api/flows", s.requireAuth(s.handleFlowCreate))
	mux.HandleFunc("GET /api/flows/{id}", s.requireAuth(s.handleFlowGet))
	mux.HandleFunc("PUT /api/flows/{id}", s.requireAuth(s.handleFlowUpdate))
	mux.HandleFunc("DELETE /api/flows/{id}", s.requireAuth(s.handleFlowDelete))
	mux.HandleFunc("POST /api/flows/{id}/duplicate", s.requireAuth(s.handleFlowDuplicate))
	mux.HandleFunc("POST /api/flows/{id}/test", s.requireAuth(s.handleFlowTest))
	mux.HandleFunc("GET /api/flows/{id}/runs", s.requireAuth(s.handleFlowRuns))
	mux.HandleFunc("GET /api/flows/{id}/stats", s.requireAuth(s.handleFlowStats))
	mux.HandleFunc("GET /api/runs/{id}/events", s.requireAuth(s.handleRunEvents))
	mux.HandleFunc("POST /api/flow/assets", s.requireAuth(s.handleFlowAssetUpload))
}

// Maximum upload size for a flow asset (audio/image/video/document) — 50 MB.
const maxFlowAssetBytes int64 = 50 * 1024 * 1024

func flowAssetsDir() string { return filepath.Join("media", "flowassets") }

func extForFlowAsset(mime, filename string) string {
	if ext := filepath.Ext(filename); ext != "" && len(ext) <= 6 {
		return strings.ToLower(ext)
	}
	switch {
	case strings.HasPrefix(mime, "audio/webm"):
		return ".webm"
	case strings.HasPrefix(mime, "audio/ogg"):
		return ".ogg"
	case strings.HasPrefix(mime, "audio/mpeg"):
		return ".mp3"
	case strings.HasPrefix(mime, "audio/wav"), strings.HasPrefix(mime, "audio/x-wav"):
		return ".wav"
	case strings.HasPrefix(mime, "audio/mp4"), strings.HasPrefix(mime, "audio/aac"):
		return ".m4a"
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/jpeg"), strings.HasPrefix(mime, "image/jpg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mime, "video/mp4"):
		return ".mp4"
	case strings.HasPrefix(mime, "video/webm"):
		return ".webm"
	case strings.HasPrefix(mime, "application/pdf"):
		return ".pdf"
	}
	return ".bin"
}

// handleFlowAssetUpload stores an arbitrary file (audio recording, image,
// video, document) under media/flowassets and returns its public URL so it can
// be referenced by record_audio / whatsapp_media flow nodes.
func (s *server) handleFlowAssetUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFlowAssetBytes+1024)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload: " + err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'file' field"})
		return
	}
	defer file.Close()

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = r.FormValue("mime")
	}
	if mime == "" {
		mime = "application/octet-stream"
	}

	if err := os.MkdirAll(flowAssetsDir(), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create assets dir"})
		return
	}
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "id gen failed"})
		return
	}
	id := hex.EncodeToString(idBytes)
	ext := extForFlowAsset(mime, header.Filename)
	relPath := filepath.Join("flowassets", id+ext)
	absPath := filepath.Join("media", relPath)

	dst, err := os.Create(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot write file"})
		return
	}
	written, copyErr := io.Copy(dst, file)
	_ = dst.Close()
	if copyErr != nil {
		_ = os.Remove(absPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload failed: " + copyErr.Error()})
		return
	}
	if written > maxFlowAssetBytes {
		_ = os.Remove(absPath)
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
		return
	}
	url := "/api/media/" + relPath
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        url,
		"size":       written,
		"mime":       mime,
		"filename":   header.Filename,
		"uploadedAt": time.Now().UnixMilli(),
	})
}

func (s *server) handleFlowList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	list, err := s.flows.List(r.Context(), u.ID, u.IsAdmin())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"flows": list})
}

func (s *server) handleFlowCreate(w http.ResponseWriter, r *http.Request) {
	var body FlowRow
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Name == "" {
		body.Name = "Novo fluxo"
	}
	if body.Trigger == "" {
		body.Trigger = "inbound"
	}
	if body.Graph == "" {
		body.Graph = `{"nodes":[],"edges":[],"startNodeId":""}`
	}
	body.ID = newFlowID()
	body.Enabled = true
	body.OwnerID = currentUserFromReq(r).ID
	body.Keywords = normalizeKeywords(body.Keywords)
	if body.KeywordMatch == "" {
		body.KeywordMatch = "contains"
	}
	if err := s.flows.Insert(r.Context(), body); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out, _ := s.flows.Get(r.Context(), body.ID)
	writeJSON(w, 200, out)
}

// flowAccess fetches a flow and enforces ownership. Returns nil after writing
// the appropriate error response.
func (s *server) flowAccess(w http.ResponseWriter, r *http.Request, id string) *FlowRow {
	row, err := s.flows.Get(r.Context(), id)
	if err != nil || row == nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return nil
	}
	u := currentUserFromReq(r)
	if !u.IsAdmin() && row.OwnerID != u.ID {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return nil
	}
	return row
}

func (s *server) handleFlowGet(w http.ResponseWriter, r *http.Request) {
	row := s.flowAccess(w, r, r.PathValue("id"))
	if row == nil {
		return
	}
	writeJSON(w, 200, row)
}

func (s *server) handleFlowUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur := s.flowAccess(w, r, id)
	if cur == nil {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	var body FlowRow
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	// Detect which fields were actually present in the request so we can
	// preserve current values for omitted ones (Go's default for `bool`
	// is `false`, which would otherwise silently disable a flow on every
	// autosave that only sends {name, trigger, graph}).
	var present map[string]json.RawMessage
	_ = json.Unmarshal(raw, &present)
	if _, ok := present["enabled"]; !ok {
		body.Enabled = cur.Enabled
	}
	if _, ok := present["keywords"]; !ok {
		body.Keywords = cur.Keywords
	} else {
		body.Keywords = normalizeKeywords(body.Keywords)
	}
	if _, ok := present["keywordMatch"]; !ok {
		body.KeywordMatch = cur.KeywordMatch
	}
	body.ID = id
	body.OwnerID = cur.OwnerID
	if body.Name == "" {
		body.Name = cur.Name
	}
	if body.Trigger == "" {
		body.Trigger = cur.Trigger
	}
	if body.Graph == "" {
		body.Graph = cur.Graph
	}
	if err := s.flows.Update(r.Context(), body); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out, _ := s.flows.Get(r.Context(), id)
	writeJSON(w, 200, out)
}

func (s *server) handleFlowDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if cur := s.flowAccess(w, r, id); cur == nil {
		return
	}
	if err := s.flows.Delete(r.Context(), id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *server) handleFlowDuplicate(w http.ResponseWriter, r *http.Request) {
	cur := s.flowAccess(w, r, r.PathValue("id"))
	if cur == nil {
		return
	}
	cp := *cur
	cp.ID = newFlowID()
	cp.Name = cur.Name + " (cópia)"
	cp.OwnerID = currentUserFromReq(r).ID
	cp.CreatedAt = 0
	cp.UpdatedAt = 0
	if err := s.flows.Insert(r.Context(), cp); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out, _ := s.flows.Get(r.Context(), cp.ID)
	writeJSON(w, 200, out)
}

func (s *server) handleFlowTest(w http.ResponseWriter, r *http.Request) {
	cur := s.flowAccess(w, r, r.PathValue("id"))
	if cur == nil {
		return
	}
	var body struct {
		Inputs map[string]interface{} `json:"inputs"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Inputs == nil {
		body.Inputs = map[string]interface{}{}
	}
	trace, err := s.flowExec.TestRun(r.Context(), cur, body.Inputs)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"trace": trace})
}

func (s *server) handleFlowRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if cur := s.flowAccess(w, r, id); cur == nil {
		return
	}
	runs, err := s.flows.ListRuns(r.Context(), id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"runs": runs})
}

func (s *server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.flows.ListEvents(r.Context(), id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"events": events})
}

// handleFlowStats returns aggregate counts shown in the flow report dialog.
func (s *server) handleFlowStats(w http.ResponseWriter, r *http.Request) {
	cur := s.flowAccess(w, r, r.PathValue("id"))
	if cur == nil {
		return
	}
	st, err := s.flows.Stats(r.Context(), cur.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"flowId":   cur.ID,
		"name":     cur.Name,
		"keywords": cur.Keywords,
		"stats":    st,
	})
}

// normalizeKeywords trims, lowercases and dedupes a comma-separated list.
func normalizeKeywords(raw string) string {
	if raw == "" {
		return ""
	}
	seen := map[string]bool{}
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, ",")
}

// handleFlowHealth reports whether the runtime dependencies a flow needs to
// actually run are configured. The FlowEditor surfaces these as a banner so
// the operator notices a missing TTS endpoint before the first caller
// complains about silence.
func (s *server) handleFlowHealth(w http.ResponseWriter, r *http.Request) {
	tts := false
	stt := false
	if s.flowExec != nil && s.flowExec.bridge != nil {
		tts = s.flowExec.bridge.TTSConfigured()
		stt = s.flowExec.bridge.STTConfigured()
	}
	writeJSON(w, 200, map[string]any{
		"ttsConfigured": tts,
		"sttConfigured": stt,
		"hint": map[string]string{
			"tts": "WACALLS_TTS_URL (Piper/Coqui) ou voz ElevenLabs por fluxo",
			"stt": "WACALLS_STT_URL para capturar fala livre (DTMF funciona sem isso)",
		},
	})
}

// handleFlowTrace returns the correlated step-by-step trace for a single
// call attempt — every decision the inbound URA pipeline made, keyed by
// the same traceId that appears in backend logs and in the `flow-skip`
// SSE event the frontend received. Without `callId`, returns the most
// recent calls for the current owner so the user can pick one.
func (s *server) handleFlowTrace(w http.ResponseWriter, r *http.Request) {
	if s.flowTracer == nil {
		writeJSON(w, 200, map[string]any{"traces": []any{}})
		return
	}
	if cid := strings.TrimSpace(r.URL.Query().Get("callId")); cid != "" {
		steps := s.flowTracer.Get(cid)
		writeJSON(w, 200, map[string]any{"callId": cid, "steps": steps})
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	writeJSON(w, 200, map[string]any{"traces": s.flowTracer.ListBySession(sessionID, 20)})
}
