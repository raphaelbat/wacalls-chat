package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// billingConfig is the persisted Stripe configuration (SaaS admin only).
// It is stored as JSON in settings_kv under the key "billing_stripe".
type billingConfig struct {
	SecretKey      string `json:"secretKey"`
	WebhookSecret  string `json:"webhookSecret"`
	PublishableKey string `json:"publishableKey,omitempty"`
	Currency       string `json:"currency,omitempty"` // brl, usd, eur (default brl)
	// RequirePaidToConnect — quando true (padrão) bloqueia criação/pareamento
	// de novas conexões WhatsApp para usuários sem assinatura ativa. Quando
	// falso, vale apenas o limite gratuito definido em free_tier_*.
	RequirePaidToConnect *bool `json:"requirePaidToConnect,omitempty"`
}

func (s *server) requirePaidToConnect(ctx context.Context) bool {
	cfg, err := s.loadBillingConfig(ctx)
	if err != nil {
		return false
	}
	if cfg.RequirePaidToConnect == nil {
		// Default = false: cliente novo consegue criar a 1ª conexão
		// (gratuita) sem precisar de assinatura ativa. O admin pode
		// ligar o flag em Configurações → Financeiro quando quiser
		// exigir pagamento para qualquer nova conexão.
		return false
	}
	return *cfg.RequirePaidToConnect
}

// enforcePaidConnect retorna QuotaError quando o sistema exige pagamento para
// conectar (criar/parear) uma nova sessão e o usuário não tem assinatura ativa.
func (s *server) enforcePaidConnect(ctx context.Context, userID string, isAdmin bool) *QuotaError {
	if !s.requirePaidToConnect(ctx) {
		return nil
	}
	if s.userHasPaidPlan(ctx, userID, isAdmin) {
		return nil
	}
	// Mesmo com a política "exigir pagamento" ligada, o plano gratuito continua
	// liberando a primeira conexão configurada em Free Tier. Sem este bypass, o
	// cliente novo via 0/1 em Cobrança, mas era bloqueado ao criar/parear o QR.
	lim := s.freeTierLimits(ctx)
	if lim.Connections > 0 && len(s.sessions.infosFor(userID, false)) <= lim.Connections {
		return nil
	}
	return &QuotaError{
		Kind:    "payment_required",
		Label:   "assinatura ativa",
		Limit:   1,
		Current: 0,
		Message: "Para conectar um WhatsApp é necessário um plano ativo. Acesse Financeiro e faça o pagamento.",
	}
}

func (s *server) loadBillingConfig(ctx context.Context) (billingConfig, error) {
	var cfg billingConfig
	v, err := s.settings.getKV(ctx, "billing_stripe")
	if err != nil || v == "" {
		return cfg, err
	}
	_ = json.Unmarshal([]byte(v), &cfg)
	if cfg.Currency == "" {
		cfg.Currency = "brl"
	}
	return cfg, nil
}

func (s *server) saveBillingConfig(ctx context.Context, cfg billingConfig) error {
	b, _ := json.Marshal(cfg)
	return s.settings.setKV(ctx, "billing_stripe", string(b))
}

