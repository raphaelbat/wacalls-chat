package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// Flow row stored in SQLite. Graph is a JSON string produced/consumed by the
// React Flow editor on the client; the executor parses it lazily.
type FlowRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Trigger   string `json:"trigger"` // inbound | outbound | manual
	Graph     string `json:"graph"`   // JSON {nodes,edges,startNodeId}
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	OwnerID   string `json:"-"`
	// Keywords (comma-separated, lowercased on write) that, when found in an
	// inbound text message, can trigger the flow even when no flow is bound
	// to the connection. Empty disables keyword triggering for this flow.
	Keywords     string `json:"keywords"`
	KeywordMatch string `json:"keywordMatch"` // any | exact | contains | starts_with
}

type FlowRunRow struct {
	ID          string `json:"id"`
	FlowID      string `json:"flowId"`
	CallID      string `json:"callId"`
	SessionID   string `json:"sessionId"`
	Status      string `json:"status"`
	CurrentNode string `json:"currentNode"`
	Variables   string `json:"variables"`
	StartedAt   int64  `json:"startedAt"`
	EndedAt     *int64 `json:"endedAt"`
}

type FlowEventRow struct {
	ID        int64  `json:"id"`
	RunID     string `json:"runId"`
	Ts        int64  `json:"ts"`
	NodeID    string `json:"nodeId"`
	EventType string `json:"eventType"`
	Payload   string `json:"payload"`
}

type flowStore struct{ db *sql.DB }

func newFlowStore(ctx context.Context, db *sql.DB) (*flowStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS flows (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			trigger    TEXT NOT NULL DEFAULT 'inbound',
			graph      TEXT NOT NULL,
			enabled    INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS flow_runs (
			id           TEXT PRIMARY KEY,
			flow_id      TEXT NOT NULL,
			call_id      TEXT NOT NULL,
			session_id   TEXT NOT NULL,
			status       TEXT NOT NULL,
			current_node TEXT,
			variables    TEXT NOT NULL,
			started_at   INTEGER NOT NULL,
			ended_at     INTEGER,
			FOREIGN KEY(flow_id) REFERENCES flows(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS flow_run_events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id     TEXT NOT NULL,
			ts         INTEGER NOT NULL,
			node_id    TEXT,
			event_type TEXT NOT NULL,
			payload    TEXT,
			FOREIGN KEY(run_id) REFERENCES flow_runs(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_flow_runs_flow ON flow_runs(flow_id)`,
		`CREATE INDEX IF NOT EXISTS idx_flow_events_run ON flow_run_events(run_id)`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, err
		}
	}
	// Idempotent owner_id migration.
	_, _ = db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN owner_id TEXT NOT NULL DEFAULT ''`)
	// Idempotent keyword trigger columns.
	_, _ = db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN keywords TEXT NOT NULL DEFAULT ''`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE flows ADD COLUMN keyword_match TEXT NOT NULL DEFAULT 'contains'`)
	return &flowStore{db: db}, nil
}

func newFlowID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "flow_" + hex.EncodeToString(b)
}

func newRunID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "run_" + hex.EncodeToString(b)
}

