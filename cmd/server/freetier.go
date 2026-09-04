package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Free tier — quando o usuário não tem assinatura paga ativa, recebe:
//   - 1 conexão WhatsApp
//   - 50 ligações por semana
//   - 50 conversas (mensagens enviadas) por semana
//
// Após atingir os limites, retorna 402 com QuotaError para o frontend
// exibir o convite a comprar plano.
//
// Os limites podem ser ajustados pelo super admin em
// settings_kv (chaves free_tier_connections/calls/chats).

type freeTierLimits struct {
	Connections int
	CallsWeek   int
	ChatsWeek   int
}

func defaultFreeTierLimits() freeTierLimits {
	return freeTierLimits{Connections: 1, CallsWeek: 50, ChatsWeek: 50}
}

func (s *server) freeTierLimits(ctx context.Context) freeTierLimits {
	out := defaultFreeTierLimits()
	if s.settings == nil {
		return out
	}
	if v, _ := s.settings.getKV(ctx, "free_tier_connections"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			out.Connections = n
		}
	}
	if v, _ := s.settings.getKV(ctx, "free_tier_calls_week"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			out.CallsWeek = n
		}
	}
	if v, _ := s.settings.getKV(ctx, "free_tier_chats_week"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			out.ChatsWeek = n
		}
	}
	return out
}

// ensureFreeTierSchema cria a tabela de contadores semanais.
func (s *server) ensureFreeTierSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS usage_weekly (
		user_id TEXT NOT NULL,
		week TEXT NOT NULL,
		kind TEXT NOT NULL,
		count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(user_id, week, kind)
	)`)
	return err
}

// currentWeekKey devolve o identificador ISO YYYY-Www da semana atual em UTC.
func currentWeekKey() string {
	y, w := time.Now().UTC().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

func (s *server) weeklyUsage(ctx context.Context, userID, kind string) int {
	if s == nil || s.db == nil || userID == "" {
		return 0
	}
	var n int
	_ = s.db.QueryRowContext(ctx,
		`SELECT count FROM usage_weekly WHERE user_id=? AND week=? AND kind=?`,
		userID, currentWeekKey(), kind,
	).Scan(&n)
	return n
}

func (s *server) bumpWeeklyUsage(ctx context.Context, userID, kind string) {
	if s == nil || s.db == nil || userID == "" {
		return
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO usage_weekly(user_id, week, kind, count) VALUES(?,?,?,1)
		 ON CONFLICT(user_id, week, kind) DO UPDATE SET count=count+1`,
		userID, currentWeekKey(), kind,
	)
	// Após contabilizar, verifica se o usuário cruzou 80% / 100% do limite
	// gratuito e dispara notificação (banner + email best-effort) uma única
	// vez por semana e limiar.
	s.maybeNotifyFreeTierThreshold(ctx, userID, kind)
}

// userHasPaidPlan retorna true se o usuário possui assinatura Stripe ativa
// (active/trialing) e ainda dentro do período cobrado. Admins do SaaS são
// considerados sempre pagos para não ficarem travados.
func (s *server) userHasPaidPlan(ctx context.Context, userID string, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	if userID == "" {
		return false
	}
	row, err := s.getSubscription(ctx, userID)
	if err != nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(row.Status))
	if status != "active" && status != "trialing" {
		return false
	}
	// current_period_end == 0 significa "ainda não fechou ciclo" — aceitamos.
	if row.CurrentPeriodEnd > 0 && row.CurrentPeriodEnd < time.Now().Unix() {
		return false
	}
	return true
}

