package main

import (
	"context"
	"database/sql"
	"errors"
)

// Chat lifecycle states (waiting → open → closed). Groups always live as
// "group" so they show up in their own tab regardless of human assignment.
const (
	ChatStatusWaiting = "waiting"
	ChatStatusOpen    = "open"
	ChatStatusClosed  = "closed"
	ChatStatusGroup   = "group"
)

// ChatMeta is the per-conversation metadata kept on top of the messages
// table: human-readable name, group flag and the agent that took it.
type ChatMeta struct {
	SessionID      string `json:"sessionId"`
	ChatJID        string `json:"chatJid"`
	Name           string `json:"name"`
	IsGroup        bool   `json:"isGroup"`
	Status         string `json:"status"`
	AssignedUserID string `json:"assignedUserId,omitempty"`
	QueueID        string `json:"queueId,omitempty"`
	UpdatedAt      int64  `json:"updatedAt"`
	LastReadTs     int64  `json:"lastReadTs"`
	AvatarURL      string `json:"avatarUrl,omitempty"`
}

// ChatCloseEntry is one audit row for the "Finalizar" action.
type ChatCloseEntry struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	ChatJID   string `json:"chatJid"`
	UserID    string `json:"userId"`
	UserEmail string `json:"userEmail,omitempty"`
	Reason    string `json:"reason"`
	ClosedAt  int64  `json:"closedAt"`
}

// ChatEvent is a single lifecycle/audit row for a conversation timeline:
// creation, assignment, requeue, transfer and closure. It is shown inline in
// the chat as system pills (e.g. "Conversa aberta por Admin · 17:32").
type ChatEvent struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	ChatJID   string `json:"chatJid"`
	Kind      string `json:"kind"` // created|waiting|opened|closed|requeued|transferred
	UserID    string `json:"userId,omitempty"`
	UserEmail string `json:"userEmail,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Ts        int64  `json:"ts"`
}

type chatMetaStore struct{ db *sql.DB }

func newChatMetaStore(ctx context.Context, db *sql.DB) (*chatMetaStore, error) {
	q := `CREATE TABLE IF NOT EXISTS chats (
		session_id        TEXT NOT NULL,
		chat_jid          TEXT NOT NULL,
		name              TEXT NOT NULL DEFAULT '',
		is_group          INTEGER NOT NULL DEFAULT 0,
		status            TEXT NOT NULL DEFAULT 'waiting',
		assigned_user_id  TEXT NOT NULL DEFAULT '',
		updated_at        INTEGER NOT NULL,
		PRIMARY KEY (session_id, chat_jid)
	)`
	if _, err := db.ExecContext(ctx, q); err != nil {
		return nil, err
	}
	// Best-effort migration: add last_read_ts column (ignore "duplicate column" errors).
	if _, err := db.ExecContext(ctx, `ALTER TABLE chats ADD COLUMN last_read_ts INTEGER NOT NULL DEFAULT 0`); err != nil {
		// SQLite returns "duplicate column name" if it already exists; safe to ignore.
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE chats ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''`); err != nil {
		// idempotent: ignore "duplicate column name"
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE chats ADD COLUMN queue_id TEXT NOT NULL DEFAULT ''`); err != nil {
		// idempotent: ignore "duplicate column name"
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS chat_closures (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL,
		chat_jid    TEXT NOT NULL,
		user_id     TEXT NOT NULL DEFAULT '',
		user_email  TEXT NOT NULL DEFAULT '',
		reason      TEXT NOT NULL DEFAULT '',
		closed_at   INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_chat_closures_chat
		ON chat_closures (session_id, chat_jid, closed_at DESC)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS chat_events (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL,
		chat_jid    TEXT NOT NULL,
		kind        TEXT NOT NULL,
		user_id     TEXT NOT NULL DEFAULT '',
		user_email  TEXT NOT NULL DEFAULT '',
		detail      TEXT NOT NULL DEFAULT '',
		ts          INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_chat_events_chat
		ON chat_events (session_id, chat_jid, ts)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS chat_rating_pending (
		session_id TEXT NOT NULL,
		chat_jid   TEXT NOT NULL,
		sent_at    INTEGER NOT NULL,
		PRIMARY KEY (session_id, chat_jid)
	)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS chat_ratings (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		chat_jid   TEXT NOT NULL,
		score      INTEGER NOT NULL,
		reply      TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_chat_ratings_session
		ON chat_ratings (session_id, created_at DESC)`); err != nil {
		return nil, err
	}
	return &chatMetaStore{db: db}, nil
}

