package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// tagRow is a labelable marker that operators can later attach to chats or
// contacts. Tags are scoped per-owner for non-admin users, mirroring queues.
type tagRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	OwnerID   string `json:"ownerId,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

type tagStore struct{ db *sql.DB }

var ErrTagNotFound = errors.New("tag not found")

func newTagStore(ctx context.Context, db *sql.DB) (*tagStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS tags (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		color      TEXT NOT NULL DEFAULT '#57adf8',
		owner_id   TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS contact_tags (
		session_id TEXT NOT NULL,
		chat_jid   TEXT NOT NULL,
		tag_id     TEXT NOT NULL,
		owner_id   TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		PRIMARY KEY (session_id, chat_jid, tag_id)
	)`); err != nil {
		return nil, err
	}
	return &tagStore{db: db}, nil
}

func (s *tagStore) List(ctx context.Context, ownerID string, isAdmin bool) ([]tagRow, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if isAdmin {
		rows, err = s.db.QueryContext(ctx, `SELECT id, name, color, owner_id, created_at FROM tags ORDER BY created_at ASC`)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT id, name, color, owner_id, created_at FROM tags WHERE owner_id = ? OR owner_id = '' ORDER BY created_at ASC`, ownerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []tagRow{}
	for rows.Next() {
		var r tagRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Color, &r.OwnerID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *tagStore) Create(ctx context.Context, name, color, ownerID string) (tagRow, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tagRow{}, errors.New("name required")
	}
	if color == "" {
		color = "#57adf8"
	}
	r := tagRow{ID: newID(), Name: name, Color: color, OwnerID: ownerID, CreatedAt: time.Now().Unix()}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO tags (id, name, color, owner_id, created_at) VALUES (?,?,?,?,?)`,
		r.ID, r.Name, r.Color, r.OwnerID, r.CreatedAt); err != nil {
		return tagRow{}, err
	}
	return r, nil
}

func (s *tagStore) Update(ctx context.Context, id, name, color string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tags SET name = ?, color = ? WHERE id = ?`, strings.TrimSpace(name), color, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTagNotFound
	}
	return nil
}

func (s *tagStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	return err
}

func (s *tagStore) Get(ctx context.Context, id string) (tagRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, color, owner_id, created_at FROM tags WHERE id = ?`, id)
	var r tagRow
	if err := row.Scan(&r.ID, &r.Name, &r.Color, &r.OwnerID, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tagRow{}, ErrTagNotFound
		}
		return tagRow{}, err
	}
	return r, nil
}

// ListForChat returns the tag definitions currently attached to a chat,
// ordered by attach time.
func (s *tagStore) ListForChat(ctx context.Context, sessionID, chatJID string) ([]tagRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, t.color, t.owner_id, t.created_at
		FROM contact_tags ct
		JOIN tags t ON t.id = ct.tag_id
		WHERE ct.session_id = ? AND ct.chat_jid = ?
		ORDER BY ct.created_at ASC`, sessionID, chatJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []tagRow{}
	for rows.Next() {
		var r tagRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Color, &r.OwnerID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *tagStore) AttachToChat(ctx context.Context, sessionID, chatJID, tagID, ownerID string) error {
	if strings.TrimSpace(tagID) == "" {
		return errors.New("tagId required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO contact_tags (session_id, chat_jid, tag_id, owner_id, created_at) VALUES (?,?,?,?,?)`,
		sessionID, chatJID, tagID, ownerID, time.Now().Unix())
	return err
}

func (s *tagStore) DetachFromChat(ctx context.Context, sessionID, chatJID, tagID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM contact_tags WHERE session_id = ? AND chat_jid = ? AND tag_id = ?`,
		sessionID, chatJID, tagID)
	return err
}
