// Package cloudapi is a thin client for the official Meta WhatsApp Cloud API
// (graph.facebook.com/v20.0). It sits alongside the existing whatsmeow-based
// transport so a session can pick either mode at runtime.
package cloudapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://graph.facebook.com"
	DefaultVersion = "v20.0"
)

// Client talks to the Cloud API on behalf of one phone number.
type Client struct {
	BaseURL string
	Version string
	PhoneID string // Phone Number ID
	WABAID  string // WhatsApp Business Account ID (needed only for templates)
	Token   string // Permanent System User access token
	HTTP    *http.Client
}

func New(phoneID, wabaID, token string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		Version: DefaultVersion,
		PhoneID: phoneID,
		WABAID:  wabaID,
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) endpoint(path string) string {
	return fmt.Sprintf("%s/%s/%s", strings.TrimRight(c.BaseURL, "/"), c.Version, strings.TrimLeft(path, "/"))
}

// ---------- Errors ----------

type APIError struct {
	Status     int
	Code       int    `json:"code"`
	Message    string `json:"message"`
	Type       string `json:"type"`
	Subcode    int    `json:"error_subcode"`
	FBTraceID  string `json:"fbtrace_id"`
	Raw        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cloudapi: status=%d code=%d subcode=%d: %s", e.Status, e.Code, e.Subcode, e.Message)
}

// IsOutside24hWindow reports the "cannot send free-form outside 24h" case.
func (e *APIError) IsOutside24hWindow() bool { return e.Code == 131047 }

// ---------- Request helper ----------

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var wrap struct {
			Error APIError `json:"error"`
		}
		_ = json.Unmarshal(raw, &wrap)
		wrap.Error.Status = resp.StatusCode
		wrap.Error.Raw = string(raw)
		if wrap.Error.Message == "" {
			wrap.Error.Message = string(raw)
		}
		return &wrap.Error
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("cloudapi: decode: %w (%s)", err, string(raw))
		}
	}
	return nil
}

// ---------- Send envelopes ----------

type sendResp struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	Contacts []struct {
		Input string `json:"input"`
		WAID  string `json:"wa_id"`
	} `json:"contacts"`
}

func firstID(r sendResp) string {
	if len(r.Messages) > 0 {
		return r.Messages[0].ID
	}
	return ""
}

// SendText sends a plain text message. PreviewURL enables link previews.
func (c *Client) SendText(ctx context.Context, to, body string, previewURL bool) (string, error) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "text",
		"text":              map[string]any{"body": body, "preview_url": previewURL},
	}
	var r sendResp
	if err := c.do(ctx, http.MethodPost, c.PhoneID+"/messages", payload, &r); err != nil {
		return "", err
	}
	return firstID(r), nil
}

// MediaPayload describes an outgoing image/document/video/audio/sticker.
type MediaPayload struct {
	Kind     string // "image" | "document" | "video" | "audio" | "sticker"
	MediaID  string // preferred if uploaded
	Link     string // https URL alternative
	Caption  string // for image/document/video
	Filename string // for document
	Voice    bool   // for audio -> ptt=true
}

func (c *Client) SendMedia(ctx context.Context, to string, m MediaPayload) (string, error) {
	if m.Kind == "" {
		return "", errors.New("cloudapi: media kind required")
	}
	inner := map[string]any{}
	if m.MediaID != "" {
		inner["id"] = m.MediaID
	} else if m.Link != "" {
		inner["link"] = m.Link
	} else {
		return "", errors.New("cloudapi: media requires MediaID or Link")
	}
	if m.Caption != "" && (m.Kind == "image" || m.Kind == "document" || m.Kind == "video") {
		inner["caption"] = m.Caption
	}
	if m.Filename != "" && m.Kind == "document" {
		inner["filename"] = m.Filename
	}
	if m.Kind == "audio" && m.Voice {
		inner["voice"] = true
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              m.Kind,
		m.Kind:              inner,
	}
	var r sendResp
	if err := c.do(ctx, http.MethodPost, c.PhoneID+"/messages", payload, &r); err != nil {
		return "", err
	}
	return firstID(r), nil
}

// TemplateComponent mirrors the Cloud API template component shape (header/body/button).
type TemplateComponent struct {
	Type       string           `json:"type"`
	SubType    string           `json:"sub_type,omitempty"`
	Index      *int             `json:"index,omitempty"`
	Parameters []map[string]any `json:"parameters,omitempty"`
}

type TemplatePayload struct {
	Name       string
	Language   string // BCP-47 like "pt_BR"
	Components []TemplateComponent
}

func (c *Client) SendTemplate(ctx context.Context, to string, t TemplatePayload) (string, error) {
	tpl := map[string]any{
		"name":     t.Name,
		"language": map[string]any{"code": t.Language},
	}
	if len(t.Components) > 0 {
		tpl["components"] = t.Components
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template":          tpl,
	}
	var r sendResp
	if err := c.do(ctx, http.MethodPost, c.PhoneID+"/messages", payload, &r); err != nil {
		return "", err
	}
	return firstID(r), nil
}

// SendInteractive sends button/list interactive messages. body is the raw
// interactive object per Cloud API docs.
func (c *Client) SendInteractive(ctx context.Context, to string, interactive map[string]any) (string, error) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "interactive",
		"interactive":       interactive,
	}
	var r sendResp
	if err := c.do(ctx, http.MethodPost, c.PhoneID+"/messages", payload, &r); err != nil {
		return "", err
	}
	return firstID(r), nil
}

// MarkRead acknowledges an incoming message so the user sees read receipts.
func (c *Client) MarkRead(ctx context.Context, messageID string) error {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
	}
	return c.do(ctx, http.MethodPost, c.PhoneID+"/messages", payload, nil)
}

// UploadMedia uploads bytes and returns the media ID that can be used in Send*.
func (c *Client) UploadMedia(ctx context.Context, filename, mime string, data []byte) (string, error) {
	buf := &bytes.Buffer{}
	mp := multipart.NewWriter(buf)
	_ = mp.WriteField("messaging_product", "whatsapp")
	_ = mp.WriteField("type", mime)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", mime)
	part, err := mp.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	mp.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(c.PhoneID+"/media"), buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("cloudapi: upload status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// DownloadMedia resolves a media ID to bytes.
func (c *Client) DownloadMedia(ctx context.Context, mediaID string) ([]byte, string, error) {
	var meta struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
	}
	if err := c.do(ctx, http.MethodGet, mediaID, nil, &meta); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.URL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("cloudapi: download status=%d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	return data, meta.MimeType, err
}

// ---------- Metadata ----------

// PhoneInfo returns the phone number registration details (used as a "ping").
func (c *Client) PhoneInfo(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodGet, c.PhoneID, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Template is the shape returned by /{waba_id}/message_templates.
type Template struct {
	Name       string                   `json:"name"`
	Language   string                   `json:"language"`
	Status     string                   `json:"status"`
	Category   string                   `json:"category"`
	Components []map[string]any         `json:"components"`
	Extra      map[string]any           `json:"-"`
	Raw        map[string]any           `json:"-"`
}

// ListTemplates returns the approved templates for the WABA.
func (c *Client) ListTemplates(ctx context.Context) ([]Template, error) {
	if c.WABAID == "" {
		return nil, errors.New("cloudapi: WABA ID not configured")
	}
	var out struct {
		Data []Template `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, c.WABAID+"/message_templates?limit=200", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
