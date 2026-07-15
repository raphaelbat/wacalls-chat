package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// settingsStore persists Plans, Options, and Whitelabel using a tiny KV table
// plus a dedicated plans table. Everything stays on the existing SQLite DB so
// no Supabase/external dependency is introduced.
type settingsStore struct {
	db *sql.DB
	// activeMu serializes "set active plan" so two concurrent admins
	// cannot race to activate different plans at the same time.
	activeMu sync.Mutex
}

func newSettingsStore(ctx context.Context, db *sql.DB) (*settingsStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS settings_kv (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings_plans (
			id TEXT PRIMARY KEY,
			data TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return nil, err
		}
	}
	return &settingsStore{db: db}, nil
}

func (s *settingsStore) getKV(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings_kv WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *settingsStore) setKV(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings_kv(key,value,updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

func (s *settingsStore) listPlans(ctx context.Context) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM settings_plans ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, rows.Err()
}

func (s *settingsStore) upsertPlan(ctx context.Context, id string, data []byte) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings_plans(id,data,created_at,updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`,
		id, string(data), now, now)
	return err
}

func (s *settingsStore) deletePlan(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings_plans WHERE id=?`, id)
	return err
}

// ---------------- HTTP handlers ----------------

func (s *server) registerSettingsRoutes(mux *http.ServeMux) {
	// Read-only endpoints stay open to any authenticated user so the
	// frontend can resolve gating (features/limits) for everyone.
	mux.HandleFunc("GET /api/settings/options", s.requireAuth(s.handleGetOptions))
	mux.HandleFunc("GET /api/settings/whitelabel", s.handleGetWhitelabel) // public so login can theme
	mux.HandleFunc("GET /api/settings/plans", s.requireAuth(s.handleListPlans))
	mux.HandleFunc("GET /api/settings/active-plan", s.requireAuth(s.handleGetActivePlan))
	// Matriz de recursos do plano ativo — leitura livre (qualquer usuário
	// autenticado consulta o que está habilitado); escrita só super admin.
	mux.HandleFunc("GET /api/settings/active-plan/matrix", s.requireAuth(s.handleGetActiveMatrix))
	mux.HandleFunc("PUT /api/settings/active-plan/matrix", s.requireSuperAdmin(s.handleSetActiveMatrix))
	// Uso atual do tenant vs limites do plano ativo. Qualquer usuário
	// autenticado pode consultar para alimentar os medidores na tela de Planos.
	mux.HandleFunc("GET /api/settings/plan-usage", s.requireAuth(s.handlePlanUsage))

	// Mutating endpoints (plans, options, whitelabel, active plan) are
	// reserved to the SaaS owner account — other companies' admins must
	// not be able to change global commercial settings.
	mux.HandleFunc("PUT /api/settings/options", s.requireSuperAdmin(s.handleSetOptions))
	mux.HandleFunc("PUT /api/settings/whitelabel", s.requireSuperAdmin(s.handleSetWhitelabel))
	mux.HandleFunc("POST /api/settings/whitelabel/asset", s.requireSuperAdmin(s.handleUploadWhitelabelAsset))
	mux.HandleFunc("POST /api/settings/plans", s.requireSuperAdmin(s.handleUpsertPlan))
	mux.HandleFunc("PUT /api/settings/plans/{id}", s.requireSuperAdmin(s.handleUpsertPlan))
	mux.HandleFunc("DELETE /api/settings/plans/{id}", s.requireSuperAdmin(s.handleDeletePlan))
	mux.HandleFunc("PUT /api/settings/active-plan", s.requireSuperAdmin(s.handleSetActivePlan))
}

func (s *server) handleGetOptions(w http.ResponseWriter, r *http.Request) {
	v, err := s.settings.getKV(r.Context(), "options")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if v == "" {
		writeJSON(w, 200, map[string]any{})
		return
	}
	var obj map[string]any
	_ = json.Unmarshal([]byte(v), &obj)
	writeJSON(w, 200, obj)
}

func (s *server) handleSetOptions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "payload too large"})
		return
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.settings.setKV(r.Context(), "options", string(body)); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, probe)
}