func (s *chatMetaStore) Get(ctx context.Context, sessionID, jid string) (ChatMeta, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT session_id, chat_jid, name, is_group, status, assigned_user_id, updated_at, last_read_ts, avatar_url, queue_id
		FROM chats WHERE session_id=? AND chat_jid=?`, sessionID, jid)
	var m ChatMeta
	var ig int
	if err := row.Scan(&m.SessionID, &m.ChatJID, &m.Name, &ig, &m.Status, &m.AssignedUserID, &m.UpdatedAt, &m.LastReadTs, &m.AvatarURL, &m.QueueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChatMeta{}, false, nil
		}
		return ChatMeta{}, false, err
	}
	m.IsGroup = ig == 1
	return m, true, nil
}

// Upsert inserts or updates without overriding a meaningful name with empty.
func (s *chatMetaStore) Upsert(ctx context.Context, m ChatMeta) error {
	ig := 0
	if m.IsGroup {
		ig = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO chats (session_id, chat_jid, name, is_group, status, assigned_user_id, updated_at, last_read_ts, avatar_url)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(session_id, chat_jid) DO UPDATE SET
			name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE chats.name END,
			is_group = excluded.is_group,
			status = excluded.status,
			assigned_user_id = excluded.assigned_user_id,
			updated_at = excluded.updated_at,
			last_read_ts = CASE WHEN excluded.last_read_ts > chats.last_read_ts THEN excluded.last_read_ts ELSE chats.last_read_ts END,
			avatar_url = CASE WHEN excluded.avatar_url <> '' THEN excluded.avatar_url ELSE chats.avatar_url END`,
		m.SessionID, m.ChatJID, m.Name, ig, m.Status, m.AssignedUserID, m.UpdatedAt, m.LastReadTs, m.AvatarURL)
	return err
}

func (s *chatMetaStore) SetStatus(ctx context.Context, sessionID, jid, status, assignedUserID string, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET status=?, assigned_user_id=?, updated_at=? WHERE session_id=? AND chat_jid=?`,
		status, assignedUserID, now, sessionID, jid)
	return err
}

// SetAssignment sets status, assigned operator and queue in a single update.
// Empty userID / queueID clear the respective columns. Used by the
// "Abrir atendimento" dialog so an admin can route a brand-new conversation
// to a specific operator + queue in one shot.
func (s *chatMetaStore) SetAssignment(ctx context.Context, sessionID, jid, status, assignedUserID, queueID string, now int64) error {
	// Ensure the row exists so the UPDATE has something to touch.
	if _, ok, _ := s.Get(ctx, sessionID, jid); !ok {
		m := ChatMeta{SessionID: sessionID, ChatJID: jid, Status: status, AssignedUserID: assignedUserID, QueueID: queueID, UpdatedAt: now}
		if err := s.Upsert(ctx, m); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET status=?, assigned_user_id=?, queue_id=?, updated_at=? WHERE session_id=? AND chat_jid=?`,
		status, assignedUserID, queueID, now, sessionID, jid)
	return err
}

func (s *chatMetaStore) SetName(ctx context.Context, sessionID, jid, name string, now int64) error {
	if name == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET name=?, updated_at=? WHERE session_id=? AND chat_jid=?`, name, now, sessionID, jid)
	return err
}

// SetAvatar overrides the stored avatar URL for a chat. Pass "" to clear it.
func (s *chatMetaStore) SetAvatar(ctx context.Context, sessionID, jid, avatarURL string, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET avatar_url=?, updated_at=? WHERE session_id=? AND chat_jid=?`, avatarURL, now, sessionID, jid)
	return err
}

// Delete removes the chat metadata row and any related audit rows
// (closures + events). The actual message rows are wiped by the caller via
// messageStore.DeleteChat — kept separate so the two stores stay decoupled.
func (s *chatMetaStore) Delete(ctx context.Context, sessionID, jid string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM chat_events   WHERE session_id=? AND chat_jid=?`,
		`DELETE FROM chat_closures WHERE session_id=? AND chat_jid=?`,
		`DELETE FROM chats         WHERE session_id=? AND chat_jid=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, sessionID, jid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *chatMetaStore) ListBySession(ctx context.Context, sessionID string) (map[string]ChatMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id, chat_jid, name, is_group, status, assigned_user_id, updated_at, last_read_ts, avatar_url, queue_id FROM chats WHERE session_id=?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ChatMeta{}
	for rows.Next() {
		var m ChatMeta
		var ig int
		if err := rows.Scan(&m.SessionID, &m.ChatJID, &m.Name, &ig, &m.Status, &m.AssignedUserID, &m.UpdatedAt, &m.LastReadTs, &m.AvatarURL, &m.QueueID); err != nil {
			return nil, err
		}
		m.IsGroup = ig == 1
		out[m.ChatJID] = m
	}
	return out, rows.Err()
}

// ChatRating is one survey reply from a customer (1=Bom, 2=Ruim, 3=Péssimo).
type ChatRating struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	ChatJID   string `json:"chatJid"`
	Score     int    `json:"score"`
	Reply     string `json:"reply"`
	CreatedAt int64  `json:"createdAt"`
}

