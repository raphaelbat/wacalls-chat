package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// transcriber posts an audio file to the configured speech-to-text endpoint
// and returns the plain-text transcript.
//
// Two wire formats are supported:
//   - multipart (default when the URL looks OpenAI-compatible, i.e. contains
//     "/audio/transcriptions", or when WACALLS_STT_MODE=multipart): the file
//     is sent as the "file" form field alongside "model" and "language".
//   - raw (default otherwise, matching the URA bridge): the audio bytes are
//     POSTed with the file's Content-Type and X-Language header.
//
// Both expect a JSON response with a "text" field.
type transcriber struct {
	url    string
	auth   string
	lang   string
	model  string
	mode   string
	client *http.Client
}

func newTranscriber() *transcriber {
	return &transcriber{
		url:    strings.TrimSpace(os.Getenv("WACALLS_STT_URL")),
		auth:   strings.TrimSpace(os.Getenv("WACALLS_STT_AUTH")),
		lang:   strings.TrimSpace(os.Getenv("WACALLS_STT_LANG")),
		model:  strings.TrimSpace(os.Getenv("WACALLS_STT_MODEL")),
		mode:   strings.ToLower(strings.TrimSpace(os.Getenv("WACALLS_STT_MODE"))),
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (t *transcriber) Enabled() bool { return t != nil && t.url != "" }

func (t *transcriber) Engine() string {
	if !t.Enabled() {
		return ""
	}
	if t.model != "" {
		return t.model
	}
	return "stt"
}

func (t *transcriber) multipartMode() bool {
	switch t.mode {
	case "multipart", "openai":
		return true
	case "raw", "binary":
		return false
	}
	return strings.Contains(t.url, "/audio/transcriptions")
}

// maxTranscribeBytes guards against feeding huge files to the STT provider.
const maxTranscribeBytes = 40 << 20 // 40 MiB

// TranscribeFile reads a local audio file and returns its transcript.
func (t *transcriber) TranscribeFile(ctx context.Context, path, mime string) (string, error) {
	if !t.Enabled() {
		return "", errors.New("transcrição não configurada (defina WACALLS_STT_URL)")
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("áudio não encontrado: %w", err)
	}
	if st.Size() == 0 {
		return "", errors.New("arquivo de áudio vazio")
	}
	if st.Size() > maxTranscribeBytes {
		return "", errors.New("arquivo de áudio muito grande para transcrever")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if mime == "" {
		mime = guessAudioMime(path)
	}
	if t.multipartMode() {
		return t.postMultipart(ctx, filepath.Base(path), mime, data)
	}
	return t.postRaw(ctx, mime, data)
}

func (t *transcriber) postRaw(ctx context.Context, mime string, data []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mime)
	if t.auth != "" {
		req.Header.Set("Authorization", t.auth)
	}
	if t.lang != "" {
		req.Header.Set("X-Language", t.lang)
	}
	return t.do(req)
}

func (t *transcriber) postMultipart(ctx context.Context, name, mime string, data []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename=%q`, name)}
	h["Content-Type"] = []string{mime}
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	model := t.model
	if model == "" {
		model = "whisper-1"
	}
	_ = mw.WriteField("model", model)
	if t.lang != "" {
		_ = mw.WriteField("language", t.lang)
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if t.auth != "" {
		req.Header.Set("Authorization", t.auth)
	}
	return t.do(req)
}

func (t *transcriber) do(req *http.Request) (string, error) {
	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("stt http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Text       string `json:"text"`
		Transcript string `json:"transcript"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		// Some bridges answer with plain text.
		txt := strings.TrimSpace(string(body))
		if txt != "" && !strings.HasPrefix(txt, "{") {
			return txt, nil
		}
		return "", err
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		text = strings.TrimSpace(out.Transcript)
	}
	return text, nil
}

func guessAudioMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".webm":
		return "audio/webm"
	case ".wav":
		return "audio/wav"
	}
	return "application/octet-stream"
}