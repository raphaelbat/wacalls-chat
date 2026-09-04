package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// Notificações do plano gratuito — disparam (uma vez por semana, por tipo e
// limiar) quando o usuário atinge 80% ou 100% do limite. A dedupe é feita pela
// tabela free_tier_notifications. A entrega é melhor-esforço: se o SMTP não
// estiver configurado o e-mail é ignorado, mas a notificação ainda aparece na
// tela via /api/billing/free-tier (que retorna o histórico para o frontend
// exibir banner/toast).

type smtpConfig struct {
	Host string `json:"host"`
	Port string `json:"port"`
	User string `json:"user"`
	Pass string `json:"pass,omitempty"`
	From string `json:"from"`
}

type freeTierAlertRow struct {
	Kind      string `json:"kind"`
	Threshold int    `json:"threshold"`
	Week      string `json:"week"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *server) ensureFreeTierNotifySchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS free_tier_notifications (
		user_id TEXT NOT NULL,
		week TEXT NOT NULL,
		kind TEXT NOT NULL,
		threshold INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		PRIMARY KEY(user_id, week, kind, threshold)
	)`)
	return err
}

func (s *server) loadSMTPConfig(ctx context.Context) smtpConfig {
	c := smtpConfig{}
	if s.settings == nil {
		return c
	}
	c.Host, _ = s.settings.getKV(ctx, "smtp_host")
	c.Port, _ = s.settings.getKV(ctx, "smtp_port")
	c.User, _ = s.settings.getKV(ctx, "smtp_user")
	c.Pass, _ = s.settings.getKV(ctx, "smtp_pass")
	c.From, _ = s.settings.getKV(ctx, "smtp_from")
	if strings.TrimSpace(c.Port) == "" {
		c.Port = "587"
	}
	return c
}

func (s *server) userEmail(ctx context.Context, userID string) string {
	if s == nil || s.db == nil || userID == "" {
		return ""
	}
	var email string
	_ = s.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ?`, userID).Scan(&email)
	return email
}

// freeTierKindDescriptor — rótulo + uso atual para um tipo de limite.
func (s *server) freeTierUsage(ctx context.Context, userID, kind string) (used, limit int, label string) {
	lim := s.freeTierLimits(ctx)
	switch kind {
	case "free_calls":
		return s.weeklyUsage(ctx, userID, kind), lim.CallsWeek, "ligações/semana"
	case "free_chats":
		return s.weeklyUsage(ctx, userID, kind), lim.ChatsWeek, "conversas/semana"
	case "free_connections":
		return len(s.sessions.infosFor(userID, false)), lim.Connections, "conexões"
	}
	return 0, 0, ""
}

// maybeNotifyFreeTierThreshold dispara o alerta (banner + email) se o uso
// atingiu 80% ou 100% pela primeira vez nesta semana. Seguro para chamar
// repetidamente: a dedupe é feita pela PK da tabela.
func (s *server) maybeNotifyFreeTierThreshold(ctx context.Context, userID, kind string) {
	if s == nil || s.db == nil || userID == "" {
		return
	}
	if s.userHasPaidPlan(ctx, userID, false) {
		return
	}
	used, limit, label := s.freeTierUsage(ctx, userID, kind)
	if limit <= 0 {
		return
	}
	// Só marca 100% quando o limite foi de fato ULTRAPASSADO (used > limit).
	// Usar used == limit como 100% gerava falso positivo logo na primeira
	// criação permitida (ex.: 1 conexão gratuita = 1/1 = 100%).
	// 80% também exige que ainda haja folga (used < limit) para não duplicar
	// com o alerta de bloqueio.
	threshold := 0
	if used > limit {
		threshold = 100
	} else if used < limit && used*100/limit >= 80 {
		threshold = 80
	}
	if threshold == 0 {
		return
	}
	week := currentWeekKey()
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO free_tier_notifications(user_id, week, kind, threshold, created_at) VALUES(?,?,?,?,?)`,
		userID, week, kind, threshold, time.Now().Unix(),
	)
	if err != nil {
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return // já notificado este limiar nesta semana
	}
	go s.sendFreeTierEmail(context.Background(), userID, kind, label, used, limit, threshold)
}

