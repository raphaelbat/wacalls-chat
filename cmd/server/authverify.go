package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const activationCodeTTL = 30 * time.Minute

func (s *server) ensureEmailVerificationSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS email_verifications (
		user_id    TEXT PRIMARY KEY,
		code       TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		attempts   INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	)`)
	return err
}

func newActivationCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func (s *server) registerEmailVerificationRoutes(mux *http.ServeMux) {
	_ = s.ensureEmailVerificationSchema(context.Background())
	mux.HandleFunc("POST /api/auth/verify-email", s.handleVerifyEmail)
	mux.HandleFunc("POST /api/auth/resend-code", s.handleResendCode)
}

// issueActivationCode cria/atualiza o código e envia por e-mail. Sem SMTP,
// devolve o código para o admin self-hosted conseguir ativar a conta.
func (s *server) issueActivationCode(ctx context.Context, r *http.Request, userID, email string) string {
	code := newActivationCode()
	now := time.Now()
	_, _ = s.db.ExecContext(ctx, `INSERT INTO email_verifications (user_id, code, created_at, expires_at, attempts)
		VALUES (?,?,?,?,0)
		ON CONFLICT(user_id) DO UPDATE SET code=excluded.code, created_at=excluded.created_at, expires_at=excluded.expires_at, attempts=0`,
		userID, code, now.Unix(), now.Add(activationCodeTTL).Unix())
	cfg := s.loadSMTPConfig(ctx)
	smtpOK := cfg.Host != "" && cfg.From != ""
	if smtpOK {
		brand := s.loadEmailBrand(ctx, r)
		go func() {
			msg := fmt.Sprintf("Use o código abaixo para confirmar sua conta. Ele expira em %d minutos.",
				int(activationCodeTTL.Minutes()))
			htmlBody := renderBrandedEmailRich(brand, "Código de ativação", msg, renderActivationCodeBlock(code), "", "")
			if err := sendBrandedEmail(cfg, email, "Código de ativação — "+brand.AppName, htmlBody); err != nil {
				fmt.Printf("[verify-email] smtp send failed for %s: %v\n", email, err)
			}
		}()
		return ""
	}
	fmt.Printf("[verify-email] %s -> %s\n", email, code)
	return code
}

func (s *server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "muitas tentativas, tente mais tarde"})
		return
	}
	var body struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	email := normalizeEmail(body.Email)
	code := strings.TrimSpace(body.Code)
	if email == "" || len(code) < 4 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dados incompletos"})
		return
	}
	ctx := r.Context()
	var userID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&userID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "código inválido"})
		return
	}
	var saved string
	var expires int64
	var attempts int
	if err := s.db.QueryRowContext(ctx, `SELECT code, expires_at, attempts FROM email_verifications WHERE user_id = ?`, userID).
		Scan(&saved, &expires, &attempts); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "código inválido"})
		return
	}
	if attempts >= 6 {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "muitas tentativas, solicite um novo código"})
		return
	}
	if time.Now().Unix() > expires {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "código expirado"})
		return
	}
	if !strings.EqualFold(saved, code) {
		_, _ = s.db.ExecContext(ctx, `UPDATE email_verifications SET attempts = attempts + 1 WHERE user_id = ?`, userID)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "código inválido"})
		return
	}
	if err := s.auth.SetActive(ctx, userID, true); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM email_verifications WHERE user_id = ?`, userID)
	token, err := s.auth.IssueToken(ctx, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	setAuthCookie(w, r, token)
	u, err := s.auth.UserByToken(ctx, token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *server) handleResendCode(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "muitas tentativas, tente mais tarde"})
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	email := normalizeEmail(body.Email)
	ctx := r.Context()
	var userID string
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT id, active FROM users WHERE email = ?`, email).Scan(&userID, &active)
	resp := map[string]any{"ok": true}
	if err != nil || active == 1 {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if dev := s.issueActivationCode(ctx, r, userID, email); dev != "" {
		resp["devCode"] = dev
	}
	writeJSON(w, http.StatusOK, resp)
}
