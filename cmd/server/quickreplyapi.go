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
)

// Limite do anexo de uma resposta rápida. O WhatsApp aceita bem mais em
// documento, mas 32 MB cobre imagem/áudio/vídeo curto sem encher o disco.
const maxQuickReplyMediaBytes int64 = 32 * 1024 * 1024

func quickRepliesMediaDir() string { return filepath.Join("media", "quickreplies") }

func (s *server) registerQuickReplyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/quick-replies", s.requireAuth(s.handleQuickReplyList))
	mux.HandleFunc("POST /api/quick-replies", s.requireAuth(s.handleQuickReplyCreate))
	mux.HandleFunc("PUT /api/quick-replies/{id}", s.requireAuth(s.handleQuickReplyUpdate))
	mux.HandleFunc("DELETE /api/quick-replies/{id}", s.requireAuth(s.handleQuickReplyDelete))
	mux.HandleFunc("POST /api/quick-replies/{id}/used", s.requireAuth(s.handleQuickReplyUsed))
	mux.HandleFunc("POST /api/quick-replies/{id}/media", s.requireAuth(s.handleQuickReplyMediaUpload))
	mux.HandleFunc("DELETE /api/quick-replies/{id}/media", s.requireAuth(s.handleQuickReplyMediaDelete))
}

func (s *server) handleQuickReplyList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	rows, err := s.quickReplies.List(r.Context(), u.ID, u.TenantID())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quickReplies": rows})
}

type quickReplyBody struct {
	Shortcut string `json:"shortcut"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Global   bool   `json:"global"`
}

func (s *server) handleQuickReplyCreate(w http.ResponseWriter, r *http.Request) {
	var body quickReplyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u := currentUserFromReq(r)
	row, err := s.quickReplies.Create(r.Context(), quickReplyRow{
		Shortcut: body.Shortcut, Title: body.Title, Content: body.Content,
		Global: body.Global, OwnerID: u.ID, TenantID: u.TenantID(),
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// canManageQuickReply allows the author, or any admin inside the tenant that
// owns a global snippet, to change it.
func (s *server) canManageQuickReply(r *http.Request, id string) bool {
	u := currentUserFromReq(r)
	if u == nil {
		return false
	}
	if u.IsSuperAdmin() {
		return true
	}
	row, err := s.quickReplies.Get(r.Context(), id)
	if err != nil {
		return false
	}
	if row.OwnerID == u.ID {
		return true
	}
	return row.Global && row.TenantID == u.TenantID() && u.IsAdmin()
}

func (s *server) handleQuickReplyUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageQuickReply(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such quick reply"})
		return
	}
	var body quickReplyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.quickReplies.Update(r.Context(), id, quickReplyRow{
		Shortcut: body.Shortcut, Title: body.Title, Content: body.Content, Global: body.Global,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleQuickReplyDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageQuickReply(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such quick reply"})
		return
	}
	// Apaga o anexo junto com o snippet para não deixar arquivo órfão.
	if row, err := s.quickReplies.Get(r.Context(), id); err == nil {
		removeQuickReplyFile(row.MediaPath)
	}
	if err := s.quickReplies.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleQuickReplyUsed(w http.ResponseWriter, r *http.Request) {
	_ = s.quickReplies.BumpUsage(r.Context(), r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// handleQuickReplyMediaUpload anexa (ou substitui) o arquivo de uma resposta
// rápida. Multipart com o campo "file"; "kind" é opcional e, quando ausente,
// é deduzido do Content-Type.
func (s *server) handleQuickReplyMediaUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageQuickReply(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such quick reply"})
		return
	}
	current, err := s.quickReplies.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such quick reply"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQuickReplyMediaBytes+1024)
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
		mime = strings.TrimSpace(r.FormValue("mime"))
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	kind := quickReplyKind(strings.ToLower(strings.TrimSpace(r.FormValue("kind"))), mime)

	if err := os.MkdirAll(quickRepliesMediaDir(), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create media dir"})
		return
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "id gen failed"})
		return
	}
	name := id + "-" + hex.EncodeToString(suffix) + extForFlowAsset(mime, header.Filename)
	absPath := filepath.Join(quickRepliesMediaDir(), name)

	dst, err := os.Create(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot write file"})
		return
	}
	written, copyErr := io.Copy(dst, io.LimitReader(file, maxQuickReplyMediaBytes+1))
	_ = dst.Close()
	if copyErr != nil {
		_ = os.Remove(absPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload failed: " + copyErr.Error()})
		return
	}
	if written > maxQuickReplyMediaBytes {
		_ = os.Remove(absPath)
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "arquivo maior que 32 MB"})
		return
	}

	filename := strings.TrimSpace(r.FormValue("filename"))
	if filename == "" {
		filename = filepath.Base(header.Filename)
	}
	if filename == "" || filename == "." {
		filename = "arquivo"
	}

	relPath := "quickreplies/" + name
	row, err := s.quickReplies.SetMedia(r.Context(), id, relPath, filename, mime, kind, written)
	if err != nil {
		_ = os.Remove(absPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Só descarta o anexo anterior depois que o novo já está salvo no banco.
	if current.MediaPath != "" && current.MediaPath != relPath {
		removeQuickReplyFile(current.MediaPath)
	}
	writeJSON(w, http.StatusOK, row)
}

// handleQuickReplyMediaDelete remove o anexo, mantendo o texto do snippet.
func (s *server) handleQuickReplyMediaDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageQuickReply(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such quick reply"})
		return
	}
	old, err := s.quickReplies.ClearMedia(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	removeQuickReplyFile(old)
	w.WriteHeader(http.StatusNoContent)
}

// quickReplyKind normaliza o tipo do anexo. Aceita o valor enviado pelo
// cliente e, quando vazio ou inválido, deduz pelo MIME.
func quickReplyKind(kind, mime string) string {
	switch kind {
	case "image", "video", "audio", "document":
		return kind
	}
	m := strings.ToLower(mime)
	switch {
	case strings.HasPrefix(m, "image/"):
		return "image"
	case strings.HasPrefix(m, "video/"):
		return "video"
	case strings.HasPrefix(m, "audio/"):
		return "audio"
	default:
		return "document"
	}
}

// removeQuickReplyFile apaga um anexo pelo caminho relativo salvo no banco,
// recusando qualquer caminho fora de media/quickreplies/.
func removeQuickReplyFile(relPath string) {
	if relPath == "" {
		return
	}
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if !strings.HasPrefix(clean, "quickreplies"+string(filepath.Separator)) {
		return
	}
	_ = os.Remove(filepath.Join("media", clean))
}
