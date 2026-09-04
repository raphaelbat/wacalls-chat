package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// transcriptRow is the searchable text extracted from an audio artifact.
// Scope is either "message" (a received/sent WhatsApp audio) or "recording"
// (a saved call recording); RefID points at the message id or call id.
type transcriptRow struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Scope     string `json:"scope"`
	RefID     string `json:"refId"`
	ChatJID   string `json:"chatJid,omitempty"`
	Text      string `json:"text"`
	Lang      string `json:"lang,omitempty"`
	Engine    string `json:"engine,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

type transcriptStore struct{ db *sql.DB }

var ErrTranscriptNotFound = errors.New("transcript not found")

func newTranscriptStore(ctx context.Context, db *sql.DB) (*transcriptStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS transcripts (
		id         TEXT PRIMARY KEY,
		session_id TEXT NOT NULL DEFAULT '',
		scope      TEXT NOT NULL,
		ref_id     TEXT NOT NULL,
		chat_jid   TEXT NOT NULL DEFAULT '',
		text       TEXT NOT NULL DEFAULT '',
		lang       TEXT NOT NULL DEFAULT '',
		engine     TEXT NOT NULL DEFAULT '',
		status     TEXT NOT NULL DEFAULT 'done',
		error      TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_transcripts_ref
		ON transcripts (scope, ref_id)`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_transcripts_chat
		ON transcripts (session_id, chat_jid)`)
	return &transcriptStore{db: db}, nil
}

// Upsert stores (or replaces) the transcript for one artifact.
func (s *transcriptStore) Upsert(ctx context.Context, in transcriptRow) (transcriptRow, error) {
	in.Scope = strings.TrimSpace(in.Scope)
	in.RefID = strings.TrimSpace(in.RefID)
	if in.Scope == "" || in.RefID == "" {
		return transcriptRow{}, errors.New("scope and refId required")
	}
	if in.Status == "" {
		in.Status = "done"
	}
	if in.ID == "" {
		in.ID = newID()
	}
	if in.CreatedAt == 0 {
		in.CreatedAt = time.Now().Unix()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO transcripts
		(id, session_id, scope, ref_id, chat_jid, text, lang, engine, status, error, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(scope, ref_id) DO UPDATE SET
			session_id = excluded.session_id,
			chat_jid   = excluded.chat_jid,
			text       = excluded.text,
			lang       = excluded.lang,
			engine     = excluded.engine,
			status     = excluded.status,
			error      = excluded.error,
			created_at = excluded.created_at`,
		in.ID, in.SessionID, in.Scope, in.RefID, in.ChatJID, in.Text, in.Lang, in.Engine, in.Status, in.Error, in.CreatedAt)
	if err != nil {
		return transcriptRow{}, err
	}
	return s.Get(ctx, in.Scope, in.RefID)
}

func scanTranscripts(rows *sql.Rows) ([]transcriptRow, error) {
	defer rows.Close()
	out := []transcriptRow{}
	for rows.Next() {
		var r transcriptRow
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Scope, &r.RefID, &r.ChatJID, &r.Text,
			&r.Lang, &r.Engine, &r.Status, &r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const transcriptCols = `id, session_id, scope, ref_id, chat_jid, text, lang, engine, status, error, created_at`

func (s *transcriptStore) Get(ctx context.Context, scope, refID string) (transcriptRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+transcriptCols+` FROM transcripts WHERE scope = ? AND ref_id = ?`, scope, refID)
	var r transcriptRow
	if err := row.Scan(&r.ID, &r.SessionID, &r.Scope, &r.RefID, &r.ChatJID, &r.Text,
		&r.Lang, &r.Engine, &r.Status, &r.Error, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return transcriptRow{}, ErrTranscriptNotFound
		}
		return transcriptRow{}, err
	}
	return r, nil
}

// ListByChat returns every transcript attached to one conversation so the
// chat UI can render them inline and filter locally.
func (s *transcriptStore) ListByChat(ctx context.Context, sessionID, chatJID string) ([]transcriptRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+transcriptCols+` FROM transcripts
		WHERE session_id = ? AND chat_jid = ? ORDER BY created_at DESC`, sessionID, chatJID)
	if err != nil {
		return nil, err
	}
	return scanTranscripts(rows)
}

// ListByScope returns transcripts of a given scope for a session (used by
// the reports page to show call-recording transcripts).
func (s *transcriptStore) ListByScope(ctx context.Context, sessionID, scope string, limit int) ([]transcriptRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+transcriptCols+` FROM transcripts
		WHERE (? = '' OR session_id = ?) AND scope = ? ORDER BY created_at DESC LIMIT ?`,
		sessionID, sessionID, scope, limit)
	if err != nil {
		return nil, err
	}
	return scanTranscripts(rows)
}

// Search does a case-insensitive substring match over transcript text.
func (s *transcriptStore) Search(ctx context.Context, sessionID, chatJID, q string, limit int) ([]transcriptRow, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []transcriptRow{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+transcriptCols+` FROM transcripts
		WHERE (? = '' OR session_id = ?)
		  AND (? = '' OR chat_jid = ?)
		  AND status = 'done'
		  AND LOWER(text) LIKE ?
		ORDER BY created_at DESC LIMIT ?`,
		sessionID, sessionID, chatJID, chatJID, "%"+strings.ToLower(q)+"%", limit)
	if err != nil {
		return nil, err
	}
	return scanTranscripts(rows)
}

func (s *transcriptStore) Delete(ctx context.Context, scope, refID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM transcripts WHERE scope = ? AND ref_id = ?`, scope, refID)
	return err
}