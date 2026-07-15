package main

import (
	"context"
	"database/sql"
)

// CallRow is one historical call persisted in SQLite. It mirrors the
// in-memory CallRecord used by the broker, but adds duration and a video
// flag so the BI reports can aggregate without re-deriving from peer.
type CallRow struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	OwnerUser  string `json:"ownerUserId,omitempty"`
	Direction  string `json:"direction"`
	Peer       string `json:"peer"`
	StartedAt  int64  `json:"startedAt"`
	EndedAt    int64  `json:"endedAt"`
	DurationMs int64  `json:"durationMs"`
	EndReason  string `json:"endReason,omitempty"`
	Video      bool   `json:"video"`
	Answered   bool   `json:"answered"`
}

// RecordingInfo describes a media recording persisted for a call.
type RecordingInfo struct {
	CallID   string `json:"callId"`
	Path     string `json:"-"`
	Mime     string `json:"mime"`
	Size     int64  `json:"size"`
	Token    string `json:"token"`
	Uploaded int64  `json:"uploadedAt"`
}

type callStore struct{ db *sql.DB }

func newCallStore(ctx context.Context, db *sql.DB) (*callStore, error) {
	q := `CREATE TABLE IF NOT EXISTS call_records (
		id           TEXT PRIMARY KEY,
		session_id   TEXT NOT NULL,
		owner_user   TEXT NOT NULL DEFAULT '',
		direction    TEXT NOT NULL,
		peer         TEXT NOT NULL,
		started_at   INTEGER NOT NULL,
		ended_at     INTEGER NOT NULL,
		duration_ms  INTEGER NOT NULL DEFAULT 0,
		end_reason   TEXT NOT NULL DEFAULT '',
		video        INTEGER NOT NULL DEFAULT 0,
		answered     INTEGER NOT NULL DEFAULT 0
	)`
	if _, err := db.ExecContext(ctx, q); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_call_records_session_ts
		ON call_records (session_id, started_at DESC)`); err != nil {
		return nil, err
	}
	// Idempotent migrations for recording columns; SQLite has no
	// ADD COLUMN IF NOT EXISTS, so swallow duplicate-column errors.
	for _, alter := range []string{
		`ALTER TABLE call_records ADD COLUMN recording_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE call_records ADD COLUMN recording_mime TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE call_records ADD COLUMN recording_size INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE call_records ADD COLUMN recording_token TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE call_records ADD COLUMN recording_uploaded_at INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = db.ExecContext(ctx, alter)
	}
	_, _ = db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_call_records_rec_token
		ON call_records (recording_token) WHERE recording_token <> ''`)
	return &callStore{db: db}, nil
}

// Insert is idempotent on id (INSERT OR REPLACE).
func (s *callStore) Insert(ctx context.Context, r CallRow) error {
	v := 0
	if r.Video {
		v = 1
	}
	a := 0
	if r.Answered {
		a = 1
	}
	// Use the ANSI-style UPSERT supported by both SQLite (since 3.24) and
	// MariaDB (since 10.5). Avoids the SQLite-only `INSERT OR REPLACE` form.
	_, err := s.db.ExecContext(ctx, `INSERT INTO call_records
		(id, session_id, owner_user, direction, peer, started_at, ended_at, duration_ms, end_reason, video, answered)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id  = excluded.session_id,
			owner_user  = excluded.owner_user,
			direction   = excluded.direction,
			peer        = excluded.peer,
			started_at  = excluded.started_at,
			ended_at    = excluded.ended_at,
			duration_ms = excluded.duration_ms,
			end_reason  = excluded.end_reason,
			video       = excluded.video,
			answered    = excluded.answered`,
		r.ID, r.SessionID, r.OwnerUser, r.Direction, r.Peer, r.StartedAt, r.EndedAt, r.DurationMs, r.EndReason, v, a)
	return err
}

// ListBetween returns persisted calls between [from,to] (unix ms) optionally
// filtered by session id. Newest first, capped at 5000 rows for safety.
func (s *callStore) ListBetween(ctx context.Context, sessionID string, from, to int64) ([]CallRow, error) {
	args := []any{from, to}
	q := `SELECT id, session_id, owner_user, direction, peer, started_at, ended_at, duration_ms, end_reason, video, answered
		FROM call_records WHERE started_at >= ? AND started_at <= ?`
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY started_at DESC LIMIT 5000`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CallRow{}
	for rows.Next() {
		var r CallRow
		var v, a int
		if err := rows.Scan(&r.ID, &r.SessionID, &r.OwnerUser, &r.Direction, &r.Peer, &r.StartedAt, &r.EndedAt, &r.DurationMs, &r.EndReason, &v, &a); err != nil {
			return nil, err
		}
		r.Video = v == 1
		r.Answered = a == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteForSession is used when a session is deleted.
func (s *callStore) DeleteForSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM call_records WHERE session_id = ?`, sessionID)
	return err
}

// SetRecording attaches a recording file to a previously persisted call.
// The call row must already exist (broker persists it on end).
func (s *callStore) SetRecording(ctx context.Context, callID string, info RecordingInfo) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_records
		SET recording_path = ?, recording_mime = ?, recording_size = ?, recording_token = ?, recording_uploaded_at = ?
		WHERE id = ?`,
		info.Path, info.Mime, info.Size, info.Token, info.Uploaded, callID)
	return err
}

// RecordingByCall returns the recording attached to a call, if any.
func (s *callStore) RecordingByCall(ctx context.Context, callID string) (RecordingInfo, bool, error) {
	var info RecordingInfo
	err := s.db.QueryRowContext(ctx, `SELECT id, recording_path, recording_mime, recording_size, recording_token, recording_uploaded_at
		FROM call_records WHERE id = ?`, callID).
		Scan(&info.CallID, &info.Path, &info.Mime, &info.Size, &info.Token, &info.Uploaded)
	if err == sql.ErrNoRows || info.Path == "" {
		return RecordingInfo{}, false, nil
	}
	if err != nil {
		return RecordingInfo{}, false, err
	}
	return info, true, nil
}

// RecordingByToken resolves a public share token to its recording.
func (s *callStore) RecordingByToken(ctx context.Context, token string) (RecordingInfo, bool, error) {
	var info RecordingInfo
	err := s.db.QueryRowContext(ctx, `SELECT id, recording_path, recording_mime, recording_size, recording_token, recording_uploaded_at
		FROM call_records WHERE recording_token = ?`, token).
		Scan(&info.CallID, &info.Path, &info.Mime, &info.Size, &info.Token, &info.Uploaded)
	if err == sql.ErrNoRows {
		return RecordingInfo{}, false, nil
	}
	if err != nil {
		return RecordingInfo{}, false, err
	}
	return info, true, nil
}

// CallMeta returns the session id and owner user of a call, used to
// authorize signed-URL generation and authenticated direct downloads.
func (s *callStore) CallMeta(ctx context.Context, callID string) (sessionID, ownerUser string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT session_id, owner_user FROM call_records WHERE id = ?`, callID).
		Scan(&sessionID, &ownerUser)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return sessionID, ownerUser, true, nil
}