func (s *server) handleGetWhitelabel(w http.ResponseWriter, r *http.Request) {
	v, err := s.settings.getKV(r.Context(), "whitelabel")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if v == "" {
		writeJSON(w, 200, map[string]any{})
		return
	}
	var obj map[string]any
	_ = json.Unmarshal([]byte(v), &obj)
	writeJSON(w, 200, obj)
}

func (s *server) handleSetWhitelabel(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "payload too large"})
		return
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.settings.setKV(r.Context(), "whitelabel", string(body)); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, probe)
}

// handleUploadWhitelabelAsset accepts a multipart upload `file` plus a `kind`
// form field (logoLight, logoDark, favicon, logoMobile, bgLight, bgDark, splash)
// and writes it under media/whitelabel/. Returns a cache-busted URL.
func (s *server) handleUploadWhitelabelAsset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	allowed := map[string]bool{
		"logoLight": true, "logoDark": true, "favicon": true, "logoMobile": true,
		"bgLight": true, "bgDark": true, "splash": true,
	}
	if !allowed[kind] {
		writeJSON(w, 400, map[string]string{"error": "invalid kind"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "file required"})
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".ico", ".svg":
	default:
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "unsupported file type"})
		return
	}
	dir := filepath.Join("media", "whitelabel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Wipe any previous file for the same kind regardless of extension.
	for _, e := range []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".ico", ".svg"} {
		_ = os.Remove(filepath.Join(dir, kind+e))
	}
	name := kind + ext
	full := filepath.Join(dir, name)
	dst, err := os.Create(full)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		_ = os.Remove(full)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	dst.Close()
	url := fmt.Sprintf("/api/media/whitelabel/%s?v=%d", name, time.Now().Unix())
	writeJSON(w, 200, map[string]string{"url": url, "kind": kind})
}

func (s *server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.settings.listPlans(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"plans": plans})
}

func (s *server) handleUpsertPlan(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	id, _ := obj["id"].(string)
	if pid := r.PathValue("id"); pid != "" {
		id = pid
		obj["id"] = id
	}
	if id == "" {
		id = settingsNewID()
		obj["id"] = id
	}
	final, _ := json.Marshal(obj)
	if err := s.settings.upsertPlan(r.Context(), id, final); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, obj)
}

func (s *server) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "id required"})
		return
	}
	if err := s.settings.deletePlan(r.Context(), id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetActivePlan returns { id, plan } where plan is the resolved plan
// JSON (or null if none chosen / not found / inativo).
func (s *server) handleGetActivePlan(w http.ResponseWriter, r *http.Request) {
	id, err := s.settings.getKV(r.Context(), "active_plan_id")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]any{"id": id, "plan": nil}
	var data string
	if id != "" {
		err = s.db.QueryRowContext(r.Context(), `SELECT data FROM settings_plans WHERE id=?`, id).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			data = ""
			err = nil
		}
	}
	// Fallback: quando não há plano explicitamente "ativo" (ou o ativo
	// some), assume o primeiro plano cadastrado como vigente. Regra do
	// produto: todo cliente novo é vinculado ao plano #1 do sistema.
	if data == "" {
		err = s.db.QueryRowContext(r.Context(),
			`SELECT id, data FROM settings_plans ORDER BY created_at ASC LIMIT 1`).Scan(&id, &data)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 200, resp)
			return
		}
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	resp["id"] = id
	resp["plan"] = json.RawMessage(data)
	writeJSON(w, 200, resp)
}

func (s *server) handleSetActivePlan(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4*1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "payload too large"})
		return
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	// Serialize and run inside a single SQLite transaction. Two concurrent
	// admins racing to activate different plans cannot both win: the loser
	// observes the winner's write inside the same TX and is rejected.
	s.settings.activeMu.Lock()
	defer s.settings.activeMu.Unlock()

	ctx := r.Context()
	tx, err := s.settings.db.BeginTx(ctx, nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	id := strings.TrimSpace(obj.ID)
	if id != "" {
		// Validate that the target plan exists and is not flagged as inativo.
		var data string
		if err := tx.QueryRowContext(ctx, `SELECT data FROM settings_plans WHERE id=?`, id).Scan(&data); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, 404, map[string]string{"error": "plano não encontrado"})
				return
			}
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var probe map[string]any
		_ = json.Unmarshal([]byte(data), &probe)
		if ativo, ok := probe["ativo"].(bool); ok && !ativo {
			writeJSON(w, 409, map[string]string{"error": "plano está inativo — ative-o antes de torná-lo o plano atual"})
			return
		}
	}

	// Enforce single active plan: upsert the unique KV row in the same TX.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO settings_kv(key,value,updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		"active_plan_id", id, time.Now().Unix()); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}

