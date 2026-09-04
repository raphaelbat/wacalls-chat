package main

import (
	"context"
	"database/sql"
)

// MessageRow mirrors a row in the `messages` table. Times are unix
// milliseconds; `FromMe` is 0/1.
type MessageRow struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	ChatJID    string `json:"chatJid"`
	SenderJID  string `json:"senderJid"`
	FromMe     bool   `json:"fromMe"`
	Ts         int64  `json:"ts"`
	Kind       string `json:"kind"`
	Body       string `json:"body"`
	MediaMime  string `json:"mediaMime,omitempty"`
	MediaURL   string `json:"mediaUrl,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	FileSize   int64  `json:"fileSize,omitempty"`
	QuotedID   string `json:"quotedId,omitempty"`
	SenderName string `json:"senderName,omitempty"`
	Edited     bool   `json:"edited,omitempty"`
	Deleted    bool   `json:"deleted,omitempty"`
}

// ChatSummary is the per-conversation row returned by ListChats.
type ChatSummary struct {
	ChatJID     string `json:"chatJid"`
	LastMessage string `json:"lastMessage"`
	LastKind    string `json:"lastKind"`
	LastTs      int64  `json:"lastTs"`
	LastFromMe  bool   `json:"lastFromMe"`
	Count       int    `json:"count"`
	// Filled in by the API layer from chatMetaStore (best-effort).
	Name           string `json:"name,omitempty"`
	IsGroup        bool   `json:"isGroup,omitempty"`
	Status         string `json:"status,omitempty"`
	AssignedUserID string `json:"assignedUserId,omitempty"`
	Unread         int    `json:"unread"`
	LastReadTs     int64  `json:"lastReadTs"`
	AvatarURL      string `json:"avatarUrl,omitempty"`
}

type messageStore struct{ db *sql.DB }

func newMessageStore(ctx context.Context, db *sql.DB) (*messageStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS messages (
			id          TEXT NOT NULL,
			session_id  TEXT NOT NULL,
			chat_jid    TEXT NOT NULL,
			sender_jid  TEXT NOT NULL,
			from_me     INTEGER NOT NULL,
			ts          INTEGER NOT NULL,
			kind        TEXT NOT NULL,
			body        TEXT NOT NULL DEFAULT '',
			media_mime  TEXT NOT NULL DEFAULT '',
			quoted_id   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (session_id, id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_ts
			ON messages (session_id, chat_jid, ts DESC)`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, err
		}
	}
	// Additive migrations; ignore errors when columns already exist.
	for _, q := range []string{
		`ALTER TABLE messages ADD COLUMN edited INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN media_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN file_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN file_size INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN sender_name TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = db.ExecContext(ctx, q)
	}
	return &messageStore{db: db}, nil
}

// Insert stores a single message (idempotent on (session_id, id)).
func (s *messageStore) Insert(ctx context.Context, m MessageRow) error {
	fromMe := 0
	if m.FromMe {
		fromMe = 1
	}
	// Use UPSERT so subsequent inserts for the same (session_id, id) — e.g.
	// the whatsmeow self-echo that follows a media upload — never clobber
	// the already-persisted media_url/file_name/file_size with empty values.
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages
		(id, session_id, chat_jid, sender_jid, from_me, ts, kind, body, media_mime, quoted_id, media_url, file_name, file_size, sender_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, id) DO UPDATE SET
			chat_jid    = excluded.chat_jid,
			sender_jid  = excluded.sender_jid,
			from_me     = excluded.from_me,
			ts          = excluded.ts,
			kind        = excluded.kind,
			body        = CASE WHEN excluded.body       != '' THEN excluded.body       ELSE messages.body       END,
			media_mime  = CASE WHEN excluded.media_mime != '' THEN excluded.media_mime ELSE messages.media_mime END,
			quoted_id   = CASE WHEN excluded.quoted_id  != '' THEN excluded.quoted_id  ELSE messages.quoted_id  END,
			media_url   = CASE WHEN excluded.media_url  != '' THEN excluded.media_url  ELSE messages.media_url  END,
			file_name   = CASE WHEN excluded.file_name  != '' THEN excluded.file_name  ELSE messages.file_name  END,
			file_size   = CASE WHEN excluded.file_size  > 0  THEN excluded.file_size   ELSE messages.file_size  END,
			sender_name = CASE WHEN excluded.sender_name != '' THEN excluded.sender_name ELSE messages.sender_name END`,
		m.ID, m.SessionID, m.ChatJID, m.SenderJID, fromMe, m.Ts, m.Kind, m.Body, m.MediaMime, m.QuotedID, m.MediaURL, m.FileName, m.FileSize, m.SenderName)
	return err
}