func (s *flowStore) List(ctx context.Context, ownerID string, includeAll bool) ([]FlowRow, error) {
	var rows *sql.Rows
	var err error
	if includeAll {
		rows, err = s.db.QueryContext(ctx, `SELECT id, name, trigger, graph, enabled, created_at, updated_at, COALESCE(owner_id,''), COALESCE(keywords,''), COALESCE(keyword_match,'contains') FROM flows ORDER BY updated_at DESC`)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT id, name, trigger, graph, enabled, created_at, updated_at, COALESCE(owner_id,''), COALESCE(keywords,''), COALESCE(keyword_match,'contains') FROM flows WHERE owner_id = ? ORDER BY updated_at DESC`, ownerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowRow{}
	for rows.Next() {
		var r FlowRow
		var enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.Trigger, &r.Graph, &enabled, &r.CreatedAt, &r.UpdatedAt, &r.OwnerID, &r.Keywords, &r.KeywordMatch); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *flowStore) Get(ctx context.Context, id string) (*FlowRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, trigger, graph, enabled, created_at, updated_at, COALESCE(owner_id,''), COALESCE(keywords,''), COALESCE(keyword_match,'contains') FROM flows WHERE id = ?`, id)
	var r FlowRow
	var enabled int
	if err := row.Scan(&r.ID, &r.Name, &r.Trigger, &r.Graph, &enabled, &r.CreatedAt, &r.UpdatedAt, &r.OwnerID, &r.Keywords, &r.KeywordMatch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Enabled = enabled == 1
	return &r, nil
}

func (s *flowStore) Insert(ctx context.Context, r FlowRow) error {
	now := time.Now().Unix()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	if r.KeywordMatch == "" {
		r.KeywordMatch = "contains"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO flows (id,name,trigger,graph,enabled,created_at,updated_at,owner_id,keywords,keyword_match) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Trigger, r.Graph, enabled, r.CreatedAt, r.UpdatedAt, r.OwnerID, r.Keywords, r.KeywordMatch)
	return err
}

func (s *flowStore) Update(ctx context.Context, r FlowRow) error {
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	if r.KeywordMatch == "" {
		r.KeywordMatch = "contains"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE flows SET name=?, trigger=?, graph=?, enabled=?, keywords=?, keyword_match=?, updated_at=? WHERE id=?`,
		r.Name, r.Trigger, r.Graph, enabled, r.Keywords, r.KeywordMatch, time.Now().Unix(), r.ID)
	return err
}

func (s *flowStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM flows WHERE id = ?`, id)
	return err
}

func (s *flowStore) FindEnabledByTrigger(ctx context.Context, trigger string) (*FlowRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, trigger, graph, enabled, created_at, updated_at, COALESCE(owner_id,''), COALESCE(keywords,''), COALESCE(keyword_match,'contains') FROM flows WHERE trigger=? AND enabled=1 ORDER BY updated_at DESC LIMIT 1`, trigger)
	var r FlowRow
	var enabled int
	if err := row.Scan(&r.ID, &r.Name, &r.Trigger, &r.Graph, &enabled, &r.CreatedAt, &r.UpdatedAt, &r.OwnerID, &r.Keywords, &r.KeywordMatch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Enabled = enabled == 1
	return &r, nil
}

// FindEnabledByTriggerForOwner returns the most recent enabled flow for a given
// trigger owned by ownerID (or any owner when ownerID==""). Used so each user
// only triggers their own flows.
func (s *flowStore) FindEnabledByTriggerForOwner(ctx context.Context, trigger, ownerID string) (*FlowRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, trigger, graph, enabled, created_at, updated_at, COALESCE(owner_id,''), COALESCE(keywords,''), COALESCE(keyword_match,'contains') FROM flows WHERE trigger=? AND enabled=1 AND owner_id=? ORDER BY updated_at DESC LIMIT 1`, trigger, ownerID)
	var r FlowRow
	var enabled int
	if err := row.Scan(&r.ID, &r.Name, &r.Trigger, &r.Graph, &enabled, &r.CreatedAt, &r.UpdatedAt, &r.OwnerID, &r.Keywords, &r.KeywordMatch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Enabled = enabled == 1
	return &r, nil
}

// ListEnabledWithKeywordsForOwner returns every enabled flow owned by ownerID
// that has at least one keyword configured. Used by the inbound message
// router to pick the best match for a free-form text.
func (s *flowStore) ListEnabledWithKeywordsForOwner(ctx context.Context, ownerID string) ([]FlowRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, trigger, graph, enabled, created_at, updated_at, COALESCE(owner_id,''), COALESCE(keywords,''), COALESCE(keyword_match,'contains')
		FROM flows WHERE enabled=1 AND owner_id=? AND COALESCE(keywords,'') <> '' ORDER BY updated_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowRow{}
	for rows.Next() {
		var r FlowRow
		var enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.Trigger, &r.Graph, &enabled, &r.CreatedAt, &r.UpdatedAt, &r.OwnerID, &r.Keywords, &r.KeywordMatch); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// FlowStats aggregates counts for the per-flow report dialog.
type FlowStats struct {
	Executions    int     `json:"executions"`
	Completed     int     `json:"completed"`
	Interacted    int     `json:"interacted"`
	AvgNodes      float64 `json:"avgNodes"`
	CompletionPct int     `json:"completionPct"`
	InteractPct   int     `json:"interactPct"`
}

func (s *flowStore) Stats(ctx context.Context, flowID string) (FlowStats, error) {
	var st FlowStats
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0) FROM flow_runs WHERE flow_id=?`,
		flowID).Scan(&st.Executions, &st.Completed); err != nil {
		return st, err
	}
	// avg distinct nodes per run + how many runs actually interacted (>=1 event)
	if st.Executions > 0 {
		var totalDistinct int
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(c),0) FROM (
			SELECT COUNT(DISTINCT node_id) AS c FROM flow_run_events e
			JOIN flow_runs r ON r.id = e.run_id WHERE r.flow_id=? GROUP BY e.run_id
		)`, flowID).Scan(&totalDistinct)
		st.AvgNodes = float64(totalDistinct) / float64(st.Executions)
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT e.run_id) FROM flow_run_events e
			JOIN flow_runs r ON r.id = e.run_id WHERE r.flow_id=?`, flowID).Scan(&st.Interacted)
		st.CompletionPct = int(float64(st.Completed) * 100 / float64(st.Executions))
		st.InteractPct = int(float64(st.Interacted) * 100 / float64(st.Executions))
	}
	return st, nil
}

func (s *flowStore) InsertRun(ctx context.Context, r FlowRunRow) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO flow_runs (id,flow_id,call_id,session_id,status,current_node,variables,started_at,ended_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, r.FlowID, r.CallID, r.SessionID, r.Status, r.CurrentNode, r.Variables, r.StartedAt, r.EndedAt)
	return err
}

func (s *flowStore) UpdateRun(ctx context.Context, id, status, currentNode, variables string, endedAt *int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE flow_runs SET status=?, current_node=?, variables=?, ended_at=? WHERE id=?`,
		status, currentNode, variables, endedAt, id)
	return err
}

