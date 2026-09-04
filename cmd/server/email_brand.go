package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/smtp"
	"strings"
)

// emailBrand reúne os campos do whitelabel usados nos e-mails transacionais.
type emailBrand struct {
	AppName string
	LogoURL string // absoluto, pronto para <img src>
	BaseURL string // http(s)://host
}

func (s *server) loadEmailBrand(ctx context.Context, r *http.Request) emailBrand {
	b := emailBrand{AppName: "VozZap"}
	b.BaseURL = baseURLFromRequest(r)
	v, _ := s.settings.getKV(ctx, "whitelabel")
	if v != "" {
		var wl map[string]any
		if err := json.Unmarshal([]byte(v), &wl); err == nil {
			if n, ok := wl["appName"].(string); ok && strings.TrimSpace(n) != "" {
				b.AppName = strings.TrimSpace(n)
			}
			logo, _ := wl["logoLight"].(string)
			if logo == "" {
				logo, _ = wl["logoDark"].(string)
			}
			if logo == "" {
				logo, _ = wl["favicon"].(string)
			}
			if logo != "" {
				if strings.HasPrefix(logo, "http://") || strings.HasPrefix(logo, "https://") {
					b.LogoURL = logo
				} else if b.BaseURL != "" {
					b.LogoURL = strings.TrimRight(b.BaseURL, "/") + "/" + strings.TrimLeft(logo, "/")
				}
			}
		}
	}
	return b
}

func baseURLFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "https"
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	host := r.Host
	if f := r.Header.Get("X-Forwarded-Host"); f != "" {
		host = strings.TrimSpace(strings.Split(f, ",")[0])
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

// renderBrandedEmail monta um HTML simples e responsivo com a logo do
// whitelabel no topo. ctaURL/ctaLabel são opcionais.
func renderBrandedEmail(b emailBrand, title, message, ctaURL, ctaLabel string) string {
	return renderBrandedEmailRich(b, title, message, "", ctaURL, ctaLabel)
}

// renderBrandedEmailRich aceita um bloco HTML extra (ex.: código de ativação)
// que NÃO é escapado, evitando que tags apareçam como texto no e-mail.
func renderBrandedEmailRich(b emailBrand, title, message, extraHTML, ctaURL, ctaLabel string) string {
	logoBlock := ""
	if b.LogoURL != "" {
		logoBlock = fmt.Sprintf(`<img src="%s" alt="%s" style="max-height:48px;max-width:200px;display:block;margin:0 auto 16px;">`,
			html.EscapeString(b.LogoURL), html.EscapeString(b.AppName))
	} else {
		logoBlock = fmt.Sprintf(`<div style="font-size:22px;font-weight:700;color:#111;margin-bottom:16px;text-align:center;">%s</div>`,
			html.EscapeString(b.AppName))
	}
	cta := ""
	if ctaURL != "" && ctaLabel != "" {
		cta = fmt.Sprintf(`<div style="text-align:center;margin:28px 0;">
  <a href="%s" style="background:#1f6feb;color:#ffffff;text-decoration:none;padding:12px 22px;border-radius:8px;font-weight:600;display:inline-block;">%s</a>
</div>
<p style="font-size:12px;color:#6b7280;text-align:center;">Caso o botão não funcione, copie e cole no navegador:<br><a href="%s" style="color:#1f6feb;word-break:break-all;">%s</a></p>`,
			html.EscapeString(ctaURL), html.EscapeString(ctaLabel),
			html.EscapeString(ctaURL), html.EscapeString(ctaURL))
	}
	return fmt.Sprintf(`<!doctype html><html><body style="margin:0;padding:0;background:#f3f4f6;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;">
<table width="100%%" cellpadding="0" cellspacing="0" style="background:#f3f4f6;padding:32px 0;"><tr><td align="center">
<table width="560" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:12px;box-shadow:0 4px 14px rgba(0,0,0,.06);overflow:hidden;">
<tr><td style="padding:32px 36px 24px;">
%s
<h1 style="font-size:20px;color:#111827;margin:0 0 12px;text-align:center;">%s</h1>
<p style="font-size:15px;color:#374151;line-height:1.55;margin:0 0 8px;">%s</p>
%s
%s
<hr style="border:none;border-top:1px solid #e5e7eb;margin:24px 0 12px;">
<p style="font-size:11px;color:#9ca3af;text-align:center;margin:0;">Você recebeu este e-mail porque está cadastrado em %s.</p>
</td></tr></table>
</td></tr></table></body></html>`,
		logoBlock,
		html.EscapeString(title),
		html.EscapeString(message),
		extraHTML,
		cta,
		html.EscapeString(b.AppName),
	)
}

// renderActivationCodeBlock devolve o bloco HTML estilizado com o código.
func renderActivationCodeBlock(code string) string {
	return fmt.Sprintf(`<div style="margin:24px auto;max-width:320px;background:#f9fafb;border:1px solid #e5e7eb;border-radius:12px;padding:18px 12px;text-align:center;">
  <div style="font-size:11px;letter-spacing:2px;color:#6b7280;text-transform:uppercase;margin-bottom:8px;">Seu código</div>
  <div style="font-family:'SFMono-Regular',Menlo,Consolas,monospace;font-size:34px;font-weight:700;letter-spacing:10px;color:#111827;">%s</div>
</div>`, html.EscapeString(code))
}

// sendBrandedEmail entrega um e-mail HTML usando a configuração SMTP.
func sendBrandedEmail(cfg smtpConfig, to, subject, htmlBody string) error {
	headers := []string{
		"From: " + cfg.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
	}
	msg := []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + htmlBody)
	addr := cfg.Host + ":" + cfg.Port
	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}
	return smtp.SendMail(addr, auth, cfg.From, []string{to}, msg)
}