// ---------------- Plan quota enforcement ----------------

// QuotaError descreve uma violação de cota do plano ativo. É serializado no
// payload de respostas HTTP 402 para que o frontend monte mensagens claras.
type QuotaError struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Limit   int    `json:"limit"`
	Current int    `json:"current"`
	Message string `json:"error"`
	Code    string `json:"code"`
	Upgrade bool   `json:"upgrade"`
}

func (q *QuotaError) Error() string { return q.Message }

// writeQuotaError escreve a resposta padronizada 402 Payment Required com o
// payload estruturado que o frontend consome em handleQuotaResponse.
func writeQuotaError(w http.ResponseWriter, q *QuotaError) {
	if q == nil {
		return
	}
	q.Code = "quota_exceeded"
	q.Upgrade = true
	writeJSON(w, http.StatusPaymentRequired, q)
}

// activePlanLimits returns the limits encoded in the active plan. When no
// plan is set (or the active plan is inativo / missing) every limit is 0
// meaning "unlimited" — same convention as the UI.
func (s *server) activePlanLimits(ctx context.Context) (usuarios, conexoes, filas int, features map[string]bool) {
	features = map[string]bool{}
	id, err := s.settings.getKV(ctx, "active_plan_id")
	if err != nil || id == "" {
		return 0, 0, 0, features
	}
	var data string
	if err := s.settings.db.QueryRowContext(ctx, `SELECT data FROM settings_plans WHERE id=?`, id).Scan(&data); err != nil {
		return 0, 0, 0, features
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return 0, 0, 0, features
	}
	if ativo, ok := obj["ativo"].(bool); ok && !ativo {
		return 0, 0, 0, features
	}
	asInt := func(v any) int {
		switch x := v.(type) {
		case float64:
			return int(x)
		case int:
			return x
		}
		return 0
	}
	usuarios = asInt(obj["usuarios"])
	conexoes = asInt(obj["conexoes"])
	filas = asInt(obj["filas"])
	if m, ok := obj["recursos"].(map[string]any); ok {
		for k, v := range m {
			if b, ok := v.(bool); ok {
				features[k] = b
			}
		}
	}
	return usuarios, conexoes, filas, features
}

// enforceQuota checks whether `current+1` would exceed the plan limit for the
// given resource kind. Returns a *QuotaError when the limit is hit
// (zero/negative limit = unlimited, returns nil).
func (s *server) enforceQuota(ctx context.Context, kind string, current int) *QuotaError {
	usuarios, conexoes, filas, _ := s.activePlanLimits(ctx)
	var limit int
	var label string
	switch kind {
	case "usuarios":
		limit, label = usuarios, "usuários"
	case "conexoes":
		limit, label = conexoes, "conexões"
	case "filas":
		limit, label = filas, "filas"
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
			Message: fmt.Sprintf("Limite do plano atingido: %d %s permitidas (uso atual: %d). Faça upgrade do plano para liberar mais.", limit, label, current),
		}
	}
	return nil
}

func settingsNewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// handleGetActiveMatrix devolve { id, recursos } do plano atualmente ativo.
// recursos é um mapa label->bool. Retorna {} quando não há plano ativo.
func (s *server) handleGetActiveMatrix(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := s.settings.getKV(ctx, "active_plan_id")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := map[string]any{"id": id, "recursos": map[string]bool{}}
	if id == "" {
		writeJSON(w, 200, out)
		return
	}
	var data string
	err = s.settings.db.QueryRowContext(ctx, `SELECT data FROM settings_plans WHERE id=?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, out)
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var obj map[string]any
	_ = json.Unmarshal([]byte(data), &obj)
	if m, ok := obj["recursos"].(map[string]any); ok {
		rec := map[string]bool{}
		for k, v := range m {
			if b, ok := v.(bool); ok {
				rec[k] = b
			}
		}
		out["recursos"] = rec
	}
	writeJSON(w, 200, out)
}

// handleSetActiveMatrix salva a matriz de recursos no plano ativo. Body:
// { "recursos": { "<label>": bool, ... } }. Operação atômica sob activeMu
// para não conflitar com troca de plano ativo simultânea.
func (s *server) handleSetActiveMatrix(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "payload too large"})
		return
	}
	var in struct {
		Recursos map[string]bool `json:"recursos"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if in.Recursos == nil {
		in.Recursos = map[string]bool{}
	}
	s.settings.activeMu.Lock()
	defer s.settings.activeMu.Unlock()
	ctx := r.Context()
	id, err := s.settings.getKV(ctx, "active_plan_id")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if id == "" {
		writeJSON(w, 409, map[string]string{"error": "nenhum plano ativo definido"})
		return
	}
	tx, err := s.settings.db.BeginTx(ctx, nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	var data string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM settings_plans WHERE id=?`, id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "plano ativo não encontrado"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		writeJSON(w, 500, map[string]string{"error": "plano corrompido"})
		return
	}
	obj["recursos"] = in.Recursos
	final, _ := json.Marshal(obj)
	if _, err := tx.ExecContext(ctx,
		`UPDATE settings_plans SET data=?, updated_at=? WHERE id=?`,
		string(final), time.Now().Unix(), id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "recursos": in.Recursos})
}

// handlePlanUsage devolve os limites do plano ativo e o uso atual do tenant
// do solicitante. A resposta segue o formato { limits, usage } onde cada
// chave traz {usuarios, conexoes, filas}. Limite 0 significa "ilimitado".
// Super admins enxergam o uso global (todos os tenants somados).
func (s *server) handlePlanUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	usuariosLim, conexoesLim, filasLim, _ := s.activePlanLimits(ctx)

	actor := currentUserFromReq(r)
	tenantID := ""
	isSuper := false
	if actor != nil {
		tenantID = actor.TenantID()
		isSuper = actor.IsSuperAdmin()
	}

	// Conta usuários ativos do tenant (ou globais, no caso do super admin).
	usuariosUso := 0
	var users []UserRow
	var uerr error
	if isSuper || tenantID == "" {
		users, uerr = s.auth.ListUsers(ctx)
	} else {
		users, uerr = s.auth.ListUsersByTenant(ctx, tenantID)
	}
	if uerr == nil {
		for _, u := range users {
			if u.Active {
				usuariosUso++
			}
		}
	}

	// Conta conexões (sessions). Sem helper "por tenant" pronto, derivamos do
	// conjunto de usuários do tenant: qualquer sessão cujo owner pertença ao
	// tenant entra na conta. Super admin enxerga todas.
	conexoesUso := 0
	infos := s.sessions.infos()
	if isSuper || tenantID == "" {
		conexoesUso = len(infos)
	} else {
		owned := map[string]bool{}
		for _, u := range users {
			owned[u.ID] = true
		}
		for _, si := range infos {
			if owned[si.OwnerID] {
				conexoesUso++
			}
		}
	}

	// Conta filas. queueStore.List já aplica filtro por tenant.
	filasUso := 0
	if s.queues != nil {
		uid := ""
		if actor != nil {
			uid = actor.ID
		}
		if rows, err := s.queues.List(ctx, uid, tenantID, true, isSuper); err == nil {
			filasUso = len(rows)
		}
	}

	writeJSON(w, 200, map[string]any{
		"limits": map[string]int{
			"usuarios": usuariosLim,
			"conexoes": conexoesLim,
			"filas":    filasLim,
		},
		"usage": map[string]int{
			"usuarios": usuariosUso,
			"conexoes": conexoesUso,
			"filas":    filasUso,
		},
		"updatedAt": time.Now().Unix(),
	})
}
