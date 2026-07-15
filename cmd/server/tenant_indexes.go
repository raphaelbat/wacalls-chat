package main

import (
	"context"
	"database/sql"
	"log/slog"
)

// ensureTenantIndexes adds composite indexes used by the multi-tenant SaaS
// query paths. Both SQLite (>=3.8) and MariaDB (>=10.0.2) accept
// `CREATE INDEX IF NOT EXISTS`, so the same DDL works on either backend.
//
// Errors are logged but never abort boot — if a table does not exist yet
// (e.g. the corresponding store has not been wired) we just skip it.
//
// Index targets reflect the hot read paths observed in the SaaS:
//
//   - messages: tenant timelines and per-chat history are always filtered by
//     session_id + chat_jid; (sender_jid, ts) supports per-contact lookups.
//   - chats:    per-tenant inbox listing sorted by last_ts.
//   - call_records: dashboards/reports filtered by session + answered window.
//   - sessions: super-admin views filter by owner_id (the tenant root).
func ensureTenantIndexes(ctx context.Context, db *sql.DB, log *slog.Logger) {
	stmts := []string{
		// messages
		`CREATE INDEX IF NOT EXISTS idx_messages_session_ts ON messages (session_id, ts DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_sender_ts  ON messages (session_id, sender_jid, ts DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_unread ON messages (session_id, chat_jid, from_me, ts)`,
		// chats (inbox)
		`CREATE INDEX IF NOT EXISTS idx_chats_session_last  ON chats (session_id, last_ts DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_chats_assigned      ON chats (session_id, assigned_user_id, status)`,
		// call records
		`CREATE INDEX IF NOT EXISTS idx_calls_session_answered ON call_records (session_id, answered_at DESC)`,
		// sessions per tenant
		`CREATE INDEX IF NOT EXISTS idx_sessions_owner      ON sessions (owner_id)`,
		// chat closures / events
		`CREATE INDEX IF NOT EXISTS idx_chat_closures_session ON chat_closures (session_id, closed_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_events_session   ON chat_events (session_id, ts DESC)`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			if log != nil {
				log.Debug("tenant index skipped", "stmt", q, "err", err)
			}
		}
	}
}