func (s *flowStore) ListRuns(ctx context.Context, flowID string) ([]FlowRunRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, flow_id, call_id, session_id, status, COALESCE(current_node,''), variables, started_at, ended_at FROM flow_runs WHERE flow_id=? ORDER BY started_at DESC LIMIT 100`, flowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowRunRow{}
	for rows.Next() {
		var r FlowRunRow
		var ended sql.NullInt64
		if err := rows.Scan(&r.ID, &r.FlowID, &r.CallID, &r.SessionID, &r.Status, &r.CurrentNode, &r.Variables, &r.StartedAt, &ended); err != nil {
			return nil, err
		}
		if ended.Valid {
			v := ended.Int64
			r.EndedAt = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *flowStore) InsertEvent(ctx context.Context, e FlowEventRow) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO flow_run_events (run_id, ts, node_id, event_type, payload) VALUES (?,?,?,?,?)`,
		e.RunID, e.Ts, e.NodeID, e.EventType, e.Payload)
	return err
}

func (s *flowStore) ListEvents(ctx context.Context, runID string) ([]FlowEventRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, ts, COALESCE(node_id,''), event_type, COALESCE(payload,'') FROM flow_run_events WHERE run_id=? ORDER BY id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowEventRow{}
	for rows.Next() {
		var e FlowEventRow
		if err := rows.Scan(&e.ID, &e.RunID, &e.Ts, &e.NodeID, &e.EventType, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
