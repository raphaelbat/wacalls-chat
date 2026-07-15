package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"wacalls/internal/wa/cloudapi"
)

// registerCloudAPIRoutes wires the official Cloud API management endpoints.
// They live under /api/sessions/{sid}/cloud/... to keep grouping obvious.
func (s *server) registerCloudAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/sessions/{sid}/cloud/enable", s.requireAuth(s.handleCloudEnable))
	mux.HandleFunc("POST /api/sessions/{sid}/cloud/disable", s.requireAuth(s.handleCloudDisable))
	mux.HandleFunc("GET  /api/sessions/{sid}/cloud", s.requireAuth(s.handleCloudGet))
	mux.HandleFunc("GET  /api/sessions/{sid}/cloud/templates", s.requireAuth(s.handleCloudTemplates))
	mux.HandleFunc("POST /api/sessions/{sid}/cloud/test", s.requireAuth(s.handleCloudTestSend))
	mux.HandleFunc("POST /api/sessions/{sid}/cloud/send", s.requireAuth(s.handleCloudSend))
	// Meta webhook (verify + inbound). Public: auth is via verify_token +
	// X-Hub-Signature-256; never protect with requireAuth.
	mux.HandleFunc("GET  /api/wa-official/webhook/{sid}", s.handleCloudWebhookVerify)
	mux.HandleFunc("POST /api/wa-official/webhook/{sid}", s.handleCloudWebhookInbound)
}

// randomVerifyToken generates a 32-char hex verify token that the user pastes
// into Meta's webhook config.
func randomVerifyToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- Handlers ----------

type cloudEnableReq struct {
	PhoneID   string `json:"phoneId"`
	WABAID    string `json:"wabaId"`
	Token     string `json:"token"`
	AppSecret string `json:"appSecret"`
}

func (s *server) handleCloudEnable(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var body cloudEnableReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json inválido"})
		return
	}
	body.PhoneID = strings.TrimSpace(body.PhoneID)
	body.WABAID = strings.TrimSpace(body.WABAID)
	body.Token = strings.TrimSpace(body.Token)
	if body.PhoneID == "" || body.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phoneId e token são obrigatórios"})
		return
	}
	// Sanity-check credentials against the Graph API before persisting.
	client := cloudapi.New(body.PhoneID, body.WABAID, body.Token)
	if _, err := client.PhoneInfo(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Meta rejeitou as credenciais: " + err.Error()})
		return
	}
	verifyTok := randomVerifyToken()
	if err := s.sessions.EnableCloud(r.Context(), sid, body.PhoneID, body.WABAID, body.Token, body.AppSecret, verifyTok); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Host, "localhost") {
		scheme = "https" // assume TLS is terminated upstream (nginx)
	}
	if r.TLS == nil && strings.HasPrefix(r.Host, "localhost") {
		scheme = "http"
	}
	webhookURL := scheme + "://" + r.Host + "/api/wa-official/webhook/" + sid
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"mode":        "cloud",
		"webhookUrl":  webhookURL,
		"verifyToken": verifyTok,
	})
}

func (s *server) handleCloudDisable(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	if err := s.sessStore.setMode(r.Context(), sid, "whatsmeow"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": "whatsmeow"})
}

func (s *server) handleCloudGet(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	row, err := s.sessStore.getByID(r.Context(), sid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sessão não encontrada"})
		return
	}
	webhookURL := ""
	if row.Mode == "cloud" {
		scheme := "https"
		if r.TLS == nil && strings.HasPrefix(r.Host, "localhost") {
			scheme = "http"
		}
		webhookURL = scheme + "://" + r.Host + "/api/wa-official/webhook/" + sid
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           row.ID,
		"mode":         row.Mode,
		"phoneId":      row.CloudPhoneID,
		"wabaId":       row.CloudWABAID,
		"verifyToken":  row.CloudVerifyToken,
		"webhookUrl":   webhookURL,
		"hasToken":     row.CloudTokenEnc != "",
		"hasAppSecret": row.CloudAppSecretEnc != "",
	})
}

// cloudClientForSession loads decrypted credentials and returns a ready client.
func (s *server) cloudClientForSession(ctx context.Context, sid string) (*cloudapi.Client, sessionRow, error) {
	row, err := s.sessStore.getByID(ctx, sid)
	if err != nil {
		return nil, row, err
	}
	tok, _, err := row.CloudCreds()
	if err != nil {
		return nil, row, err
	}
	return cloudapi.New(row.CloudPhoneID, row.CloudWABAID, tok), row, nil
}

