package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
)

type sessionRow struct {
	ID                string
	Name              string
	JID               string
	OwnerID           string
	Color             string
	IsDefault         bool
	AllowGroups       bool
	IntegrationToken  string
	QueueID           string
	RedirectMinutes   int
	FlowID            string
	ChatFlowID        string
	GreetingMessage   string
	CompletionMessage string
	OutOfHoursMessage string
	SurveyEnabled     bool
	SurveyPrompt      string
	// Cloud API (Meta Official) — mode is "whatsmeow" or "cloud".
	Mode              string
	CloudPhoneID      string
	CloudWABAID       string
	CloudTokenEnc     string
	CloudAppSecretEnc string
	CloudVerifyToken  string
}

// CloudCreds returns decrypted credentials for a session in cloud mode.
func (r sessionRow) CloudCreds() (token, appSecret string, err error) {
	if token, err = decryptSecret(r.CloudTokenEnc); err != nil {
		return
	}
	appSecret, err = decryptSecret(r.CloudAppSecretEnc)
	return
}

type sessionStore struct{ db *sql.DB }

func newSessionStore(ctx context.Context, db *sql.DB) (*sessionStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sessions (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		jid  TEXT
	)`); err != nil {
		return nil, err
	}
	// Idempotent migration: add owner_id if missing.
	if _, err := db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN owner_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			// SQLite reports duplicate column when already added; ignore.
		}
	}
	migrations := []string{
		`ALTER TABLE sessions ADD COLUMN color TEXT NOT NULL DEFAULT '#57adf8'`,
		`ALTER TABLE sessions ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN allow_groups INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN integration_token TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN queue_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN redirect_minutes INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN flow_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN chat_flow_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN greeting_message TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN completion_message TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN out_of_hours_message TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN survey_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN survey_prompt TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'whatsmeow'`,
		`ALTER TABLE sessions ADD COLUMN cloud_phone_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN cloud_waba_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN cloud_token_enc TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN cloud_app_secret_enc TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN cloud_verify_token TEXT NOT NULL DEFAULT ''`,
	}
	for _, q := range migrations {
		_, _ = db.ExecContext(ctx, q)
	}
	// Repair legacy Official API rows created by older builds: cloud-only
	// sessions sometimes kept mode='whatsmeow', so they were not restored into
	// memory and webhook messages were saved but never appeared in the chat UI.
	_, _ = db.ExecContext(ctx, `UPDATE sessions
		SET mode='cloud'
		WHERE COALESCE(mode, 'whatsmeow') <> 'cloud'
		  AND COALESCE(cloud_phone_id, '') <> ''
		  AND COALESCE(cloud_token_enc, '') <> ''
		  AND COALESCE(jid, '') = ''`)
	return &sessionStore{db: db}, nil
}

func newSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *sessionStore) list(ctx context.Context) ([]sessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(jid, ''), COALESCE(owner_id, ''),
		COALESCE(color, '#57adf8'), COALESCE(is_default, 0), COALESCE(allow_groups, 0),
		COALESCE(integration_token, ''), COALESCE(queue_id, ''), COALESCE(redirect_minutes, 0),
		COALESCE(flow_id, ''), COALESCE(chat_flow_id, ''), COALESCE(greeting_message, ''), COALESCE(completion_message, ''),
		COALESCE(out_of_hours_message, ''),
		COALESCE(survey_enabled, 0), COALESCE(survey_prompt, ''),
		COALESCE(mode, 'whatsmeow'), COALESCE(cloud_phone_id, ''), COALESCE(cloud_waba_id, ''),
		COALESCE(cloud_token_enc, ''), COALESCE(cloud_app_secret_enc, ''), COALESCE(cloud_verify_token, '')
		FROM sessions ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionRow
	for rows.Next() {
		var r sessionRow
		var isDefault, allowGroups, surveyEnabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.JID, &r.OwnerID,
			&r.Color, &isDefault, &allowGroups, &r.IntegrationToken, &r.QueueID, &r.RedirectMinutes, &r.FlowID,
			&r.ChatFlowID, &r.GreetingMessage, &r.CompletionMessage, &r.OutOfHoursMessage,
			&surveyEnabled, &r.SurveyPrompt,
			&r.Mode, &r.CloudPhoneID, &r.CloudWABAID, &r.CloudTokenEnc, &r.CloudAppSecretEnc, &r.CloudVerifyToken); err != nil {
			return nil, err
		}
		r.IsDefault = isDefault == 1
		r.AllowGroups = allowGroups == 1
		r.SurveyEnabled = surveyEnabled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// getByID returns a single session row (used by cloud API handlers).
func (s *sessionStore) getByID(ctx context.Context, id string) (sessionRow, error) {
	rows, err := s.list(ctx)
	if err != nil {
		return sessionRow{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	return sessionRow{}, sql.ErrNoRows
}

// getCloudByPhoneID resolves the local cloud session by Meta's
// metadata.phone_number_id. This is used by the public webhook so incoming
// events still land in the right inbox even if Meta was configured with an
// older callback URL containing another local session id.
func (s *sessionStore) getCloudByPhoneID(ctx context.Context, phoneID string) (sessionRow, error) {
	phoneID = strings.TrimSpace(phoneID)
	if phoneID == "" {
		return sessionRow{}, sql.ErrNoRows
	}
	rows, err := s.list(ctx)
	if err != nil {
		return sessionRow{}, err
	}
	var fallback *sessionRow
	for i := range rows {
		if strings.TrimSpace(rows[i].CloudPhoneID) != phoneID {
			continue
		}
		if rows[i].Mode == "cloud" {
			return rows[i], nil
		}
		if fallback == nil {
			fallback = &rows[i]
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return sessionRow{}, sql.ErrNoRows
}

// setCloudCreds writes cloud-mode credentials for a session. Pass empty
// strings for fields you do not want to change.
func (s *sessionStore) setCloudCreds(ctx context.Context, id, phoneID, wabaID, token, appSecret, verifyToken string) error {
	tokenEnc, err := encryptSecret(token)
	if err != nil {
		return err
	}
	appSecretEnc, err := encryptSecret(appSecret)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sessions SET
		mode='cloud',
		cloud_phone_id=?,
		cloud_waba_id=?,
		cloud_token_enc=?,
		cloud_app_secret_enc=?,
		cloud_verify_token=?
		WHERE id=?`,
		phoneID, wabaID, tokenEnc, appSecretEnc, verifyToken, id)
	return err
}

