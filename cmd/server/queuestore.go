package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type queueRow struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Color            string `json:"color"`
	OwnerID          string `json:"ownerId,omitempty"`
	CreatedAt        int64  `json:"createdAt"`
	OrderBot         string `json:"orderBot"`
	CloseTicket      bool   `json:"closeTicket"`
	Rotation         bool   `json:"rotation"`
	RotationInterval string `json:"rotationInterval"`
	RotationMode     string `json:"rotationMode"`
	AutoRandomize    bool   `json:"autoRandomize"`
	AgentID          string `json:"agentId"`
	Greeting         string `json:"greeting"`
}

type queueExtras struct {
	OrderBot         string
	CloseTicket      bool
	Rotation         bool
	RotationInterval string
	RotationMode     string
	AutoRandomize    bool
	AgentID          string
	Greeting         string
}

func qBool(b bool) int {
	if b {
		return 1
	}
	return 0
}

type queueStore struct{ db *sql.DB }

var ErrQueueNotFound = errors.New("queue not found")

func newQueueStore(ctx context.Context, db *sql.DB) (*queueStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS queues (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		color      TEXT NOT NULL DEFAULT '#57adf8',
		owner_id   TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	// Best-effort column adds (idempotent: ignored when already present).
	for _, stmt := range []string{
		`ALTER TABLE queues ADD COLUMN order_bot TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE queues ADD COLUMN close_ticket INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE queues ADD COLUMN rotation INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE queues ADD COLUMN rotation_interval TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE queues ADD COLUMN rotation_mode TEXT NOT NULL DEFAULT 'Random'`,
		`ALTER TABLE queues ADD COLUMN auto_randomize INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE queues ADD COLUMN agent_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE queues ADD COLUMN greeting TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = db.ExecContext(ctx, stmt)
	}
	return &queueStore{db: db}, nil
}

// List returns queues visible to a user. When superAdmin is true every
// queue is returned. When isAdmin is true the caller sees queues owned by
// any user within their tenant (identified by tenantID). Otherwise only
// queues linked via user_queues for that specific user are returned.
func (s *queueStore) List(ctx context.Context, userID, tenantID string, isAdmin, superAdmin bool) ([]queueRow, error) {
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case isAdmin || superAdmin:
		rows, err = s.db.QueryContext(ctx, `SELECT id, name, color, owner_id, created_at,
			order_bot, close_ticket, rotation, rotation_interval, rotation_mode, auto_randomize, agent_id, greeting
			FROM queues
			WHERE owner_id IN (SELECT id FROM users WHERE id = ? OR parent_id = ?)
			ORDER BY created_at ASC`, tenantID, tenantID)
	default:
		rows, err = s.db.QueryContext(ctx, `SELECT q.id, q.name, q.color, q.owner_id, q.created_at,
			q.order_bot, q.close_ticket, q.rotation, q.rotation_interval, q.rotation_mode, q.auto_randomize, q.agent_id, q.greeting
			FROM queues q
			INNER JOIN user_queues uq ON uq.queue_id = q.id
			WHERE uq.user_id = ?
			  AND q.owner_id IN (SELECT id FROM users WHERE id = ? OR parent_id = ?)
			ORDER BY q.created_at ASC`, userID, tenantID, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []queueRow{}
	for rows.Next() {
		var r queueRow
		var ct, rt, ar int
		if err := rows.Scan(&r.ID, &r.Name, &r.Color, &r.OwnerID, &r.CreatedAt,
			&r.OrderBot, &ct, &rt, &r.RotationInterval, &r.RotationMode, &ar, &r.AgentID, &r.Greeting); err != nil {
			return nil, err
		}
		r.CloseTicket = ct != 0
		r.Rotation = rt != 0
		r.AutoRandomize = ar != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *queueStore) Create(ctx context.Context, name, color, ownerID string) (queueRow, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return queueRow{}, errors.New("name required")
	}
	if color == "" {
		color = "#57adf8"
	}
	r := queueRow{ID: newID(), Name: name, Color: color, OwnerID: ownerID, CreatedAt: time.Now().Unix()}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO queues (id, name, color, owner_id, created_at) VALUES (?,?,?,?,?)`,
		r.ID, r.Name, r.Color, r.OwnerID, r.CreatedAt); err != nil {
		return queueRow{}, err
	}
	return r, nil
}

func (s *queueStore) Update(ctx context.Context, id, name, color string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE queues SET name = ?, color = ? WHERE id = ?`, strings.TrimSpace(name), color, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrQueueNotFound
	}
	return nil
}

func (s *queueStore) UpdateFull(ctx context.Context, id, name, color string, e queueExtras) error {
	if e.RotationMode == "" {
		e.RotationMode = "Random"
	}
	res, err := s.db.ExecContext(ctx, `UPDATE queues SET name = ?, color = ?,
		order_bot = ?, close_ticket = ?, rotation = ?, rotation_interval = ?, rotation_mode = ?,
		auto_randomize = ?, agent_id = ?, greeting = ?
		WHERE id = ?`,
		strings.TrimSpace(name), color,
		e.OrderBot, qBool(e.CloseTicket), qBool(e.Rotation), e.RotationInterval, e.RotationMode,
		qBool(e.AutoRandomize), e.AgentID, e.Greeting, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrQueueNotFound
	}
	return nil
}

func (s *queueStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM queues WHERE id = ?`, id)
	return err
}

func (s *queueStore) Get(ctx context.Context, id string) (queueRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, color, owner_id, created_at,
		order_bot, close_ticket, rotation, rotation_interval, rotation_mode, auto_randomize, agent_id, greeting
		FROM queues WHERE id = ?`, id)
	var r queueRow
	var ct, rt, ar int
	if err := row.Scan(&r.ID, &r.Name, &r.Color, &r.OwnerID, &r.CreatedAt,
		&r.OrderBot, &ct, &rt, &r.RotationInterval, &r.RotationMode, &ar, &r.AgentID, &r.Greeting); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return queueRow{}, ErrQueueNotFound
		}
		return queueRow{}, err
	}
	r.CloseTicket = ct != 0
	r.Rotation = rt != 0
	r.AutoRandomize = ar != 0
	return r, nil
}
