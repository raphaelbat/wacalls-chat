package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// loginLimiter is a tiny in-memory rate limiter (per IP).
type loginLimiter struct {
	mu      sync.Mutex
	hits    map[string][]int64
	max     int
	windowS int64
}

func newLoginLimiter() *loginLimiter {
	// Janela curta para frear ataques de força bruta sem prender o usuário
	// legítimo por longos períodos após poucos erros de digitação.
	return &loginLimiter{hits: map[string][]int64{}, max: 20, windowS: 5 * 60}
}

func (l *loginLimiter) allow(ip string) bool {
	now := time.Now().Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now - l.windowS
	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if t > cutoff {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, now)
	return true
}

// reset clears the rate-limit counter for an IP (called after a successful
// login so a user who mistyped the password isn't punished afterwards).
func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, ip)
}

func clientIP(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		return strings.TrimSpace(strings.Split(f, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func (s *server) registerAuthRoutes(mux *http.ServeMux) {
	// Signup público desabilitado: cadastro de usuários é feito apenas pelo admin em /api/users.
	// mux.HandleFunc("POST /api/auth/signup", s.handleSignup)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.handleMe)
	mux.HandleFunc("GET /api/auth/stream", s.handleAuthStream)
	mux.HandleFunc("GET /api/me/signature", s.requireAuth(s.handleGetSignature))
	mux.HandleFunc("PUT /api/me/signature", s.requireAuth(s.handleSetSignature))
	mux.HandleFunc("PUT /api/me/email", s.requireAuth(s.handleUpdateEmail))
	mux.HandleFunc("PUT /api/me/password", s.requireAuth(s.handleUpdatePassword))
	mux.HandleFunc("POST /api/me/avatar", s.requireAuth(s.handleUploadAvatar))
	mux.HandleFunc("DELETE /api/me/avatar", s.requireAuth(s.handleDeleteAvatar))

	mux.HandleFunc("GET /api/users", s.requireAdmin(s.handleListUsers))
	// Lightweight directory used by chat transfer; any authed user can read it.
	mux.HandleFunc("GET /api/operators", s.requireAuth(s.handleListOperators))
	mux.HandleFunc("POST /api/users", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("PUT /api/users/{id}", s.requireAdmin(s.handleUpdateUser))
	mux.HandleFunc("POST /api/users/{id}/roles", s.requireAdmin(s.handleSetRole))
	mux.HandleFunc("GET /api/audit/roles", s.requireAdmin(s.handleRoleAuditList))
	mux.HandleFunc("DELETE /api/users/{id}", s.requireAdmin(s.handleDeleteUser))
	mux.HandleFunc("GET /api/users/{id}/queues", s.requireAdmin(s.handleListUserQueues))
	mux.HandleFunc("PUT /api/users/{id}/queues", s.requireAdmin(s.handleSetUserQueues))
	mux.HandleFunc("GET /api/users/{id}/sessions", s.requireAdmin(s.handleListUserSessions))
	mux.HandleFunc("PUT /api/users/{id}/sessions", s.requireAdmin(s.handleSetUserSessions))
	mux.HandleFunc("GET /api/users/{id}/permissions", s.requireAdmin(s.handleListUserPermissions))
	mux.HandleFunc("PUT /api/users/{id}/permissions", s.requireAdmin(s.handleSetUserPermissions))

	// Companies (SaaS owner only): cross-tenant view of every signup. Each
	// tenant admin only manages their own sub-users via /api/users.
	mux.HandleFunc("GET /api/companies", s.requireSuperAdmin(s.handleListCompanies))
	mux.HandleFunc("POST /api/companies/{id}/active", s.requireSuperAdmin(s.handleSetActive))
	mux.HandleFunc("DELETE /api/companies/{id}", s.requireSuperAdmin(s.handleDeleteUser))
}

type credentialsBody struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	CompanyName string `json:"companyName"`
	CPF         string `json:"cpf"`
}

func (s *server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var body credentialsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u, err := s.auth.Signup(r.Context(), SignupInput{
		Email:       body.Email,
		Password:    body.Password,
		CompanyName: body.CompanyName,
		CPF:         body.CPF,
	})
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrEmailTaken) {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	// O primeiro usuário (admin master) entra direto — não há como receber
	// e-mail antes de cadastrar SMTP. Para os demais exigimos confirmação
	// por código enviado ao e-mail (ou exposto no JSON em modo dev).
	isAdmin := false
	for _, role := range u.Roles {
		if role == RoleAdmin {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		if err := s.auth.SetActive(r.Context(), u.ID, false); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		dev := s.issueActivationCode(r.Context(), r, u.ID, u.Email)
		resp := map[string]any{
			"needsVerification": true,
			"email":             u.Email,
		}
		if dev != "" {
			resp["devCode"] = dev
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	token, err := s.auth.IssueToken(r.Context(), u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	setAuthCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var body credentialsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u, token, err := s.auth.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	s.loginLimit.reset(clientIP(r))
	setAuthCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(authCookieName); err == nil && c.Value != "" {
		_ = s.auth.RevokeToken(r.Context(), c.Value)
	}
	clearAuthCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := s.resolveUser(r)
	if u == nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil, "needsSignup": s.needsSignup(r)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{
		"id": u.ID, "email": u.Email, "name": u.Name, "roles": u.Roles,
		"companyName": u.CompanyName, "cpf": u.CPF, "active": u.Active,
		"signatureEnabled": u.SignatureEnabled, "signature": u.Signature,
		"avatarUrl":   u.AvatarURL,
		"permissions": u.Permissions,
		"parentId":    u.ParentID,
	}})
}

func (s *server) needsSignup(r *http.Request) bool {
	n, err := s.auth.UserCount(r.Context())
	if err != nil {
		return false
	}
	return n == 0
}

func (s *server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	users := []UserRow{}
	var err error
	// Listagem usada pelo painel "Usuários" das empresas — sempre restrita ao
	// próprio tenant para garantir o isolamento SaaS, mesmo para super-admin.
	// A visão global de empresas vive em /api/companies.
	if u != nil {
		users, err = s.auth.ListUsersByTenant(r.Context(), u.TenantID())
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// handleListCompanies returns ALL tenant roots (companies) across the SaaS.
// Used by the super-admin "Empresas" panel — different from handleListUsers,
// which is scoped to a single tenant.
func (s *server) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	all, err := s.auth.ListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]UserRow, 0, len(all))
	for _, u := range all {
		if u.ParentID == "" {
			out = append(out, u)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// assertSameTenant returns true when the actor is allowed to manage the
// target user. The super-admin can manage anyone; tenant admins can only
// touch themselves and sub-users sharing their tenant root.
func (s *server) assertSameTenant(w http.ResponseWriter, r *http.Request, targetID string) bool {
	actor := currentUserFromReq(r)
	if actor == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	if actor.IsSuperAdmin() {
		return true
	}
	tenant := actor.TenantID()
	if targetID == tenant || targetID == actor.ID {
		return true
	}
	pid, err := s.auth.ParentOf(r.Context(), targetID)
	if err != nil || pid != tenant {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return false
	}
	return true
}

// handleListOperators returns a slim {id,email,companyName} list of active
// users so any authed agent can populate the "Transferir para" picker without
// requiring admin privileges.
func (s *server) handleListOperators(w http.ResponseWriter, r *http.Request) {
	actor := currentUserFromReq(r)
	users := []UserRow{}
	var err error
	// Always restrict to the actor's tenant tree so each company only sees
	// its own operators in pickers (transfer, assign, etc.). Even the SaaS
	// super-admin is scoped here — cross-tenant listing lives under the
	// dedicated admin endpoints (e.g. handleListUsers).
	if actor != nil {
		users, err = s.auth.ListUsersByTenant(r.Context(), actor.TenantID())
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type slim struct {
		ID          string `json:"id"`
		Email       string `json:"email"`
		Name        string `json:"name,omitempty"`
		CompanyName string `json:"companyName,omitempty"`
	}
	out := make([]slim, 0, len(users))
	for _, u := range users {
		if !u.Active {
			continue
		}
		out = append(out, slim{ID: u.ID, Email: u.Email, Name: u.Name, CompanyName: u.CompanyName})
	}
	writeJSON(w, http.StatusOK, map[string]any{"operators": out})
}

func (s *server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string   `json:"email"`
		Password    string   `json:"password"`
		Name        string   `json:"name"`
		CompanyName string   `json:"companyName"`
		CPF         string   `json:"cpf"`
		Role        string   `json:"role"`
		QueueIDs    []string `json:"queueIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	// Quota: count currently active users and reject if the active plan
	// limit is reached. Inactive (suspended) accounts are not counted so
	// admins can recycle a slot by deactivating an old user.
	actor := currentUserFromReq(r)
	tenantID := ""
	if actor != nil && !actor.IsSuperAdmin() {
		tenantID = actor.TenantID()
	}
	var existing []UserRow
	var qerr error
	if tenantID == "" {
		existing, qerr = s.auth.ListUsers(r.Context())
	} else {
		existing, qerr = s.auth.ListUsersByTenant(r.Context(), tenantID)
	}
	if qerr == nil {
		active := 0
		for _, u := range existing {
			if u.Active {
				active++
			}
		}
		if qe := s.enforceQuota(r.Context(), "usuarios", active); qe != nil {
			writeQuotaError(w, qe)
			return
		}
	}
	parentID := ""
	if actor != nil && !actor.IsSuperAdmin() {
		parentID = actor.TenantID()
	}
	// Sub-usuários NÃO são empresas: eles herdam o nome/CPF da empresa
	// cadastrada no signup. Ignoramos qualquer valor enviado no body para
	// evitar que a listagem trate cada operador como uma empresa separada.
	companyName := body.CompanyName
	cpf := body.CPF
	if parentID != "" {
		if rows, e := s.auth.ListUsersByTenant(r.Context(), parentID); e == nil {
			for _, ur := range rows {
				if ur.ID == parentID {
					companyName = ur.CompanyName
					cpf = ur.CPF
					break
				}
			}
		}
	}
	u, err := s.auth.AdminCreateUser(r.Context(), body.Email, body.Password, companyName, cpf, body.Role, parentID)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrEmailTaken) {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	if len(body.QueueIDs) > 0 {
		_ = s.auth.SetUserQueues(r.Context(), u.ID, body.QueueIDs)
	}
	if strings.TrimSpace(body.Name) != "" {
		_ = s.auth.SetDisplayName(r.Context(), u.ID, body.Name)
		u.Name = strings.TrimSpace(body.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	var body struct {
		Email       string `json:"email"`
		Name        string `json:"name"`
		CompanyName string `json:"companyName"`
		CPF         string `json:"cpf"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	// Sub-usuários herdam empresa/CPF do tenant root. Só o próprio tenant
	// root (ou super-admin editando um tenant root) pode alterar esses campos.
	companyName := body.CompanyName
	cpf := body.CPF
	if pid, _ := s.auth.ParentOf(r.Context(), id); pid != "" {
		if rows, e := s.auth.ListUsersByTenant(r.Context(), pid); e == nil {
			for _, ur := range rows {
				if ur.ID == pid {
					companyName = ur.CompanyName
					cpf = ur.CPF
					break
				}
			}
		}
	}
	if err := s.auth.AdminUpdateUser(r.Context(), id, body.Email, companyName, cpf, body.NewPassword); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrEmailTaken) {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	if err := s.auth.SetDisplayName(r.Context(), id, body.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleListUserQueues(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	ids, err := s.auth.QueuesFor(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueIds": ids})
}

func (s *server) handleSetUserQueues(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	var body struct {
		QueueIDs []string `json:"queueIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.auth.SetUserQueues(r.Context(), id, body.QueueIDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleListUserSessions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	ids, err := s.auth.SessionsFor(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessionIds": ids})
}

func (s *server) handleSetUserSessions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	var body struct {
		SessionIDs []string `json:"sessionIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.auth.SetUserSessions(r.Context(), id, body.SessionIDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleListUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	perms, err := s.auth.PermissionsFor(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}

func (s *server) handleSetUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.auth.SetUserPermissions(r.Context(), id, body.Permissions); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSetRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.assertSameTenant(w, r, id) {
		return
	}
	var body struct {
		Role  string `json:"role"`
		Grant bool   `json:"grant"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	role := strings.TrimSpace(body.Role)
	if role == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role required"})
		return
	}
	actor := currentUserFromReq(r)
	if actor != nil && actor.ID == id && !body.Grant && role == "admin" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove your own admin role"})
		return
	}
	prev, _ := s.auth.HasRole(r.Context(), id, role)
	if err := s.auth.SetRole(r.Context(), id, role, body.Grant); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if prev != body.Grant {
		entry := roleAuditRow{
			TargetID:    id,
			TargetEmail: lookupUserEmail(r.Context(), s.db, id),
			Role:        role,
			Granted:     body.Grant,
			PrevGranted: prev,
		}
		if actor != nil {
			entry.ActorID = actor.ID
			entry.ActorEmail = actor.Email
		}
		if err := logRoleChange(r.Context(), s.db, entry); err != nil {
			s.log.Warn("audit log write failed", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *server) handleRoleAuditList(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	rows, err := listRoleAudit(r.Context(), s.db, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": rows})
}

func (s *server) handleGetSignature(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": u.SignatureEnabled, "text": u.Signature})
}

func (s *server) handleSetSignature(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	var body struct {
		Enabled bool   `json:"enabled"`
		Text    string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if len(body.Text) > 200 {
		body.Text = body.Text[:200]
	}
	if err := s.auth.UpdateSignature(r.Context(), u.ID, body.Enabled, body.Text); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": body.Enabled, "text": body.Text})
}

func (s *server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := currentUserFromReq(r)
	if me != nil && me.ID == id {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete your own account"})
		return
	}
	if !s.assertSameTenant(w, r, id) {
		return
	}
	if err := s.auth.DeleteUser(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Sessions/flows/messages owned by this user become orphaned.
	// They'll remain in the DB but invisible (no owner matches).
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSetActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := currentUserFromReq(r)
	var body struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if me != nil && me.ID == id && !body.Active {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot deactivate your own account"})
		return
	}
	if err := s.auth.SetActive(r.Context(), id, body.Active); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// handleUpdateEmail changes the authenticated user's email after confirming
// the current password. Returns 409 if the email is already taken.
func (s *server) handleUpdateEmail(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.auth.UpdateEmail(r.Context(), u.ID, body.Email, body.Password); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrInvalidLogin) {
			code = http.StatusUnauthorized
		} else if errors.Is(err, ErrEmailTaken) {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": normalizeEmail(body.Email)})
}

// handleUpdatePassword rotates the password after verifying the current one.
func (s *server) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	var body struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.auth.UpdatePassword(r.Context(), u.ID, body.Current, body.New); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrInvalidLogin) {
			code = http.StatusUnauthorized
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// handleUploadAvatar accepts a multipart "file" upload (max 4MB), saves it to
// media/avatars/user-<id>.<ext> and stores the public URL on the user row.
func (s *server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file required"})
		return
	}
	defer file.Close()
	if header.Size > 4*1024*1024 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "max 4MB"})
		return
	}
	ct := header.Header.Get("Content-Type")
	ext := ".png"
	switch {
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		ext = ".jpg"
	case strings.Contains(ct, "webp"):
		ext = ".webp"
	case strings.Contains(ct, "gif"):
		ext = ".gif"
	case strings.Contains(ct, "png"):
		ext = ".png"
	default:
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "only png/jpg/webp/gif"})
		return
	}
	dir := filepath.Join("media", "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Remove any previous avatar with a different extension.
	for _, e := range []string{".png", ".jpg", ".webp", ".gif"} {
		_ = os.Remove(filepath.Join(dir, "user-"+u.ID+e))
	}
	name := "user-" + u.ID + ext
	full := filepath.Join(dir, name)
	dst, err := os.Create(full)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		_ = os.Remove(full)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dst.Close()
	// Cache-bust on every upload so the browser picks up the new image.
	url := fmt.Sprintf("/api/media/avatars/%s?v=%d", name, time.Now().Unix())
	if err := s.auth.UpdateAvatar(r.Context(), u.ID, url); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatarUrl": url})
}

// handleDeleteAvatar clears the stored avatar and removes the file from disk.
func (s *server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	dir := filepath.Join("media", "avatars")
	for _, e := range []string{".png", ".jpg", ".webp", ".gif"} {
		_ = os.Remove(filepath.Join(dir, "user-"+u.ID+e))
	}
	if err := s.auth.UpdateAvatar(r.Context(), u.ID, ""); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