func (s *server) handleCloudTemplates(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	c, _, err := s.cloudClientForSession(r.Context(), sid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tpls, err := c.ListTemplates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": tpls})
}

func (s *server) handleCloudTestSend(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var body struct {
		To       string `json:"to"`
		Template string `json:"template"`
		Language string `json:"language"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	body.To = strings.TrimSpace(body.To)
	if body.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campo 'to' é obrigatório (formato E.164 sem +)"})
		return
	}
	if body.Template == "" {
		body.Template = "hello_world"
		body.Language = "en_US"
	}
	if body.Language == "" {
		body.Language = "pt_BR"
	}
	c, _, err := s.cloudClientForSession(r.Context(), sid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id, err := c.SendTemplate(r.Context(), body.To, cloudapi.TemplatePayload{Name: body.Template, Language: body.Language})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "messageId": id})
}

// handleCloudSend is a generic send endpoint the frontend/chat can use for
// cloud-mode sessions (text, media, template, interactive).
func (s *server) handleCloudSend(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var body struct {
		To          string                    `json:"to"`
		Kind        string                    `json:"kind"` // text|image|audio|video|document|sticker|template|interactive
		Text        string                    `json:"text"`
		PreviewURL  bool                      `json:"previewUrl"`
		MediaID     string                    `json:"mediaId"`
		Link        string                    `json:"link"`
		Caption     string                    `json:"caption"`
		Filename    string                    `json:"filename"`
		Voice       bool                      `json:"voice"`
		Template    *cloudapi.TemplatePayload `json:"template"`
		Interactive map[string]any            `json:"interactive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json inválido"})
		return
	}
	body.To = strings.TrimSpace(body.To)
	if body.To == "" || body.Kind == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to e kind são obrigatórios"})
		return
	}
	c, row, err := s.cloudClientForSession(r.Context(), sid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if row.Mode != "cloud" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sessão não está em modo cloud"})
		return
	}
	var msgID string
	switch body.Kind {
	case "text":
		msgID, err = c.SendText(r.Context(), body.To, body.Text, body.PreviewURL)
	case "template":
		if body.Template == nil {
			err = errNilTemplate
			break
		}
		msgID, err = c.SendTemplate(r.Context(), body.To, *body.Template)
	case "interactive":
		msgID, err = c.SendInteractive(r.Context(), body.To, body.Interactive)
	case "image", "audio", "video", "document", "sticker":
		msgID, err = c.SendMedia(r.Context(), body.To, cloudapi.MediaPayload{
			Kind: body.Kind, MediaID: body.MediaID, Link: body.Link,
			Caption: body.Caption, Filename: body.Filename, Voice: body.Voice,
		})
	default:
		err = errUnsupportedKind
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "messageId": msgID})
}

var (
	errNilTemplate     = &cloudSendErr{"template payload ausente"}
	errUnsupportedKind = &cloudSendErr{"kind não suportado"}
)

type cloudSendErr struct{ msg string }

func (e *cloudSendErr) Error() string { return e.msg }

// cloudapiMediaPayloadFor builds the MediaPayload the Cloud API needs for a
// given local kind. Voice notes (recorded via the mic) send as audio with
// voice=true so WhatsApp shows the play-pill UI instead of a document icon.
func cloudapiMediaPayloadFor(kind, mediaID, caption, filename string) cloudapi.MediaPayload {
	p := cloudapi.MediaPayload{
		Kind:     kind,
		MediaID:  mediaID,
		Caption:  caption,
		Filename: filename,
	}
	if kind == "audio" {
		p.Voice = true
	}
	return p
}

// cloudSendFriendlyErr converts Meta's raw API errors into messages the
// operator can act on. The two most common cases are: (1) trying to send a
// free-form message outside the 24h customer service window, and (2) using
// an unregistered or non-approved template.
func cloudSendFriendlyErr(err error) string {
	if err == nil {
		return ""
	}
	if ae, ok := err.(*cloudapi.APIError); ok {
		switch {
		case ae.IsOutside24hWindow():
			return "Fora da janela de 24h: o contato precisa te enviar uma mensagem primeiro, ou envie um template aprovado."
		case ae.Code == 132000, ae.Code == 132001, ae.Code == 132005, ae.Code == 132007, ae.Code == 132012:
			return "Template inválido ou não aprovado pela Meta: " + ae.Message
		case ae.Code == 190:
			return "Token do WhatsApp expirado ou inválido — atualize as credenciais em Conexões."
		case ae.Code == 100:
			return "Requisição rejeitada pela Meta: " + ae.Message
		}
		if ae.Message != "" {
			return "WhatsApp: " + ae.Message
		}
	}
	return err.Error()
}

// cloudSendStatus maps Cloud API errors to HTTP status codes so the frontend
// can distinguish permanent failures (bad credentials) from transient ones.
func cloudSendStatus(err error) int {
	if ae, ok := err.(*cloudapi.APIError); ok {
		if ae.Code == 190 || ae.Status == 401 || ae.Status == 403 {
			return http.StatusUnauthorized
		}
		if ae.IsOutside24hWindow() {
			return http.StatusUnprocessableEntity
		}
	}
	return http.StatusBadGateway
}
