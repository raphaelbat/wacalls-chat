package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// scheduledMessageRow is a text message queued to be sent later by the
// background worker. Kind "scheduled" is an explicit agendamento; kind
// "followup" is auto-cancelled when the customer replies or the chat closes.
type scheduledMessageRow struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	ChatJID   string `json:"chatJid"`
	Text      string `json:"text"`
	RunAt     int64  `json:"runAt"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	OwnerID   string `json:"ownerId,omitempty"`
	TenantID  string `json:"tenantId,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	SentAt    int64  `json:"sentAt,omitempty"`
}

type scheduleStore struct{ db *sql.DB }

var ErrScheduleNotFound = errors.New("scheduled message not found")

func newScheduleStore(ctx context.Context, db *sql.DB) (*scheduleStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS scheduled_messages (
		id         TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		chat_jid   TEXT NOT NULL,
		text       TEXT NOT NULL DEFAULT '',
		run_at     INTEGER NOT NULL,
		kind       TEXT NOT NULL DEFAULT 'scheduled',
		status     TEXT NOT NULL DEFAULT 'pending',
		error      TEXT NOT NULL DEFAULT '',
		owner_id   TEXT NOT NULL DEFAULT '',
		tenant_id  TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		sent_at    INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_sched_due ON scheduled_messages (status, run_at)`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_sched_chat ON scheduled_messages (session_id, chat_jid, status)`)
	return &scheduleStore{db: db}, nil
}

func (s *scheduleStore) Create(ctx context.Context, in scheduledMessageRow) (scheduledMessageRow, error) {
	in.Text = strings.TrimSpace(in.Text)
	if in.SessionID == "" || in.ChatJID == "" {
		return scheduledMessageRow{}, errors.New("session and chat required")
	}
	if in.Text == "" {
		return scheduledMessageRow{}, errors.New("text required")
	}
	if in.RunAt <= 0 {
		return scheduledMessageRow{}, errors.New("runAt required")
	}
	if in.Kind != "followup" {
		in.Kind = "scheduled"
	}
	in.ID = newID()
	in.Status = "pending"
	in.CreatedAt = time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO scheduled_messages
		(id, session_id, chat_jid, text, run_at, kind, status, error, owner_id, tenant_id, created_at, sent_at)
		VALUES (?,?,?,?,?,?,?,'',?,?,?,0)`,
		in.ID, in.SessionID, in.ChatJID, in.Text, in.RunAt, in.Kind, in.Status, in.OwnerID, in.TenantID, in.CreatedAt); err != nil {
		return scheduledMessageRow{}, err
	}
	return in, nil
}

func (s *scheduleStore) scan(rows *sql.Rows) ([]scheduledMessageRow, error) {
	defer rows.Close()
	out := []scheduledMessageRow{}
	for rows.Next() {
		var r scheduledMessageRow
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ChatJID, &r.Text, &r.RunAt, &r.Kind, &r.Status, &r.Error, &r.OwnerID, &r.TenantID, &r.CreatedAt, &r.SentAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const schedCols = `id, session_id, chat_jid, text, run_at, kind, status, error, owner_id, tenant_id, created_at, sent_at`

// List returns agendamentos for a session, optionally filtered by chat.
func (s *scheduleStore) List(ctx context.Context, sessionID, chatJID string, includeDone bool) ([]scheduledMessageRow, error) {
	q := `SELECT ` + schedCols + ` FROM scheduled_messages WHERE session_id = ?`
	args := []any{sessionID}
	if chatJID != "" {
		q += ` AND chat_jid = ?`
		args = append(args, chatJID)
	}
	if !includeDone {
		q += ` AND status = 'pending'`
	}
	q += ` ORDER BY run_at ASC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

func (s *scheduleStore) Due(ctx context.Context, now int64) ([]scheduledMessageRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+schedCols+` FROM scheduled_messages
		WHERE status = 'pending' AND run_at <= ? ORDER BY run_at ASC LIMIT 50`, now)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

func (s *scheduleStore) Get(ctx context.Context, id string) (scheduledMessageRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+schedCols+` FROM scheduled_messages WHERE id = ?`, id)
	if err != nil {
		return scheduledMessageRow{}, err
	}
	list, err := s.scan(rows)
	if err != nil {
		return scheduledMessageRow{}, err
	}
	if len(list) == 0 {
		return scheduledMessageRow{}, ErrScheduleNotFound
	}
	return list[0], nil
}

func (s *scheduleStore) MarkSent(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scheduled_messages SET status = 'sent', sent_at = ?, error = '' WHERE id = ?`, time.Now().UnixMilli(), id)
	return err
}

func (s *scheduleStore) MarkFailed(ctx context.Context, id, msg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scheduled_messages SET status = 'failed', error = ? WHERE id = ?`, msg, id)
	return err
}

func (s *scheduleStore) Cancel(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE scheduled_messages SET status = 'cancelled' WHERE id = ? AND status = 'pending'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

// CancelFollowups drops pending follow-ups for a chat — used when the customer
// replies or the atendimento is closed.
func (s *scheduleStore) CancelFollowups(ctx context.Context, sessionID, chatJID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scheduled_messages SET status = 'cancelled'
		WHERE session_id = ? AND chat_jid = ? AND kind = 'followup' AND status = 'pending'`, sessionID, chatJID)
	return err
}
