package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Token de recuperação válido por 1 hora.
const passwordResetTTL = time.Hour

var (
	ErrResetTokenInvalid = errors.New("link de recuperação inválido ou expirado")
)

func (s *server) ensurePasswordResetSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS password_resets (
		token       TEXT PRIMARY KEY,
		user_id     TEXT NOT NULL,
		created_at  INTEGER NOT NULL,
		expires_at  INTEGER NOT NULL,
		used_at     INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

func newResetToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *server) registerPasswordResetRoutes(mux *http.ServeMux) {
	_ = s.ensurePasswordResetSchema(context.Background())
	mux.HandleFunc("POST /api/auth/forgot-password", s.handleForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", s.handleResetPassword)
}

// handleForgotPassword sempre responde 200 (não revela existência de e-mail).
// Se SMTP estiver configurado, envia o link por e-mail. Caso contrário,
// devolve o link no JSON para que o admin self-hosted consiga repassar.
func (s *server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
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

	resp := map[string]any{
		"ok":      true,
		"message": "Se o e-mail estiver cadastrado, enviaremos as instruções para redefinir a senha.",
	}

	var userID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ? AND active = 1`, email).Scan(&userID); err != nil {
		// Mesma resposta — não vazamos info.
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Invalida tokens anteriores e gera novo.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = ?`, userID)
	token := newResetToken()
	now := time.Now()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO password_resets (token, user_id, created_at, expires_at) VALUES (?,?,?,?)`,
		token, userID, now.Unix(), now.Add(passwordResetTTL).Unix()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	link := resetLinkForRequest(r, token)
	cfg := s.loadSMTPConfig(ctx)
	smtpOK := cfg.Host != "" && cfg.From != ""
	if smtpOK {
		brand := s.loadEmailBrand(ctx, r)
		go func() {
			html := renderBrandedEmail(brand,
				"Recuperação de senha",
				"Recebemos uma solicitação para redefinir sua senha. O link abaixo é válido por 1 hora. Se você não solicitou, ignore este e-mail.",
				link, "Redefinir minha senha")
			if err := sendBrandedEmail(cfg, email, "Recuperação de senha — "+brand.AppName, html); err != nil {
				fmt.Printf("[password-reset] smtp send failed for %s: %v\n", email, err)
			}
		}()
	} else {
		// Self-hosted sem SMTP: devolve o link para o admin copiar.
		resp["recoveryUrl"] = link
		resp["message"] = "SMTP não configurado. Copie o link abaixo e abra para definir uma nova senha (expira em 1 hora)."
		fmt.Printf("[password-reset] %s -> %s\n", email, link)
	}
	writeJSON(w, http.StatusOK, resp)
}

func resetLinkForRequest(r *http.Request, token string) string {
	return strings.TrimRight(baseURLFromRequest(r), "/") + "/reset-password?token=" + token
}

func (s *server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var body struct {
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ErrResetTokenInvalid.Error()})
		return
	}
	if len(body.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ErrWeakPassword.Error()})
		return
	}
	ctx := r.Context()
	now := time.Now().Unix()
	var userID string
	var expires, used int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at, used_at FROM password_resets WHERE token = ?`,
		token,
	).Scan(&userID, &expires, &used)
	if err != nil || used != 0 || expires < now {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ErrResetTokenInvalid.Error()})
		return
	}
	nh, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), 12)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(nh), userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE password_resets SET used_at = ? WHERE token = ?`, now, token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Revoga sessões ativas — força novo login.
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = ?`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