// SetPendingRating marks a chat as awaiting the next reply for CSAT scoring.
func (s *chatMetaStore) SetPendingRating(ctx context.Context, sessionID, jid string, ts int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO chat_rating_pending (session_id, chat_jid, sent_at)
		VALUES (?,?,?)`, sessionID, jid, ts)
	return err
}

// HasPendingRating returns true when the chat is awaiting a survey reply.
func (s *chatMetaStore) HasPendingRating(ctx context.Context, sessionID, jid string) bool {
	row := s.db.QueryRowContext(ctx, `SELECT 1 FROM chat_rating_pending WHERE session_id=? AND chat_jid=?`, sessionID, jid)
	var n int
	if err := row.Scan(&n); err != nil {
		return false
	}
	return n == 1
}

// ClearPendingRating removes any pending CSAT request for the chat.
func (s *chatMetaStore) ClearPendingRating(ctx context.Context, sessionID, jid string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM chat_rating_pending WHERE session_id=? AND chat_jid=?`, sessionID, jid)
}

// InsertRating persists a customer rating answer.
func (s *chatMetaStore) InsertRating(ctx context.Context, r ChatRating) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO chat_ratings (session_id, chat_jid, score, reply, created_at)
		VALUES (?,?,?,?,?)`, r.SessionID, r.ChatJID, r.Score, r.Reply, r.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListRatingsInRange returns ratings for the session in [from,to].
func (s *chatMetaStore) ListRatingsInRange(ctx context.Context, sessionID string, from, to int64) ([]ChatRating, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, chat_jid, score, reply, created_at
		FROM chat_ratings WHERE session_id=? AND created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC`, sessionID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatRating{}
	for rows.Next() {
		var r ChatRating
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ChatJID, &r.Score, &r.Reply, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkRead sets last_read_ts to max(current, ts). Returns the resulting meta.
func (s *chatMetaStore) MarkRead(ctx context.Context, sessionID, jid string, ts int64) (ChatMeta, error) {
	now := ts
	// Ensure the row exists so the UPDATE has something to touch.
	if _, ok, _ := s.Get(ctx, sessionID, jid); !ok {
		m := ChatMeta{SessionID: sessionID, ChatJID: jid, Status: ChatStatusWaiting, UpdatedAt: now, LastReadTs: ts}
		if err := s.Upsert(ctx, m); err != nil {
			return ChatMeta{}, err
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE chats SET last_read_ts = CASE WHEN ? > last_read_ts THEN ? ELSE last_read_ts END WHERE session_id=? AND chat_jid=?`,
		ts, ts, sessionID, jid); err != nil {
		return ChatMeta{}, err
	}
	m, _, err := s.Get(ctx, sessionID, jid)
	return m, err
}

// InsertClosure records a "Finalizar" event for auditing.
func (s *chatMetaStore) InsertClosure(ctx context.Context, e ChatCloseEntry) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO chat_closures (session_id, chat_jid, user_id, user_email, reason, closed_at)
		VALUES (?, ?, ?, ?, ?, ?)`, e.SessionID, e.ChatJID, e.UserID, e.UserEmail, e.Reason, e.ClosedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListClosures returns closure entries newest first.
func (s *chatMetaStore) ListClosures(ctx context.Context, sessionID, jid string, limit int) ([]ChatCloseEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, chat_jid, user_id, user_email, reason, closed_at
		FROM chat_closures WHERE session_id=? AND chat_jid=? ORDER BY closed_at DESC LIMIT ?`, sessionID, jid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatCloseEntry{}
	for rows.Next() {
		var e ChatCloseEntry
		if err := rows.Scan(&e.ID, &e.SessionID, &e.ChatJID, &e.UserID, &e.UserEmail, &e.Reason, &e.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// listClosuresInRange is used by the reports endpoint.
func (s *chatMetaStore) listClosuresInRange(ctx context.Context, sessionID string, from, to int64) ([]ChatCloseEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, chat_jid, user_id, user_email, reason, closed_at
		FROM chat_closures WHERE session_id=? AND closed_at >= ? AND closed_at <= ?
		ORDER BY closed_at DESC`, sessionID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatCloseEntry{}
	for rows.Next() {
		var e ChatCloseEntry
		if err := rows.Scan(&e.ID, &e.SessionID, &e.ChatJID, &e.UserID, &e.UserEmail, &e.Reason, &e.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertEvent appends a lifecycle event for a chat. Returns the row ID.
func (s *chatMetaStore) InsertEvent(ctx context.Context, e ChatEvent) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO chat_events (session_id, chat_jid, kind, user_id, user_email, detail, ts)
		VALUES (?,?,?,?,?,?,?)`, e.SessionID, e.ChatJID, e.Kind, e.UserID, e.UserEmail, e.Detail, e.Ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListEvents returns chat events in chronological (oldest first) order.
func (s *chatMetaStore) ListEvents(ctx context.Context, sessionID, jid string, limit int) ([]ChatEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, chat_jid, kind, user_id, user_email, detail, ts
		FROM chat_events WHERE session_id=? AND chat_jid=? ORDER BY ts ASC, id ASC LIMIT ?`, sessionID, jid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatEvent{}
	for rows.Next() {
		var e ChatEvent
		if err := rows.Scan(&e.ID, &e.SessionID, &e.ChatJID, &e.Kind, &e.UserID, &e.UserEmail, &e.Detail, &e.Ts); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