// ensureBillingSchema creates the subscriptions table the first time the
// billing endpoints are touched. Kept inside billingapi.go to avoid changing
// unrelated bootstrap code.
func (s *server) ensureBillingSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS billing_subscriptions (
		user_id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'inactive',
		stripe_customer TEXT NOT NULL DEFAULT '',
		stripe_subscription TEXT NOT NULL DEFAULT '',
		quantity INTEGER NOT NULL DEFAULT 1,
		current_period_end INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL
	)`)
	return err
}

func maskKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "••••"
	}
	return k[:4] + "••••" + k[len(k)-4:]
}

func (s *server) registerBillingRoutes(mux *http.ServeMux) {
	_ = s.ensureBillingSchema(context.Background())
	mux.HandleFunc("GET /api/billing/config", s.requireSuperAdmin(s.handleGetBillingConfig))
	mux.HandleFunc("PUT /api/billing/config", s.requireSuperAdmin(s.handleSetBillingConfig))
	mux.HandleFunc("GET /api/billing/subscription", s.requireAuth(s.handleGetSubscription))
	mux.HandleFunc("PUT /api/billing/subscription/plan", s.requireAuth(s.handleSetSubscriptionPlan))
	mux.HandleFunc("POST /api/billing/checkout", s.requireAuth(s.handleCreateCheckout))
	// Webhook is intentionally unauthenticated: Stripe signs it with the
	// webhook secret and we verify it ourselves.
	mux.HandleFunc("POST /api/billing/webhook", s.handleStripeWebhook)
}

// ---------------- Config (super admin) ----------------

func (s *server) handleGetBillingConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadBillingConfig(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	rpc := false
	if cfg.RequirePaidToConnect != nil {
		rpc = *cfg.RequirePaidToConnect
	}
	writeJSON(w, 200, map[string]any{
		"secretKeyMask":        maskKey(cfg.SecretKey),
		"webhookSecretSet":     cfg.WebhookSecret != "",
		"publishableKey":       cfg.PublishableKey,
		"currency":             cfg.Currency,
		"enabled":              cfg.SecretKey != "",
		"requirePaidToConnect": rpc,
	})
}

func (s *server) handleSetBillingConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32*1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "payload too large"})
		return
	}
	var in struct {
		SecretKey            *string `json:"secretKey"`
		WebhookSecret        *string `json:"webhookSecret"`
		PublishableKey       *string `json:"publishableKey"`
		Currency             *string `json:"currency"`
		RequirePaidToConnect *bool   `json:"requirePaidToConnect"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	cfg, _ := s.loadBillingConfig(r.Context())
	// Only overwrite a field when the client sent a non-empty value.
	// Sending "" keeps the current saved value (so the masked form is safe
	// to re-submit without re-typing the secret).
	if in.SecretKey != nil && strings.TrimSpace(*in.SecretKey) != "" {
		cfg.SecretKey = strings.TrimSpace(*in.SecretKey)
	}
	if in.WebhookSecret != nil && strings.TrimSpace(*in.WebhookSecret) != "" {
		cfg.WebhookSecret = strings.TrimSpace(*in.WebhookSecret)
	}
	if in.PublishableKey != nil {
		cfg.PublishableKey = strings.TrimSpace(*in.PublishableKey)
	}
	if in.Currency != nil && *in.Currency != "" {
		cfg.Currency = strings.ToLower(strings.TrimSpace(*in.Currency))
	}
	if cfg.Currency == "" {
		cfg.Currency = "brl"
	}
	if in.RequirePaidToConnect != nil {
		v := *in.RequirePaidToConnect
		cfg.RequirePaidToConnect = &v
	}
	if err := s.saveBillingConfig(r.Context(), cfg); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	rpc := false
	if cfg.RequirePaidToConnect != nil {
		rpc = *cfg.RequirePaidToConnect
	}
	writeJSON(w, 200, map[string]any{
		"secretKeyMask":        maskKey(cfg.SecretKey),
		"webhookSecretSet":     cfg.WebhookSecret != "",
		"publishableKey":       cfg.PublishableKey,
		"currency":             cfg.Currency,
		"enabled":              cfg.SecretKey != "",
		"requirePaidToConnect": rpc,
	})
}

// ---------------- Subscription read ----------------

type subscriptionRow struct {
	UserID           string `json:"userId"`
	PlanID           string `json:"planId"`
	Status           string `json:"status"`
	StripeCustomer   string `json:"stripeCustomer,omitempty"`
	StripeSub        string `json:"stripeSubscription,omitempty"`
	Quantity         int    `json:"quantity"`
	CurrentPeriodEnd int64  `json:"currentPeriodEnd"`
	UpdatedAt        int64  `json:"updatedAt"`
}

func (s *server) getSubscription(ctx context.Context, userID string) (subscriptionRow, error) {
	var row subscriptionRow
	err := s.db.QueryRowContext(ctx, `SELECT user_id, plan_id, status, stripe_customer, stripe_subscription, quantity, current_period_end, updated_at FROM billing_subscriptions WHERE user_id=?`, userID).
		Scan(&row.UserID, &row.PlanID, &row.Status, &row.StripeCustomer, &row.StripeSub, &row.Quantity, &row.CurrentPeriodEnd, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return subscriptionRow{UserID: userID, Status: "inactive"}, nil
	}
	return row, err
}

