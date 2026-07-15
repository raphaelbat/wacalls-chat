package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	authCookieName = "wacalls_session"
	tokenTTL       = 30 * 24 * time.Hour
)

var (
	ErrEmailTaken     = errors.New("email already in use")
	ErrInvalidLogin   = errors.New("invalid email or password")
	ErrUserNotFound   = errors.New("user not found")
	ErrWeakPassword   = errors.New("password must be at least 8 characters")
	ErrInvalidEmail   = errors.New("invalid email")
	ErrLastAdminGuard = errors.New("cannot remove the last admin")
)

type UserRow struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// Name is the human-friendly display name shown in pickers and
	// appended as the WhatsApp signature. Optional — falls back to
	// companyName / email username when empty.
	Name             string   `json:"name"`
	CompanyName      string   `json:"companyName"`
	CPF              string   `json:"cpf"`
	Active           bool     `json:"active"`
	CreatedAt        int64    `json:"createdAt"`
	Roles            []string `json:"roles"`
	SignatureEnabled bool     `json:"signatureEnabled"`
	Signature        string   `json:"signature"`
	AvatarURL        string   `json:"avatarUrl"`
	Permissions      []string `json:"permissions"`
	// ParentID identifies the tenant root that owns this sub-user. Empty
	// for tenant roots (top-level signups) and for the super-admin.
	ParentID string `json:"parentId,omitempty"`
}

type authStore struct {
	db              *sql.DB
	OnTokensRevoked func(tokens []string) // optional notifier for live SSE clients
}

func newAuthStore(ctx context.Context, db *sql.DB) (*authStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			email         TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at    INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS user_roles (
			user_id TEXT NOT NULL,
			role    TEXT NOT NULL,
			PRIMARY KEY (user_id, role),
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS auth_tokens (
			token      TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, err
		}
	}
	migrations := []string{
		`ALTER TABLE users ADD COLUMN company_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN cpf TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN active INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE users ADD COLUMN signature_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN signature_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN google_sub TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`,
	}
	for _, q := range migrations {
		_, _ = db.ExecContext(ctx, q)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user_queues (
		user_id  TEXT NOT NULL,
		queue_id TEXT NOT NULL,
		PRIMARY KEY (user_id, queue_id),
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user_permissions (
		user_id    TEXT NOT NULL,
		permission TEXT NOT NULL,
		PRIMARY KEY (user_id, permission),
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user_sessions_link (
		user_id    TEXT NOT NULL,
		session_id TEXT NOT NULL,
		PRIMARY KEY (user_id, session_id),
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	)`); err != nil {
		return nil, err
	}
	return &authStore{db: db}, nil
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func normalizeEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}

func validEmail(e string) bool {
	return strings.Contains(e, "@") && len(e) >= 3 && len(e) <= 254
}

func (s *authStore) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&n)
	return n, err
}

func (s *authStore) AdminCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM user_roles WHERE role = ?`, RoleAdmin).Scan(&n)
	return n, err
}

type SignupInput struct {
	Email       string
	Password    string
	CompanyName string
	CPF         string
}

var (
	ErrCompanyRequired = errors.New("nome da empresa é obrigatório")
	ErrCPFInvalid      = errors.New("CPF deve conter 11 dígitos")
	ErrInactive        = errors.New("conta desativada, contate o administrador")
)

func sanitizeCPF(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *authStore) Signup(ctx context.Context, in SignupInput) (UserRow, error) {
	email := normalizeEmail(in.Email)
	password := in.Password
	companyName := strings.TrimSpace(in.CompanyName)
	cpf := sanitizeCPF(in.CPF)
	if !validEmail(email) {
		return UserRow{}, ErrInvalidEmail
	}
	if len(password) < 8 {
		return UserRow{}, ErrWeakPassword
	}

	count, err := s.UserCount(ctx)
	if err != nil {
		return UserRow{}, err
	}
	role := RoleUser
	if count == 0 {
		role = RoleAdmin
	}
	if role != RoleAdmin {
		if companyName == "" {
			return UserRow{}, ErrCompanyRequired
		}
		if len(cpf) != 11 {
			return UserRow{}, ErrCPFInvalid
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return UserRow{}, err
	}
	id := newID()
	now := time.Now().Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserRow{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at, company_name, cpf, active, display_name) VALUES (?,?,?,?,?,?,1,?)`,
		id, email, string(hash), now, companyName, cpf, ""); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return UserRow{}, ErrEmailTaken
		}
		return UserRow{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles (user_id, role) VALUES (?,?)`, id, role); err != nil {
		return UserRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return UserRow{}, err
	}
	return UserRow{ID: id, Email: email, CompanyName: companyName, CPF: cpf, Active: true, CreatedAt: now, Roles: []string{role}}, nil
}

func (s *authStore) Login(ctx context.Context, email, password string) (UserRow, string, error) {
	email = normalizeEmail(email)
	row := s.db.QueryRowContext(ctx, `SELECT id, email, password_hash, created_at, company_name, cpf, active, signature_enabled, signature_text, avatar_url, COALESCE(parent_id, ''), COALESCE(display_name,'') FROM users WHERE email = ?`, email)
	var u UserRow
	var hash string
	var active int
	var sigEnabled int
	if err := row.Scan(&u.ID, &u.Email, &hash, &u.CreatedAt, &u.CompanyName, &u.CPF, &active, &sigEnabled, &u.Signature, &u.AvatarURL, &u.ParentID, &u.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserRow{}, "", ErrInvalidLogin
		}
		return UserRow{}, "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return UserRow{}, "", ErrInvalidLogin
	}
	u.Active = active == 1
	u.SignatureEnabled = sigEnabled == 1
	if !u.Active {
		return UserRow{}, "", ErrInactive
	}
	roles, err := s.RolesFor(ctx, u.ID)
	if err != nil {
		return UserRow{}, "", err
	}
	u.Roles = roles
	if perms, err := s.PermissionsFor(ctx, u.ID); err == nil {
		u.Permissions = perms
	}
	token, err := s.IssueToken(ctx, u.ID)
	if err != nil {
		return UserRow{}, "", err
	}
	return u, token, nil
}

func (s *authStore) IssueToken(ctx context.Context, userID string) (string, error) {
	// Single-session policy: invalidate any existing tokens for this user
	// before issuing a new one. We capture the previous tokens first so the
	// SSE hub can push a "revoked" event to any open browser in real time.
	var prev []string
	if rows, err := s.db.QueryContext(ctx, `SELECT token FROM auth_tokens WHERE user_id = ?`, userID); err == nil {
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err == nil {
				prev = append(prev, t)
			}
		}
		rows.Close()
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = ?`, userID)
	tok := newToken()
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_tokens (token, user_id, created_at, expires_at) VALUES (?,?,?,?)`,
		tok, userID, now.Unix(), now.Add(tokenTTL).Unix())
	if err != nil {
		return "", err
	}
	if len(prev) > 0 && s.OnTokensRevoked != nil {
		go s.OnTokensRevoked(prev)
	}
	return tok, nil
}