// enforceFreeTier verifica se o uso atual (current) já atingiu o limite
// gratuito para o recurso (kind). Retorna QuotaError quando exceder.
// kind: "free_connections" | "free_calls" | "free_chats".
func (s *server) enforceFreeTier(ctx context.Context, userID string, isAdmin bool, kind string, current int) *QuotaError {
	if s.userHasPaidPlan(ctx, userID, isAdmin) {
		return nil
	}
	lim := s.freeTierLimits(ctx)
	var limit int
	var label, msg string
	switch kind {
	case "free_connections":
		limit = lim.Connections
		label = "conexão grátis"
		msg = fmt.Sprintf("Plano gratuito permite apenas %d conexão de WhatsApp. Assine um plano para conectar mais números.", limit)
	case "free_calls":
		limit = lim.CallsWeek
		label = "ligações/semana"
		msg = fmt.Sprintf("Você atingiu o limite gratuito de %d ligações por semana. Assine um plano para chamadas ilimitadas.", limit)
	case "free_chats":
		limit = lim.ChatsWeek
		label = "mensagens/semana"
		msg = fmt.Sprintf("Você atingiu o limite gratuito de %d conversas por semana. Assine um plano para mensagens ilimitadas.", limit)
	default:
		return nil
	}
	if limit <= 0 {
		return nil
	}
	if current >= limit {
		return &QuotaError{
			Kind:    kind,
			Label:   label,
			Limit:   limit,
			Current: current,
			Message: msg,
		}
	}
	return nil
}

// freeTierStatusResponse é o payload de /api/billing/free-tier
type freeTierStatusResponse struct {
	Paid            bool               `json:"paid"`
	Connections     int                `json:"connections"`
	ConnectionsUsed int                `json:"connectionsUsed"`
	CallsLimit      int                `json:"callsLimit"`
	CallsUsed       int                `json:"callsUsed"`
	ChatsLimit      int                `json:"chatsLimit"`
	ChatsUsed       int                `json:"chatsUsed"`
	Week            string             `json:"week"`
	Alerts          []freeTierAlertRow `json:"alerts"`
}

func (s *server) handleFreeTierStatus(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	lim := s.freeTierLimits(r.Context())
	resp := freeTierStatusResponse{
		Paid:            s.userHasPaidPlan(r.Context(), u.ID, u.IsAdmin()),
		Connections:     lim.Connections,
		ConnectionsUsed: len(s.sessions.infosFor(u.ID, false)),
		CallsLimit:      lim.CallsWeek,
		CallsUsed:       s.weeklyUsage(r.Context(), u.ID, "free_calls"),
		ChatsLimit:      lim.ChatsWeek,
		ChatsUsed:       s.weeklyUsage(r.Context(), u.ID, "free_chats"),
		Week:            currentWeekKey(),
		Alerts:          s.listFreeTierAlerts(r.Context(), u.ID),
	}
	writeJSON(w, 200, resp)
}

// handleSetFreeTierLimits — super admin ajusta limites do plano gratuito.
func (s *server) handleSetFreeTierLimits(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Connections *int `json:"connections"`
		CallsWeek   *int `json:"callsWeek"`
		ChatsWeek   *int `json:"chatsWeek"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	ctx := r.Context()
	if body.Connections != nil {
		_ = s.settings.setKV(ctx, "free_tier_connections", strconv.Itoa(*body.Connections))
	}
	if body.CallsWeek != nil {
		_ = s.settings.setKV(ctx, "free_tier_calls_week", strconv.Itoa(*body.CallsWeek))
	}
	if body.ChatsWeek != nil {
		_ = s.settings.setKV(ctx, "free_tier_chats_week", strconv.Itoa(*body.ChatsWeek))
	}
	lim := s.freeTierLimits(ctx)
	writeJSON(w, 200, map[string]any{
		"connections": lim.Connections,
		"callsWeek":   lim.CallsWeek,
		"chatsWeek":   lim.ChatsWeek,
	})
}

func (s *server) handleGetFreeTierLimits(w http.ResponseWriter, r *http.Request) {
	lim := s.freeTierLimits(r.Context())
	writeJSON(w, 200, map[string]any{
		"connections": lim.Connections,
		"callsWeek":   lim.CallsWeek,
		"chatsWeek":   lim.ChatsWeek,
	})
}

func (s *server) registerFreeTierRoutes(mux *http.ServeMux) {
	_ = s.ensureFreeTierSchema(context.Background())
	mux.HandleFunc("GET /api/billing/free-tier", s.requireAuth(s.handleFreeTierStatus))
	mux.HandleFunc("GET /api/billing/free-tier/limits", s.requireSuperAdmin(s.handleGetFreeTierLimits))
	mux.HandleFunc("PUT /api/billing/free-tier/limits", s.requireSuperAdmin(s.handleSetFreeTierLimits))
}