func (s *server) sendFreeTierEmail(ctx context.Context, userID, kind, label string, used, limit, threshold int) {
	cfg := s.loadSMTPConfig(ctx)
	if cfg.Host == "" || cfg.From == "" {
		return // SMTP não configurado — silencioso
	}
	to := s.userEmail(ctx, userID)
	if to == "" {
		return
	}
	subject := fmt.Sprintf("[VozZap] Você atingiu %d%% do limite de %s", threshold, label)
	if threshold >= 100 {
		subject = fmt.Sprintf("[VozZap] Limite do plano gratuito atingido — %s", label)
	}
	body := fmt.Sprintf(`Olá,

Você utilizou %d de %d %s do seu plano gratuito (%d%%).

%s

Para liberar uso ilimitado e continuar atendendo sem interrupções, assine um plano em Configurações → Financeiro.

— VozZap
`, used, limit, label, threshold,
		map[bool]string{true: "Seu limite semanal foi atingido. Novas operações serão bloqueadas até a próxima semana ou contratação de um plano.", false: "Você está próximo do limite semanal. Considere contratar um plano para evitar interrupções."}[threshold >= 100],
	)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.From, to, subject, body))

	addr := cfg.Host + ":" + cfg.Port
	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}
	if err := smtp.SendMail(addr, auth, cfg.From, []string{to}, msg); err != nil {
		fmt.Printf("[freetier-notify] smtp send failed for %s: %v\n", to, err)
	}
}

// listFreeTierAlerts devolve as notificações disparadas para o usuário na
// semana corrente, ordenadas por limiar (100 primeiro).
func (s *server) listFreeTierAlerts(ctx context.Context, userID string) []freeTierAlertRow {
	out := []freeTierAlertRow{}
	if s.db == nil {
		return out
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind, threshold, week, created_at FROM free_tier_notifications
		 WHERE user_id=? AND week=? ORDER BY threshold DESC, created_at DESC`,
		userID, currentWeekKey(),
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a freeTierAlertRow
		if err := rows.Scan(&a.Kind, &a.Threshold, &a.Week, &a.CreatedAt); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// ----- SMTP config endpoints (super admin) -----

func (s *server) handleGetSMTPConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.loadSMTPConfig(r.Context())
	writeJSON(w, 200, map[string]any{
		"host":       cfg.Host,
		"port":       cfg.Port,
		"user":       cfg.User,
		"from":       cfg.From,
		"passSet":    cfg.Pass != "",
		"configured": cfg.Host != "" && cfg.From != "",
	})
}

func (s *server) handleSetSMTPConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Host *string `json:"host"`
		Port *string `json:"port"`
		User *string `json:"user"`
		Pass *string `json:"pass"`
		From *string `json:"from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	ctx := r.Context()
	if in.Host != nil {
		_ = s.settings.setKV(ctx, "smtp_host", strings.TrimSpace(*in.Host))
	}
	if in.Port != nil {
		_ = s.settings.setKV(ctx, "smtp_port", strings.TrimSpace(*in.Port))
	}
	if in.User != nil {
		_ = s.settings.setKV(ctx, "smtp_user", strings.TrimSpace(*in.User))
	}
	if in.Pass != nil && strings.TrimSpace(*in.Pass) != "" {
		_ = s.settings.setKV(ctx, "smtp_pass", strings.TrimSpace(*in.Pass))
	}
	if in.From != nil {
		_ = s.settings.setKV(ctx, "smtp_from", strings.TrimSpace(*in.From))
	}
	s.handleGetSMTPConfig(w, r)
}

func (s *server) registerFreeTierNotifyRoutes(mux *http.ServeMux) {
	_ = s.ensureFreeTierNotifySchema(context.Background())
	mux.HandleFunc("GET /api/billing/smtp", s.requireSuperAdmin(s.handleGetSMTPConfig))
	mux.HandleFunc("PUT /api/billing/smtp", s.requireSuperAdmin(s.handleSetSMTPConfig))
	mux.HandleFunc("POST /api/billing/smtp/test", s.requireSuperAdmin(s.handleTestSMTP))
}

// handleTestSMTP envia um e-mail de validação para o destinatário informado
// usando o mesmo template branded da recuperação de senha.
func (s *server) handleTestSMTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		writeJSON(w, 400, map[string]string{"error": "destinatário obrigatório"})
		return
	}
	cfg := s.loadSMTPConfig(r.Context())
	if cfg.Host == "" || cfg.From == "" {
		writeJSON(w, 400, map[string]string{"error": "SMTP não configurado"})
		return
	}
	brand := s.loadEmailBrand(r.Context(), r)
	html := renderBrandedEmail(brand, "Teste de envio",
		"Este é um e-mail de teste do servidor SMTP. Se você recebeu, sua configuração está funcionando corretamente.",
		"", "")
	if err := sendBrandedEmail(cfg, to, "Teste de SMTP — "+brand.AppName, html); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "e-mail enviado"})
}