func (s *authStore) RevokeToken(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE token = ?`, token)
	return err
}

func (s *authStore) UserByToken(ctx context.Context, token string) (UserRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.created_at, u.company_name, u.cpf, u.active, u.signature_enabled, u.signature_text, u.avatar_url, COALESCE(u.parent_id, ''), COALESCE(u.display_name,'')
		FROM users u JOIN auth_tokens t ON t.user_id = u.id
		WHERE t.token = ? AND t.expires_at > ?`, token, time.Now().Unix())
	var u UserRow
	var active int
	var sigEnabled int
	if err := row.Scan(&u.ID, &u.Email, &u.CreatedAt, &u.CompanyName, &u.CPF, &active, &sigEnabled, &u.Signature, &u.AvatarURL, &u.ParentID, &u.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserRow{}, ErrUserNotFound
		}
		return UserRow{}, err
	}
	u.Active = active == 1
	u.SignatureEnabled = sigEnabled == 1
	roles, err := s.RolesFor(ctx, u.ID)
	if err != nil {
		return UserRow{}, err
	}
	u.Roles = roles
	if perms, err := s.PermissionsFor(ctx, u.ID); err == nil {
		u.Permissions = perms
	}
	return u, nil
}

func (s *authStore) RolesFor(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role FROM user_roles WHERE user_id = ? ORDER BY role`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *authStore) ListUsers(ctx context.Context) ([]UserRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, created_at, company_name, cpf, active, signature_enabled, signature_text, avatar_url, COALESCE(parent_id, ''), COALESCE(display_name,'') FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserRow{}
	for rows.Next() {
		var u UserRow
		var active int
		var sigEnabled int
		if err := rows.Scan(&u.ID, &u.Email, &u.CreatedAt, &u.CompanyName, &u.CPF, &active, &sigEnabled, &u.Signature, &u.AvatarURL, &u.ParentID, &u.Name); err != nil {
			return nil, err
		}
		u.Active = active == 1
		u.SignatureEnabled = sigEnabled == 1
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		roles, err := s.RolesFor(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Roles = roles
		perms, err := s.PermissionsFor(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Permissions = perms
	}
	return out, nil
}

// ListUsersByTenant returns the tenant root identified by tenantID together
// with all sub-users whose parent_id matches it. Used for multi-tenant
// isolation in the admin panels so company A never sees company B users.
func (s *authStore) ListUsersByTenant(ctx context.Context, tenantID string) ([]UserRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, created_at, company_name, cpf, active, signature_enabled, signature_text, avatar_url, COALESCE(parent_id, ''), COALESCE(display_name,'') FROM users WHERE id = ? OR parent_id = ? ORDER BY created_at ASC`, tenantID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserRow{}
	for rows.Next() {
		var u UserRow
		var active, sigEnabled int
		if err := rows.Scan(&u.ID, &u.Email, &u.CreatedAt, &u.CompanyName, &u.CPF, &active, &sigEnabled, &u.Signature, &u.AvatarURL, &u.ParentID, &u.Name); err != nil {
			return nil, err
		}
		u.Active = active == 1
		u.SignatureEnabled = sigEnabled == 1
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		roles, _ := s.RolesFor(ctx, out[i].ID)
		out[i].Roles = roles
		perms, _ := s.PermissionsFor(ctx, out[i].ID)
		out[i].Permissions = perms
	}
	return out, nil
}

