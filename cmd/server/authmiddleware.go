package main

import (
	"context"
	"net/http"
	"strings"
)

// SuperAdminEmail identifies the SaaS owner account that has access to
// global Settings (plans, whitelabel, options, companies management).
// Regular admin users from other companies do NOT have this access.
const SuperAdminEmail = "admin@equipechat.com"

type ctxKey int

const (
	ctxUserKey ctxKey = iota
)

type currentUser struct {
	ID               string
	Email            string
	Name             string
	Roles            []string
	CompanyName      string
	CPF              string
	Active           bool
	SignatureEnabled bool
	Signature        string
	AvatarURL        string
	Permissions      []string
	// ParentID is the tenant root this user belongs to ("" when the user
	// is itself a tenant root or the super-admin).
	ParentID string
}

func (u *currentUser) HasRole(r string) bool {
	if u == nil {
		return false
	}
	for _, x := range u.Roles {
		if x == r {
			return true
		}
	}
	return false
}

func (u *currentUser) IsAdmin() bool { return u.HasRole(RoleAdmin) }

// IsSuperAdmin returns true only for the SaaS owner account. The check is
// based on the immutable seeded email so it cannot be escalated by simply
// granting the admin role to another user.
func (u *currentUser) IsSuperAdmin() bool {
	if u == nil {
		return false
	}
	return strings.EqualFold(u.Email, SuperAdminEmail) && u.IsAdmin()
}

// TenantID returns the identifier of the tenant the user belongs to. For
// tenant roots (top-level signups) it returns their own ID; for sub-users
// it returns the parent's ID. Multi-tenant queries should use this to
// guarantee isolation between companies.
func (u *currentUser) TenantID() string {
	if u == nil {
		return ""
	}
	if u.ParentID != "" {
		return u.ParentID
	}
	return u.ID
}

func currentUserFrom(ctx context.Context) *currentUser {
	if v, ok := ctx.Value(ctxUserKey).(*currentUser); ok {
		return v
	}
	return nil
}

func currentUserFromReq(r *http.Request) *currentUser {
	return currentUserFrom(r.Context())
}

// resolveUser returns the authenticated user from the session cookie or nil.
func (s *server) resolveUser(r *http.Request) *currentUser {
	c, err := r.Cookie(authCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.auth.UserByToken(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	return &currentUser{
		ID:               u.ID,
		Email:            u.Email,
		Name:             u.Name,
		Roles:            u.Roles,
		CompanyName:      u.CompanyName,
		CPF:              u.CPF,
		Active:           u.Active,
		SignatureEnabled: u.SignatureEnabled,
		Signature:        u.Signature,
		AvatarURL:        u.AvatarURL,
		Permissions:      u.Permissions,
		ParentID:         u.ParentID,
	}
}

// requireAuth wraps a handler so it runs only for authenticated requests.
func (s *server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.resolveUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, u)
		h.ServeHTTP(w, r.WithContext(ctx))
	}
}

// requireAdmin enforces the admin role.
func (s *server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := currentUserFromReq(r)
		if u == nil || !u.IsAdmin() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

// requireSuperAdmin restricts an endpoint to the SaaS owner account.
func (s *server) requireSuperAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := currentUserFromReq(r)
		if u == nil || !u.IsSuperAdmin() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "acesso restrito ao administrador do SaaS"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

// secureCookie returns true when we should mark cookies as Secure.
func secureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	proto := strings.ToLower(r.Header.Get("X-Forwarded-Proto"))
	return proto == "https"
}

func setAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureCookie(r),
		MaxAge:   int(tokenTTL.Seconds()),
	})
}

func clearAuthCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureCookie(r),
		MaxAge:   -1,
	})
}