func (s *server) upsertSubscription(ctx context.Context, row subscriptionRow) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO billing_subscriptions(user_id, plan_id, status, stripe_customer, stripe_subscription, quantity, current_period_end, updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET
			plan_id=excluded.plan_id,
			status=excluded.status,
			stripe_customer=excluded.stripe_customer,
			stripe_subscription=excluded.stripe_subscription,
			quantity=excluded.quantity,
			current_period_end=excluded.current_period_end,
			updated_at=excluded.updated_at`,
		row.UserID, row.PlanID, row.Status, row.StripeCustomer, row.StripeSub, row.Quantity, row.CurrentPeriodEnd, now)
	if err == nil && s.broker != nil && row.UserID != "" {
		// Push real-time billing update to the user's open SSE session so the
		// UI flips Pago/Pendente instantly without polling.
		s.broker.emitBilling(row.UserID, row.Status, row.PlanID, row.CurrentPeriodEnd)
	}
	return err
}

func (s *server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	row, err := s.getSubscription(r.Context(), u.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Garante vínculo automático com o primeiro plano cadastrado quando
	// o usuário ainda não escolheu nenhum (regra: todo cliente novo entra
	// no plano #1 do sistema).
	if row.PlanID == "" {
		var firstID string
		if err := s.db.QueryRowContext(r.Context(),
			`SELECT id FROM settings_plans ORDER BY created_at ASC LIMIT 1`).Scan(&firstID); err == nil && firstID != "" {
			row.PlanID = firstID
		}
	}
	writeJSON(w, 200, row)
}

// handleSetSubscriptionPlan permite ao usuário trocar de plano (quando o
// sistema tem mais de um cadastrado). Não altera status de pagamento; só
// atualiza o plano vinculado à conta.
func (s *server) handleSetSubscriptionPlan(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	var in struct {
		PlanID string `json:"planId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	in.PlanID = strings.TrimSpace(in.PlanID)
	if in.PlanID == "" {
		writeJSON(w, 400, map[string]string{"error": "planId required"})
		return
	}
	var exists string
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT id FROM settings_plans WHERE id=?`, in.PlanID).Scan(&exists); err != nil {
		writeJSON(w, 404, map[string]string{"error": "plano não encontrado"})
		return
	}
	row, _ := s.getSubscription(r.Context(), u.ID)
	row.UserID = u.ID
	row.PlanID = in.PlanID
	if row.Status == "" {
		row.Status = "inactive"
	}
	if err := s.upsertSubscription(r.Context(), row); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, row)
}

// ---------------- Checkout ----------------

func (s *server) handleCreateCheckout(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	cfg, err := s.loadBillingConfig(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if cfg.SecretKey == "" {
		writeJSON(w, 412, map[string]string{"error": "pagamentos não configurados — peça ao admin para cadastrar a chave Stripe em Configurações → Pagamentos."})
		return
	}
	// Resolve the active plan (name + unit price). Without an active plan
	// the checkout cannot decide what to charge.
	planID, _ := s.settings.getKV(r.Context(), "active_plan_id")
	// Se o usuário já tem uma assinatura vinculada a um plano, usa esse.
	if sub, _ := s.getSubscription(r.Context(), u.ID); sub.PlanID != "" {
		planID = sub.PlanID
	}
	// Fallback: usa o primeiro plano cadastrado ("Plano #1"). Todo cliente
	// novo é vinculado a ele automaticamente, então não precisamos exigir
	// um "plano ativo" definido manualmente.
	if planID == "" {
		_ = s.db.QueryRowContext(r.Context(),
			`SELECT id FROM settings_plans ORDER BY created_at ASC LIMIT 1`).Scan(&planID)
	}
	if planID == "" {
		writeJSON(w, 400, map[string]string{"error": "nenhum plano cadastrado — crie um plano em Configurações → Planos antes de cobrar."})
		return
	}
	var planJSON string
	if err := s.db.QueryRowContext(r.Context(), `SELECT data FROM settings_plans WHERE id=?`, planID).Scan(&planJSON); err != nil {
		writeJSON(w, 400, map[string]string{"error": "plano inválido"})
		return
	}
	var plan struct {
		Nome    string  `json:"nome"`
		Valor   float64 `json:"valor"`
		Periodo string  `json:"periodo"`
	}
	_ = json.Unmarshal([]byte(planJSON), &plan)
	if plan.Valor <= 0 {
		writeJSON(w, 400, map[string]string{"error": "plano sem valor configurado"})
		return
	}
	// Body: { quantity, successUrl, cancelUrl }
	var in struct {
		Quantity   int    `json:"quantity"`
		SuccessURL string `json:"successUrl"`
		CancelURL  string `json:"cancelUrl"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Quantity < 1 {
		in.Quantity = 1
	}
	if in.SuccessURL == "" {
		in.SuccessURL = inferOrigin(r) + "/billing?status=success"
	}
	if in.CancelURL == "" {
		in.CancelURL = inferOrigin(r) + "/billing?status=cancel"
	}
	interval := stripeIntervalFor(plan.Periodo)

	// Build the Stripe Checkout Session via REST. Using price_data lets us
	// avoid pre-creating products/prices in the Stripe dashboard.
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("success_url", in.SuccessURL)
	form.Set("cancel_url", in.CancelURL)
	form.Set("customer_email", u.Email)
	form.Set("client_reference_id", u.ID)
	form.Set("metadata[user_id]", u.ID)
	form.Set("metadata[plan_id]", planID)
	form.Set("subscription_data[metadata][user_id]", u.ID)
	form.Set("subscription_data[metadata][plan_id]", planID)
	form.Set("line_items[0][quantity]", strconv.Itoa(in.Quantity))
	form.Set("line_items[0][price_data][currency]", cfg.Currency)
	form.Set("line_items[0][price_data][unit_amount]", strconv.Itoa(int(plan.Valor*100+0.5)))
	form.Set("line_items[0][price_data][recurring][interval]", interval)
	productName := plan.Nome
	if productName == "" {
		productName = "Plano"
	}
	form.Set("line_items[0][price_data][product_data][name]", productName)

	body, status, err := stripeCall(r.Context(), cfg.SecretKey, "POST", "/v1/checkout/sessions", form)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	if status >= 400 {
		writeJSON(w, status, map[string]any{"error": "stripe", "detail": json.RawMessage(body)})
		return
	}
	var session struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	_ = json.Unmarshal(body, &session)
	if session.URL == "" {
		writeJSON(w, 502, map[string]string{"error": "Stripe não retornou URL de checkout"})
		return
	}
	writeJSON(w, 200, map[string]string{"url": session.URL, "id": session.ID})
}