// ParentOf returns the parent_id column of a user, or "" if the user is a
// tenant root or does not exist.
func (s *authStore) ParentOf(ctx context.Context, userID string) (string, error) {
	var pid string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(parent_id, '') FROM users WHERE id = ?`, userID).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUserNotFound
	}
	return pid, err
}

// UpdateSignature stores agent signature settings.
func (s *authStore) UpdateSignature(ctx context.Context, userID string, enabled bool, text string) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET signature_enabled=?, signature_text=? WHERE id=?`, v, text, userID)
	return err
}

// UpdateEmail changes the user's email after verifying the current password.
func (s *authStore) UpdateEmail(ctx context.Context, userID, newEmail, currentPassword string) error {
	newEmail = normalizeEmail(newEmail)
	if !validEmail(newEmail) {
		return ErrInvalidEmail
	}
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
		return ErrInvalidLogin
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET email = ? WHERE id = ?`, newEmail, userID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrEmailTaken
		}
		return err
	}
	return nil
}

// UpdatePassword changes the user's password after verifying the current one.
func (s *authStore) UpdatePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrWeakPassword
	}
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
		return ErrInvalidLogin
	}
	nh, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(nh), userID)
	return err
}

// UpdateAvatar persists the URL of the user's avatar image.
func (s *authStore) UpdateAvatar(ctx context.Context, userID, url string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET avatar_url = ? WHERE id = ?`, url, userID)
	return err
}

func (s *authStore) SetActive(ctx context.Context, userID string, active bool) error {
	v := 0
	if active {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET active = ? WHERE id = ?`, v, userID)
	return err
}

func (s *authStore) SetRole(ctx context.Context, userID, role string, grant bool) error {
	if role != RoleAdmin && role != RoleUser {
		return errors.New("invalid role")
	}
	if !grant && role == RoleAdmin {
		n, err := s.AdminCount(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			has, _ := s.HasRole(ctx, userID, RoleAdmin)
			if has {
				return ErrLastAdminGuard
			}
		}
	}
	if grant {
		_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO user_roles (user_id, role) VALUES (?,?)`, userID, role)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ? AND role = ?`, userID, role)
	return err
}

func (s *authStore) HasRole(ctx context.Context, userID, role string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM user_roles WHERE user_id = ? AND role = ?`, userID, role).Scan(&n)
	return n > 0, err
}

