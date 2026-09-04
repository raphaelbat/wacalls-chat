// Package storage abstracts the SQL backend used by the WaCalls server.
//
// Two drivers are supported:
//
//   - "sqlite"  (default) — file-based, zero setup, fine up to ~30-80 active
//     tenants. Uses modernc.org/sqlite (pure Go, no cgo).
//   - "mariadb" (a.k.a. "mysql") — experimental app-store driver only.
//     It is intentionally not enabled by the installer because the current
//     whatsmeow sqlstore dependency supports SQLite/Postgres dialects, not
//     MySQL/MariaDB. Use SQLite for the main database and Redis for cache/fanout.
//
// The same *sql.DB is fed to whatsmeow's sqlstore via the returned
// WhatsmeowDriver name. For production installs this must remain "sqlite3".
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite" // pure-Go sqlite driver
)

// Config selects the backend. Zero value falls back to a file-based SQLite
// instance at the provided SQLitePath.
type Config struct {
	Driver     string // "sqlite" (default) or "mariadb"/"mysql"
	DSN        string // backend-specific DSN; for sqlite, leave empty and use SQLitePath
	SQLitePath string // path to the sqlite database file (sqlite driver only)
}

// FromEnv reads DB_DRIVER / DB_DSN and merges them with the fallback path.
func FromEnv(sqlitePath string) Config {
	return Config{
		Driver:     strings.ToLower(strings.TrimSpace(os.Getenv("DB_DRIVER"))),
		DSN:        strings.TrimSpace(os.Getenv("DB_DSN")),
		SQLitePath: sqlitePath,
	}
}

// Open returns the configured *sql.DB plus the driver name expected by
// whatsmeow's sqlstore. Callers MUST Close the DB.
func Open(cfg Config) (*sql.DB, string, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = "sqlite"
	}
	switch driver {
	case "sqlite", "sqlite3":
		path := cfg.SQLitePath
		if path == "" {
			path = "wacalls.db"
		}
		dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, "", err
		}
		// SQLite is a single-writer engine in this app; keep the pool at 1
		// to serialise writes and avoid SQLITE_BUSY under load.
		db.SetMaxOpenConns(1)
		return db, "sqlite3", nil
	case "mariadb", "mysql":
		return nil, "", fmt.Errorf("storage: DB_DRIVER=%s is not supported by this WaCalls build because whatsmeow does not accept the mysql dialect; remove DB_DRIVER/DB_DSN and use SQLite + Redis", driver)
	default:
		return nil, "", fmt.Errorf("storage: unknown DB_DRIVER %q (use sqlite)", driver)
	}
}