// ListChats returns one row per conversation, ordered by recency.
func (s *messageStore) ListChats(ctx context.Context, sessionID string) ([]ChatSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT chat_jid, body, kind, ts, from_me,
			       ROW_NUMBER() OVER (PARTITION BY chat_jid ORDER BY ts DESC) AS rn,
			       COUNT(*) OVER (PARTITION BY chat_jid) AS cnt
			FROM messages WHERE session_id = ?
		)
		SELECT chat_jid, body, kind, ts, from_me, cnt
		FROM ranked WHERE rn = 1
		ORDER BY ts DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatSummary{}
	for rows.Next() {
		var c ChatSummary
		var fromMe int
		if err := rows.Scan(&c.ChatJID, &c.LastMessage, &c.LastKind, &c.LastTs, &fromMe, &c.Count); err != nil {
			return nil, err
		}
		c.LastFromMe = fromMe == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns a single message by composite key (sessionID, id).
func (s *messageStore) Get(ctx context.Context, sessionID, id string) (MessageRow, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, session_id, chat_jid, sender_jid, from_me, ts, kind, body, media_mime, quoted_id, edited, deleted, media_url, file_name, file_size, sender_name
		FROM messages WHERE session_id = ? AND id = ?`, sessionID, id)
	var m MessageRow
	var fromMe, edited, deleted int
	if err := row.Scan(&m.ID, &m.SessionID, &m.ChatJID, &m.SenderJID, &fromMe, &m.Ts, &m.Kind, &m.Body, &m.MediaMime, &m.QuotedID, &edited, &deleted, &m.MediaURL, &m.FileName, &m.FileSize, &m.SenderName); err != nil {
		if err == sql.ErrNoRows {
			return MessageRow{}, false, nil
		}
		return MessageRow{}, false, err
	}
	m.FromMe = fromMe == 1
	m.Edited = edited == 1
	m.Deleted = deleted == 1
	return m, true, nil
}

// UpdateBody overwrites the stored body and marks the message as edited.
func (s *messageStore) UpdateBody(ctx context.Context, sessionID, id, body string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET body = ?, edited = 1 WHERE session_id = ? AND id = ?`, body, sessionID, id)
	return err
}

// UpdateMedia attaches a downloaded media URL / filename / size to a message.
func (s *messageStore) UpdateMedia(ctx context.Context, sessionID, id, url, name string, size int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE messages SET media_url = ?, file_name = ?, file_size = ? WHERE session_id = ? AND id = ?`,
		url, name, size, sessionID, id)
	return err
}

// MarkDeleted flags a message as revoked; body is cleared.
func (s *messageStore) MarkDeleted(ctx context.Context, sessionID, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET body = '', deleted = 1 WHERE session_id = ? AND id = ?`, sessionID, id)
	return err
}

// ListMessages returns up to `limit` messages for a chat, oldest first.
// If `before` > 0, only messages with ts < before are returned.
func (s *messageStore) ListMessages(ctx context.Context, sessionID, chatJID string, limit int, before int64) ([]MessageRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	args := []any{sessionID, chatJID}
	q := `SELECT id, session_id, chat_jid, sender_jid, from_me, ts, kind, body, media_mime, quoted_id, edited, deleted, media_url, file_name, file_size, sender_name
	      FROM messages WHERE session_id = ? AND chat_jid = ?`
	if before > 0 {
		q += ` AND ts < ?`
		args = append(args, before)
	}
	q += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MessageRow{}
	for rows.Next() {
		var m MessageRow
		var fromMe, edited, deleted int
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ChatJID, &m.SenderJID, &fromMe, &m.Ts, &m.Kind, &m.Body, &m.MediaMime, &m.QuotedID, &edited, &deleted, &m.MediaURL, &m.FileName, &m.FileSize, &m.SenderName); err != nil {
			return nil, err
		}
		m.FromMe = fromMe == 1
		m.Edited = edited == 1
		m.Deleted = deleted == 1
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse so the caller gets oldest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// DeleteForSession is used when a session is deleted.
func (s *messageStore) DeleteForSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}

// DeleteChat removes every stored message for a single conversation.
func (s *messageStore) DeleteChat(ctx context.Context, sessionID, chatJID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ? AND chat_jid = ?`, sessionID, chatJID)
	return err
}

// UnreadCounts returns the number of inbound (from_me=0) messages per chat
// whose ts is greater than the chat's last_read_ts. Reads from a joined view
// against the `chats` table so the calculation lives in one place.
func (s *messageStore) UnreadCounts(ctx context.Context, sessionID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.chat_jid, COUNT(*) AS unread
		FROM messages m
		LEFT JOIN chats c ON c.session_id = m.session_id AND c.chat_jid = m.chat_jid
		WHERE m.session_id = ? AND m.from_me = 0 AND m.ts > COALESCE(c.last_read_ts, 0)
		GROUP BY m.chat_jid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var jid string
		var n int
		if err := rows.Scan(&jid, &n); err != nil {
			return nil, err
		}
		out[jid] = n
	}
	return out, rows.Err()
}

// LatestTs returns the most recent message timestamp for a chat (0 if none).
func (s *messageStore) LatestTs(ctx context.Context, sessionID, chatJID string) (int64, error) {
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(ts) FROM messages WHERE session_id=? AND chat_jid=?`, sessionID, chatJID).Scan(&ts)
	if err != nil {
		return 0, err
	}
	if ts.Valid {
		return ts.Int64, nil
	}
	return 0, nil
}

// HasPriorOutbound returns true when this chat already contains an outbound
// (operator-sent) message strictly before `beforeTs`. Used to gate the
// chatbot so it only engages on a customer's *first* contact — never after
// the operator already started the conversation.
func (s *messageStore) HasPriorOutbound(ctx context.Context, sessionID, chatJID string, beforeTs int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM messages WHERE session_id=? AND chat_jid=? AND from_me=1 AND ts<? LIMIT 1`,
		sessionID, chatJID, beforeTs,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// listForReport returns minimal rows (chat_jid + from_me + ts) for the reporting window.
func (s *messageStore) listForReport(ctx context.Context, sessionID string, from, to int64) ([]MessageRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT chat_jid, from_me, ts FROM messages
		WHERE session_id = ? AND ts >= ? AND ts <= ?`, sessionID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MessageRow{}
	for rows.Next() {
		var chatJID string
		var fromMe int
		var ts int64
		if err := rows.Scan(&chatJID, &fromMe, &ts); err != nil {
			return nil, err
		}
		out = append(out, MessageRow{ChatJID: chatJID, FromMe: fromMe == 1, Ts: ts})
	}
	return out, rows.Err()
}
