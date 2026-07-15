package cloudapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// InboundMessage is the normalized shape our server consumes for each incoming
// message event, regardless of whether it came from whatsmeow or Cloud API.
type InboundMessage struct {
	ID          string `json:"id"`
	FromWAID    string `json:"from"`
	Timestamp   int64  `json:"ts"`   // unix ms
	Kind        string `json:"kind"` // text/image/audio/video/document/sticker/interactive/button/location
	Body        string `json:"body"`
	MediaID     string `json:"mediaId,omitempty"`
	MimeType    string `json:"mime,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Caption     string `json:"caption,omitempty"`
	ContextID   string `json:"contextId,omitempty"` // quoted message id
	ProfileName string `json:"profileName,omitempty"`
}

// StatusEvent describes a message delivery status transition.
type StatusEvent struct {
	ID        string `json:"id"`
	Status    string `json:"status"` // sent/delivered/read/failed
	Timestamp int64  `json:"ts"`
	Recipient string `json:"recipient"`
	Error     string `json:"error,omitempty"`
}

// WebhookPayload is the top-level shape POSTed by Meta.
type WebhookPayload struct {
	Object string         `json:"object"`
	Entry  []WebhookEntry `json:"entry"`
}

type WebhookEntry struct {
	ID      string          `json:"id"`
	Changes []WebhookChange `json:"changes"`
}

type WebhookChange struct {
	Field string       `json:"field"`
	Value WebhookValue `json:"value"`
}

type WebhookValue struct {
	MessagingProduct string         `json:"messaging_product"`
	Metadata         map[string]any `json:"metadata"`
	Contacts         []struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
		WAID string `json:"wa_id"`
	} `json:"contacts"`
	Messages []map[string]any `json:"messages"`
	Statuses []map[string]any `json:"statuses"`
}

// PhoneNumberIDs returns the official WhatsApp phone_number_id values present
// in the webhook metadata. Meta sends this inside each change value; using it
// lets the server route events to the correct local session even when the
// callback URL was configured with a stale/different session id.
func (p *WebhookPayload) PhoneNumberIDs() []string {
	seen := map[string]bool{}
	out := []string{}
	if p == nil {
		return out
	}
	for _, e := range p.Entry {
		for _, c := range e.Changes {
			id := strings.TrimSpace(anyString(c.Value.Metadata["phone_number_id"]))
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// ParseWebhook unmarshals the raw body into a WebhookPayload.
func ParseWebhook(raw []byte) (*WebhookPayload, error) {
	var p WebhookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Normalize walks a payload and returns flat lists of messages and statuses
// with contact/profile lookup applied.
func (p *WebhookPayload) Normalize() ([]InboundMessage, []StatusEvent) {
	var msgs []InboundMessage
	var stats []StatusEvent
	for _, e := range p.Entry {
		for _, c := range e.Changes {
			names := map[string]string{}
			for _, ct := range c.Value.Contacts {
				names[ct.WAID] = ct.Profile.Name
			}
			for _, m := range c.Value.Messages {
				im := decodeMessage(m)
				if n, ok := names[im.FromWAID]; ok {
					im.ProfileName = n
				}
				msgs = append(msgs, im)
			}
			for _, s := range c.Value.Statuses {
				stats = append(stats, decodeStatus(s))
			}
		}
	}
	return msgs, stats
}

func s(m map[string]any, k string) string {
	if v, ok := m[k]; ok {
		return anyString(v)
	}
	return ""
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}

func decodeMessage(m map[string]any) InboundMessage {
	im := InboundMessage{
		ID:       s(m, "id"),
		FromWAID: s(m, "from"),
		Kind:     s(m, "type"),
	}
	if ts := s(m, "timestamp"); ts != "" {
		// timestamps are seconds as strings
		var n int64
		for _, r := range ts {
			if r < '0' || r > '9' {
				continue
			}
			n = n*10 + int64(r-'0')
		}
		im.Timestamp = n * 1000
	}
	if ctx, ok := m["context"].(map[string]any); ok {
		im.ContextID = s(ctx, "id")
	}
	switch im.Kind {
	case "text":
		if t, ok := m["text"].(map[string]any); ok {
			im.Body = s(t, "body")
		}
	case "image", "audio", "video", "document", "sticker":
		if med, ok := m[im.Kind].(map[string]any); ok {
			im.MediaID = s(med, "id")
			im.MimeType = s(med, "mime_type")
			im.Filename = s(med, "filename")
			im.Caption = s(med, "caption")
			im.Body = im.Caption
		}
	case "interactive":
		if it, ok := m["interactive"].(map[string]any); ok {
			if br, ok := it["button_reply"].(map[string]any); ok {
				im.Body = s(br, "title")
			} else if lr, ok := it["list_reply"].(map[string]any); ok {
				im.Body = s(lr, "title")
			}
		}
	case "button":
		if b, ok := m["button"].(map[string]any); ok {
			im.Body = s(b, "text")
		}
	case "location":
		if l, ok := m["location"].(map[string]any); ok {
			im.Body = "📍 " + s(l, "name") + " " + s(l, "address")
		}
	}
	return im
}

func decodeStatus(m map[string]any) StatusEvent {
	ev := StatusEvent{
		ID:        s(m, "id"),
		Status:    s(m, "status"),
		Recipient: s(m, "recipient_id"),
	}
	if ts := s(m, "timestamp"); ts != "" {
		var n int64
		for _, r := range ts {
			if r < '0' || r > '9' {
				continue
			}
			n = n*10 + int64(r-'0')
		}
		ev.Timestamp = n * 1000
	}
	if errs, ok := m["errors"].([]any); ok && len(errs) > 0 {
		if em, ok := errs[0].(map[string]any); ok {
			ev.Error = s(em, "title")
			if msg := s(em, "message"); msg != "" {
				ev.Error = msg
			}
		}
	}
	return ev
}

// VerifySignature validates the X-Hub-Signature-256 header ("sha256=<hex>").
func VerifySignature(appSecret, header string, body []byte) bool {
	if appSecret == "" || header == "" {
		return false
	}
	header = strings.TrimPrefix(header, "sha256=")
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(header))
}