func stripeIntervalFor(p string) string {
	switch strings.ToLower(p) {
	case "anual", "yearly", "year":
		return "year"
	case "trimestral", "semestral":
		// Stripe only supports day/week/month/year; we map quarterly and
		// semi-annual to "month" + the period_count would be needed. To keep
		// things simple here we fall back to monthly — admin can configure
		// custom prices in Stripe dashboard for irregular intervals.
		return "month"
	default:
		return "month"
	}
}

func inferOrigin(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" {
		return o
	}
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Host, "localhost") {
		scheme = "https"
	} else if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// stripeCall issues a form-encoded request to the Stripe REST API. Returns
// the raw body and HTTP status — the caller decides how to parse it.
func stripeCall(ctx context.Context, secret, method, path string, form url.Values) ([]byte, int, error) {
	endpoint := "https://api.stripe.com" + path
	var body io.Reader
	if form != nil && method != http.MethodGet {
		body = strings.NewReader(form.Encode())
	} else if form != nil {
		endpoint += "?" + form.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(secret, "")
	if form != nil && method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Stripe-Version", "2024-06-20")
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return b, resp.StatusCode, nil
}

// ---------------- Webhook ----------------

func (s *server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	cfg, _ := s.loadBillingConfig(r.Context())
	if cfg.WebhookSecret == "" {
		http.Error(w, "webhook secret not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if err := verifyStripeSignature(body, r.Header.Get("Stripe-Signature"), cfg.WebhookSecret); err != nil {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	var evt struct {
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := s.applyStripeEvent(r.Context(), evt.Type, evt.Data.Object); err != nil {
		// 200 anyway to avoid retry storm on a persistent server bug; log only.
		fmt.Printf("[billing] apply event %s failed: %v\n", evt.Type, err)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// verifyStripeSignature implements the Stripe signed-payload check (v1 scheme)
// without pulling in the official SDK. Tolerance: 5 minutes.
func verifyStripeSignature(payload []byte, header, secret string) error {
	if header == "" {
		return errors.New("missing Stripe-Signature header")
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			sigs = append(sigs, kv[1])
		}
	}
	if ts == "" || len(sigs) == 0 {
		return errors.New("malformed signature header")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return err
	}
	if abs(time.Now().Unix()-tsInt) > 300 {
		return errors.New("timestamp out of tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, sig := range sigs {
		if hmac.Equal([]byte(expected), []byte(sig)) {
			return nil
		}
	}
	return errors.New("signature mismatch")
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// applyStripeEvent maps the subset of events we care about onto our
// billing_subscriptions table. Unknown events are ignored.
func (s *server) applyStripeEvent(ctx context.Context, evtType string, obj json.RawMessage) error {
	switch evtType {
	case "checkout.session.completed":
		var sess struct {
			ID            string            `json:"id"`
			Customer      string            `json:"customer"`
			Subscription  string            `json:"subscription"`
			ClientRef     string            `json:"client_reference_id"`
			Metadata      map[string]string `json:"metadata"`
			PaymentStatus string            `json:"payment_status"`
		}
		if err := json.Unmarshal(obj, &sess); err != nil {
			return err
		}
		userID := sess.ClientRef
		if userID == "" {
			userID = sess.Metadata["user_id"]
		}
		if userID == "" {
			return nil
		}
		row, _ := s.getSubscription(ctx, userID)
		row.UserID = userID
		row.StripeCustomer = sess.Customer
		row.StripeSub = sess.Subscription
		if pid := sess.Metadata["plan_id"]; pid != "" {
			row.PlanID = pid
		}
		if sess.PaymentStatus == "paid" || sess.Subscription != "" {
			row.Status = "active"
		}
		return s.upsertSubscription(ctx, row)
	case "customer.subscription.updated", "customer.subscription.created":
		var sub struct {
			ID               string            `json:"id"`
			Customer         string            `json:"customer"`
			Status           string            `json:"status"`
			CurrentPeriodEnd int64             `json:"current_period_end"`
			Metadata         map[string]string `json:"metadata"`
			Items            struct {
				Data []struct {
					Quantity int `json:"quantity"`
				} `json:"data"`
			} `json:"items"`
		}
		if err := json.Unmarshal(obj, &sub); err != nil {
			return err
		}
		userID := sub.Metadata["user_id"]
		if userID == "" {
			return nil
		}
		row, _ := s.getSubscription(ctx, userID)
		row.UserID = userID
		row.StripeCustomer = sub.Customer
		row.StripeSub = sub.ID
		row.Status = sub.Status
		row.CurrentPeriodEnd = sub.CurrentPeriodEnd
		if len(sub.Items.Data) > 0 && sub.Items.Data[0].Quantity > 0 {
			row.Quantity = sub.Items.Data[0].Quantity
		}
		if pid := sub.Metadata["plan_id"]; pid != "" {
			row.PlanID = pid
		}
		return s.upsertSubscription(ctx, row)
	case "customer.subscription.deleted":
		var sub struct {
			ID       string            `json:"id"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(obj, &sub); err != nil {
			return err
		}
		userID := sub.Metadata["user_id"]
		if userID == "" {
			return nil
		}
		row, _ := s.getSubscription(ctx, userID)
		row.UserID = userID
		row.Status = "canceled"
		return s.upsertSubscription(ctx, row)
	case "invoice.payment_failed":
		var inv struct {
			Customer     string `json:"customer"`
			Subscription string `json:"subscription"`
		}
		if err := json.Unmarshal(obj, &inv); err != nil {
			return err
		}
		if inv.Subscription == "" {
			return nil
		}
		_, err := s.db.ExecContext(ctx, `UPDATE billing_subscriptions SET status='past_due', updated_at=? WHERE stripe_subscription=?`, time.Now().Unix(), inv.Subscription)
		return err
	}
	return nil
}
