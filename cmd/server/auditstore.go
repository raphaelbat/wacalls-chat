package main

import (
	"context"
	"database/sql"
	"time"
)

type roleAuditRow struct {
	ID          string `json:"id"`
	TargetID    string `json:"targetId"`
	TargetEmail string `json:"targetEmail"`
	ActorID     string `json:"actorId"`
	ActorEmail  string `json:"actorEmail"`
	Role        string `json:"role"`
	Granted     bool   `json:"granted"`
	PrevGranted bool   `json:"prevGranted"`
	CreatedAt   int64  `json:"createdAt"`
}

func initAuditSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS role_audit_log (
		id            TEXT PRIMARY KEY,
		target_id     TEXT NOT NULL,
		target_email  TEXT NOT NULL DEFAULT '',
		actor_id      TEXT NOT NULL,
		actor_email   TEXT NOT NULL DEFAULT '',
		role          TEXT NOT NULL,
		granted       INTEGER NOT NULL,
		prev_granted  INTEGER NOT NULL,
		created_at    INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_role_audit_created ON role_audit_log(created_at DESC)`)
	return nil
}

func logRoleChange(ctx context.Context, db *sql.DB, e roleAuditRow) error {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().Unix()
	}
	g, pg := 0, 0
	if e.Granted {
		g = 1
	}
	if e.PrevGranted {
		pg = 1
	}
	_, err := db.ExecContext(ctx, `INSERT INTO role_audit_log
		(id, target_id, target_email, actor_id, actor_email, role, granted, prev_granted, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		e.ID, e.TargetID, e.TargetEmail, e.ActorID, e.ActorEmail, e.Role, g, pg, e.CreatedAt)
	return err
}

func listRoleAudit(ctx context.Context, db *sql.DB, limit int) ([]roleAuditRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `SELECT id, target_id, target_email, actor_id, actor_email, role, granted, prev_granted, created_at
		FROM role_audit_log ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []roleAuditRow{}
	for rows.Next() {
		var r roleAuditRow
		var g, pg int
		if err := rows.Scan(&r.ID, &r.TargetID, &r.TargetEmail, &r.ActorID, &r.ActorEmail, &r.Role, &g, &pg, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Granted = g == 1
		r.PrevGranted = pg == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// lookupUserEmail returns the email for an id, or empty if missing.
func lookupUserEmail(ctx context.Context, db *sql.DB, id string) string {
	var email string
	_ = db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ?`, id).Scan(&email)
	return email
}