func (s *authStore) DeleteUser(ctx context.Context, userID string) error {
	has, err := s.HasRole(ctx, userID, RoleAdmin)
	if err != nil {
		return err
	}
	if has {
		n, err := s.AdminCount(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastAdminGuard
		}
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	return err
}

// AdminCreateUser creates a new user from the admin panel. Unlike Signup it
// never auto-promotes to admin and lets the caller pick the role.
func (s *authStore) AdminCreateUser(ctx context.Context, email, password, companyName, cpf, role, parentID string) (UserRow, error) {
	email = normalizeEmail(email)
	companyName = strings.TrimSpace(companyName)
	cpf = sanitizeCPF(cpf)
	if !validEmail(email) {
		return UserRow{}, ErrInvalidEmail
	}
	if len(password) < 8 {
		return UserRow{}, ErrWeakPassword
	}
	if role != RoleAdmin && role != RoleUser {
		role = RoleUser
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return UserRow{}, err
	}
	id := newID()
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserRow{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at, company_name, cpf, active, parent_id) VALUES (?,?,?,?,?,?,1,?)`,
		id, email, string(hash), now, companyName, cpf, parentID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return UserRow{}, ErrEmailTaken
		}
		return UserRow{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles (user_id, role) VALUES (?,?)`, id, role); err != nil {
		return UserRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return UserRow{}, err
	}
	return UserRow{ID: id, Email: email, CompanyName: companyName, CPF: cpf, Active: true, CreatedAt: now, Roles: []string{role}, ParentID: parentID}, nil
}

// SetDisplayName updates the human-readable name used in signatures and
// operator pickers. Empty string clears the field.
func (s *authStore) SetDisplayName(ctx context.Context, userID, name string) error {
	name = strings.TrimSpace(name)
	_, err := s.db.ExecContext(ctx, `UPDATE users SET display_name = ? WHERE id = ?`, name, userID)
	return err
}

// AdminUpdateUser changes the fields the admin panel exposes. Empty fields are
// skipped (no-op). A non-empty newPassword resets the bcrypt hash.
func (s *authStore) AdminUpdateUser(ctx context.Context, userID, email, companyName, cpf, newPassword string) error {
	if email != "" {
		em := normalizeEmail(email)
		if !validEmail(em) {
			return ErrInvalidEmail
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET email = ? WHERE id = ?`, em, userID); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return ErrEmailTaken
			}
			return err
		}
	}
	if strings.TrimSpace(companyName) != "" {
		_, err := s.db.ExecContext(ctx, `UPDATE users SET company_name = ? WHERE id = ?`, strings.TrimSpace(companyName), userID)
		if err != nil {
			return err
		}
	}
	if cpf != "" {
		c := sanitizeCPF(cpf)
		_, err := s.db.ExecContext(ctx, `UPDATE users SET cpf = ? WHERE id = ?`, c, userID)
		if err != nil {
			return err
		}
	}
	if newPassword != "" {
		if len(newPassword) < 8 {
			return ErrWeakPassword
		}
		nh, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(nh), userID); err != nil {
			return err
		}
	}
	return nil
}

// QueuesFor returns queue IDs assigned to the user.
func (s *authStore) QueuesFor(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT queue_id FROM user_queues WHERE user_id = ? ORDER BY queue_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// SetUserQueues replaces the user's queue assignments atomically.
func (s *authStore) SetUserQueues(ctx context.Context, userID string, queueIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_queues WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, q := range queueIDs {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_queues (user_id, queue_id) VALUES (?,?)`, userID, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SeedAdmin ensures a default admin account exists when the database is empty.
// It only runs when there are zero users — never overwrites or resets an
// existing account. Safe to call on every startup.
func (s *authStore) SeedAdmin(ctx context.Context, email, password string) (bool, error) {
	n, err := s.UserCount(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := s.Signup(ctx, SignupInput{Email: email, Password: password}); err != nil {
		return false, err
	}
	return true, nil
}

// PermissionsFor returns the explicit permission keys granted to a user.
// Admin users implicitly have every capability — the UI should treat
// admins as fully permitted regardless of this list.
func (s *authStore) PermissionsFor(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT permission FROM user_permissions WHERE user_id = ? ORDER BY permission`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetUserPermissions replaces the user's permission set atomically.
func (s *authStore) SetUserPermissions(ctx context.Context, userID string, perms []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_permissions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, p := range perms {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_permissions (user_id, permission) VALUES (?,?)`, userID, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SessionsFor returns session IDs (connections) assigned to the user.
func (s *authStore) SessionsFor(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id FROM user_sessions_link WHERE user_id = ? ORDER BY session_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetUserSessions replaces the user's session/connection assignments atomically.
func (s *authStore) SetUserSessions(ctx context.Context, userID string, sessionIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_sessions_link WHERE user_id = ?`, userID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, sid := range sessionIDs {
		sid = strings.TrimSpace(sid)
		if sid == "" || seen[sid] {
			continue
		}
		seen[sid] = true
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_sessions_link (user_id, session_id) VALUES (?,?)`, userID, sid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