// setMode flips a session between whatsmeow and cloud transports.
func (s *sessionStore) setMode(ctx context.Context, id, mode string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET mode=? WHERE id=?`, mode, id)
	return err
}

// insert keeps the old two-arg signature so existing tests still compile.
// It creates an unowned session (visible only to admins).
func (s *sessionStore) insert(ctx context.Context, id, name string) error {
	return s.insertWithOwner(ctx, id, name, "")
}

func (s *sessionStore) insertWithOwner(ctx context.Context, id, name, ownerID string) error {
	tok := newToken()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, name, jid, owner_id, integration_token) VALUES (?, ?, NULL, ?, ?)`, id, name, ownerID, tok)
	return err
}

func (s *sessionStore) setJID(ctx context.Context, id, jid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET jid = ? WHERE id = ?`, jid, id)
	return err
}

func (s *sessionStore) delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

type sessionUpdate struct {
	Name              string
	Color             string
	IsDefault         bool
	AllowGroups       bool
	QueueID           string
	RedirectMinutes   int
	FlowID            string
	ChatFlowID        string
	GreetingMessage   string
	CompletionMessage string
	OutOfHoursMessage string
	SurveyEnabled     bool
	SurveyPrompt      string
}

func (s *sessionStore) update(ctx context.Context, id string, u sessionUpdate) error {
	def := 0
	if u.IsDefault {
		def = 1
	}
	grp := 0
	if u.AllowGroups {
		grp = 1
	}
	srv := 0
	if u.SurveyEnabled {
		srv = 1
	}
	if u.IsDefault {
		// Only one default per owner: clear other defaults belonging to same owner.
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET is_default = 0 WHERE id != ? AND owner_id = (SELECT owner_id FROM sessions WHERE id = ?)`, id, id)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sessions
		SET name = ?, color = ?, is_default = ?, allow_groups = ?, queue_id = ?, redirect_minutes = ?, flow_id = ?, chat_flow_id = ?,
		    greeting_message = ?, completion_message = ?, out_of_hours_message = ?,
		    survey_enabled = ?, survey_prompt = ?
		WHERE id = ?`,
		strings.TrimSpace(u.Name), u.Color, def, grp, u.QueueID, u.RedirectMinutes, u.FlowID, u.ChatFlowID,
		u.GreetingMessage, u.CompletionMessage, u.OutOfHoursMessage,
		srv, strings.TrimSpace(u.SurveyPrompt), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sessionStore) regenerateToken(ctx context.Context, id string) (string, error) {
	tok := newToken()
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET integration_token = ? WHERE id = ?`, tok, id)
	return tok, err
}

// setFlowID updates just the flow_id column. Used by the auto-bind path on
// Restore so we don't accidentally clear other settings while patching
// legacy rows that pre-date the flow_id column migration.
func (s *sessionStore) setFlowID(ctx context.Context, id, flowID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET flow_id = ? WHERE id = ?`, flowID, id)
	return err
}

func (s *sessionStore) setFlowIDs(ctx context.Context, id, voiceFlowID, chatFlowID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET flow_id = ?, chat_flow_id = ? WHERE id = ?`, voiceFlowID, chatFlowID, id)
	return err
}
