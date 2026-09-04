package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // embedded zone database (containers often lack /usr/share/zoneinfo)
)

// businessHoursDay is one weekday row of the weekly grid. Weekday follows
// time.Weekday (0 = Sunday).
type businessHoursDay struct {
	Weekday int    `json:"weekday"`
	Enabled bool   `json:"enabled"`
	Open    string `json:"open"`  // "08:00"
	Close   string `json:"close"` // "18:00"
}

// businessHoursConfig is stored per scope: an instance (session) or a queue.
// A queue with Enabled=false inherits the instance grid.
type businessHoursConfig struct {
	Scope     string             `json:"scope"` // session|queue
	ScopeID   string             `json:"scopeId"`
	Enabled   bool               `json:"enabled"`
	Timezone  string             `json:"timezone"`
	Message   string             `json:"message"`
	Days      []businessHoursDay `json:"days"`
	Holidays  []string           `json:"holidays"` // ["2026-12-25"]
	UpdatedAt int64              `json:"updatedAt"`
}

type businessHoursStore struct{ db *sql.DB }

func defaultBusinessDays() []businessHoursDay {
	days := make([]businessHoursDay, 0, 7)
	for i := 0; i < 7; i++ {
		days = append(days, businessHoursDay{
			Weekday: i,
			Enabled: i >= 1 && i <= 5,
			Open:    "08:00",
			Close:   "18:00",
		})
	}
	return days
}

func newBusinessHoursStore(ctx context.Context, db *sql.DB) (*businessHoursStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS business_hours (
		scope      TEXT NOT NULL,
		scope_id   TEXT NOT NULL,
		enabled    INTEGER NOT NULL DEFAULT 0,
		timezone   TEXT NOT NULL DEFAULT 'America/Sao_Paulo',
		message    TEXT NOT NULL DEFAULT '',
		days       TEXT NOT NULL DEFAULT '',
		holidays   TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (scope, scope_id)
	)`); err != nil {
		return nil, err
	}
	// Tracks the out-of-hours auto reply so it is sent once per closed window
	// instead of on every inbound message.
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS business_hours_notices (
		session_id TEXT NOT NULL,
		chat_jid   TEXT NOT NULL,
		window_key TEXT NOT NULL,
		sent_at    INTEGER NOT NULL,
		PRIMARY KEY (session_id, chat_jid)
	)`); err != nil {
		return nil, err
	}
	return &businessHoursStore{db: db}, nil
}

func normalizeScope(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), "queue") {
		return "queue"
	}
	return "session"
}

// Get returns the stored config or a disabled default when none exists.
func (s *businessHoursStore) Get(ctx context.Context, scope, scopeID string) (businessHoursConfig, error) {
	scope = normalizeScope(scope)
	cfg := businessHoursConfig{
		Scope: scope, ScopeID: scopeID, Enabled: false,
		Timezone: "America/Sao_Paulo", Days: defaultBusinessDays(), Holidays: []string{},
	}
	var (
		enabled            int
		daysRaw, holidays  string
	)
	row := s.db.QueryRowContext(ctx, `SELECT enabled, timezone, message, days, holidays, updated_at
		FROM business_hours WHERE scope=? AND scope_id=?`, scope, scopeID)
	if err := row.Scan(&enabled, &cfg.Timezone, &cfg.Message, &daysRaw, &holidays, &cfg.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return cfg, nil
		}
		return cfg, err
	}
	cfg.Enabled = enabled == 1
	if daysRaw != "" {
		var parsed []businessHoursDay
		if json.Unmarshal([]byte(daysRaw), &parsed) == nil && len(parsed) > 0 {
			cfg.Days = parsed
		}
	}
	if holidays != "" {
		var parsed []string
		if json.Unmarshal([]byte(holidays), &parsed) == nil {
			cfg.Holidays = parsed
		}
	}
	if cfg.Holidays == nil {
		cfg.Holidays = []string{}
	}
	return cfg, nil
}

func (s *businessHoursStore) Save(ctx context.Context, cfg businessHoursConfig) (businessHoursConfig, error) {
	cfg.Scope = normalizeScope(cfg.Scope)
	if len(cfg.Days) == 0 {
		cfg.Days = defaultBusinessDays()
	}
	if strings.TrimSpace(cfg.Timezone) == "" {
		cfg.Timezone = "America/Sao_Paulo"
	}
	if cfg.Holidays == nil {
		cfg.Holidays = []string{}
	}
	cfg.UpdatedAt = time.Now().UnixMilli()
	days, _ := json.Marshal(cfg.Days)
	holidays, _ := json.Marshal(cfg.Holidays)
	_, err := s.db.ExecContext(ctx, `INSERT INTO business_hours (scope, scope_id, enabled, timezone, message, days, holidays, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(scope, scope_id) DO UPDATE SET
			enabled=excluded.enabled, timezone=excluded.timezone, message=excluded.message,
			days=excluded.days, holidays=excluded.holidays, updated_at=excluded.updated_at`,
		cfg.Scope, cfg.ScopeID, qBool(cfg.Enabled), cfg.Timezone, cfg.Message, string(days), string(holidays), cfg.UpdatedAt)
	return cfg, err
}

// ShouldNotify records the out-of-hours reply for a chat and reports whether
// it still needs to be sent for the given closed window.
func (s *businessHoursStore) ShouldNotify(ctx context.Context, sessionID, chatJID, windowKey string) (bool, error) {
	var stored string
	row := s.db.QueryRowContext(ctx, `SELECT window_key FROM business_hours_notices WHERE session_id=? AND chat_jid=?`, sessionID, chatJID)
	err := row.Scan(&stored)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil && stored == windowKey {
		return false, nil
	}
	_, werr := s.db.ExecContext(ctx, `INSERT INTO business_hours_notices (session_id, chat_jid, window_key, sent_at)
		VALUES (?,?,?,?)
		ON CONFLICT(session_id, chat_jid) DO UPDATE SET window_key=excluded.window_key, sent_at=excluded.sent_at`,
		sessionID, chatJID, windowKey, time.Now().UnixMilli())
	if werr != nil {
		return false, werr
	}
	return true, nil
}

func parseClock(v string) (int, bool) {
	parts := strings.SplitN(strings.TrimSpace(v), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, false
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// isOpenAt reports whether the config considers `now` inside business hours,
// plus a key identifying the current closed window (used for once-per-window
// notifications).
func (cfg businessHoursConfig) isOpenAt(now time.Time) (bool, string) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.Local
	}
	local := now.In(loc)
	dateKey := local.Format("2006-01-02")
	for _, h := range cfg.Holidays {
		if strings.TrimSpace(h) == dateKey {
			return false, cfg.Scope + ":" + cfg.ScopeID + ":holiday:" + dateKey
		}
	}
	minutes := local.Hour()*60 + local.Minute()
	for _, d := range cfg.Days {
		if d.Weekday != int(local.Weekday()) {
			continue
		}
		open, okOpen := parseClock(d.Open)
		closeAt, okClose := parseClock(d.Close)
		if !d.Enabled || !okOpen || !okClose {
			break
		}
		if minutes >= open && minutes < closeAt {
			return true, ""
		}
		if minutes < open {
			return false, cfg.Scope + ":" + cfg.ScopeID + ":" + dateKey + ":before"
		}
		return false, cfg.Scope + ":" + cfg.ScopeID + ":" + dateKey + ":after"
	}
	return false, cfg.Scope + ":" + cfg.ScopeID + ":" + dateKey + ":closed"
}
