package main

import (
	"context"
	"database/sql"
	"log/slog"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"

	"wacalls/internal/cache"
	"wacalls/internal/storage"
)

type server struct {
	broker     *Broker
	sessions   *SessionManager
	log        *slog.Logger
	staticDir  string
	flows      *flowStore
	flowExec   *FlowExecutor
	flowTracer *flowTracer
	messages   *messageStore
	auth       *authStore
	loginLimit *loginLimiter
	queues     *queueStore
	tags       *tagStore
	kanban     *kanbanStore
	sessStore  *sessionStore
	chatMeta   *chatMetaStore
	calls      *callStore
	recSigner  *recordingSigner
	settings   *settingsStore
	db         *sql.DB
	authStream *authStreamHub
	cache      cache.Cache
}

func openDB(dbPath string) (*sql.DB, error) {
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func newServer(ctx context.Context, dbPath, staticDir string, maxCalls int, log *slog.Logger) (*server, error) {
	cfg := storage.FromEnv(dbPath)
	db, waDriver, err := storage.Open(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Driver != "" && cfg.Driver != "sqlite" && cfg.Driver != "sqlite3" {
		log.Info("storage backend", "driver", cfg.Driver)
	}
	cch, cerr := cache.FromEnv()
	if cerr != nil {
		log.Warn("cache: falling back to in-memory", "err", cerr)
	}
	container := sqlstore.NewWithDB(db, waDriver, waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		return nil, err
	}
	store, err := newSessionStore(ctx, db)
	if err != nil {
		return nil, err
	}

	waLogger := waLog.Noop
	if log.Enabled(ctx, slog.LevelDebug) {
		waLogger = waLog.Stdout("WA", "INFO", true)
	}

	broker := NewBroker()
	mgr := newSessionManager(ctx, container, broker, store, waLogger, log, maxCalls)
	broker.SnapshotFn = mgr.snapshotEvents
	broker.SessionOwner = mgr.ownerOf
	broker.SessionTenant = mgr.tenantOf
	broker.SessionsForFn = mgr.infosFor

	flows, err := newFlowStore(ctx, db)
	if err != nil {
		return nil, err
	}
	messages, err := newMessageStore(ctx, db)
	if err != nil {
		return nil, err
	}
	chatMeta, err := newChatMetaStore(ctx, db)
	if err != nil {
		return nil, err
	}
	callStore, err := newCallStore(ctx, db)
	if err != nil {
		return nil, err
	}
	auth, err := newAuthStore(ctx, db)
	if err != nil {
		return nil, err
	}
	mgr.UserTenantFn = func(userID string) string {
		if userID == "" {
			return ""
		}
		pid, err := auth.ParentOf(ctx, userID)
		if err == nil && pid != "" {
			return pid
		}
		if err == nil {
			return userID
		}
		return ""
	}
	mgr.IsAdminRoleFn = func(userID string) bool {
		if userID == "" {
			return false
		}
		ok, err := auth.HasRole(ctx, userID, RoleAdmin)
		if err != nil {
			return false
		}
		return ok
	}
	mgr.UserSessionsFn = func(userID string) []string {
		if userID == "" {
			return nil
		}
		ids, err := auth.SessionsFor(ctx, userID)
		if err != nil {
			return nil
		}
		return ids
	}
	queues, err := newQueueStore(ctx, db)
	if err != nil {
		return nil, err
	}
	tags, err := newTagStore(ctx, db)
	if err != nil {
		return nil, err
	}
	kanban, err := newKanbanStore(ctx, db)
	if err != nil {
		return nil, err
	}
	settings, err := newSettingsStore(ctx, db)
	if err != nil {
		return nil, err
	}
	signer, err := newRecordingSigner()
	if err != nil {
		return nil, err
	}
	if err := initAuditSchema(ctx, db); err != nil {
		return nil, err
	}
	// Composite indexes per-tenant — safe to (re)run on every boot.
	ensureTenantIndexes(ctx, db, log)
	mgr.messages = messages
	mgr.chatMeta = chatMeta
	mgr.kanban = kanban
	mgr.calls = callStore
	exec := newFlowExecutor(flows, log)
	mgr.flowExec = exec
	tracer := newFlowTracer()
	exec.AttachTracer(tracer)
	exec.AttachBroker(broker)
	bridge := newFlowBridge(mgr, log)
	bridge.AttachBroker(broker)
	bridge.AttachTracer(tracer)
	exec.AttachBridge(bridge)

	broker.PersistCall = func(rec CallRecord) {
		owner := ""
		if rec.Owner != nil {
			owner = *rec.Owner
		}
		started := rec.StartedAt
		ended := int64(0)
		if rec.EndedAt != nil {
			ended = *rec.EndedAt
		}
		dur := int64(0)
		if ended > started {
			dur = ended - started
		}
		_ = callStore.Insert(ctx, CallRow{
			ID: rec.CallID, SessionID: rec.SessionID, OwnerUser: owner,
			Direction: rec.Direction, Peer: rec.Peer, StartedAt: started, EndedAt: ended,
			DurationMs: dur, EndReason: rec.EndReason,
			Answered: rec.Answered,
		})
	}

	hub := newAuthStreamHub()
	auth.OnTokensRevoked = func(tokens []string) {
		for _, t := range tokens {
			hub.Revoke(t)
		}
	}
	srv := &server{broker: broker, sessions: mgr, log: log, staticDir: staticDir, flows: flows, flowExec: exec, flowTracer: tracer, messages: messages, auth: auth, loginLimit: newLoginLimiter(), queues: queues, tags: tags, kanban: kanban, sessStore: store, chatMeta: chatMeta, calls: callStore, recSigner: signer, settings: settings, db: db, authStream: hub, cache: cch}
	return srv, nil
}
