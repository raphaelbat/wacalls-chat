// Command migrate copies data from the legacy SQLite database into a
// MariaDB/MySQL instance so that an existing single-node WaCalls install can
// be promoted to the scalable MariaDB+Redis backend without losing tenant
// data.
//
// Workflow:
//
//  1. Start the wacalls server at least once against the target MariaDB so
//     that every store auto-creates its schema there (CREATE TABLE IF NOT
//     EXISTS runs at boot). Then stop the server.
//  2. Run this command pointing at the source SQLite file and the target
//     MariaDB DSN:
//
//         go run ./cmd/migrate \
//           --src wacalls.db \
//           --dst "user:pass@tcp(127.0.0.1:3306)/wacalls?parseTime=true" \
//           [--tenant <tenant_id>] [--truncate] [--batch 500]
//
//  3. Restart the server with DB_DRIVER=mariadb / DB_DSN=... .
//
// When --tenant is supplied, rows are copied only when one of the well-known
// tenant columns (tenant_id, owner_id, company_id, parent_id, user_id) is
// present and matches the supplied id. Tables without any of those columns
// are still copied in full (they are considered global metadata, e.g.
// schema-version rows or shared lookup tables).
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// tenantColumns lists the columns we treat as "this row belongs to tenant X"
// when --tenant is supplied. The first match wins.
var tenantColumns = []string{"tenant_id", "owner_id", "company_id", "parent_id", "user_id"}

// skipTables are sqlite internals or whatsmeow-managed tables that must not
// be cross-engine copied. whatsmeow recreates its own schema per-driver and
// the session blobs are not portable between sqlite and mysql.
var skipTables = map[string]bool{
	"sqlite_sequence":   true,
	"sqlite_stat1":      true,
	"sqlite_stat4":      true,
	"whatsmeow_version": true,
}

func main() {
	src := flag.String("src", "wacalls.db", "source SQLite database path")
	dst := flag.String("dst", "", "destination MariaDB DSN (go-sql-driver/mysql format)")
	tenant := flag.String("tenant", "", "if set, only rows matching this tenant id are copied")
	truncate := flag.Bool("truncate", false, "TRUNCATE each destination table before inserting")
	batchSize := flag.Int("batch", 500, "rows per multi-value INSERT")
	dryRun := flag.Bool("dry-run", false, "report what would be migrated without writing")
	flag.Parse()

	if *dst == "" {
		log.Fatal("--dst MariaDB DSN is required (see --help)")
	}
	if _, err := os.Stat(*src); err != nil {
		log.Fatalf("source sqlite file not readable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	srcDB, err := sql.Open("sqlite", "file:"+*src+"?_pragma=busy_timeout(10000)&mode=ro")
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)

	dstDB, err := sql.Open("mysql", *dst)
	if err != nil {
		log.Fatalf("open mariadb: %v", err)
	}
	defer dstDB.Close()
	if err := dstDB.PingContext(ctx); err != nil {
		log.Fatalf("ping mariadb: %v", err)
	}

	tables, err := listTables(ctx, srcDB)
	if err != nil {
		log.Fatalf("list tables: %v", err)
	}
	sort.Strings(tables)

	dstTables, err := listMariaTables(ctx, dstDB)
	if err != nil {
		log.Fatalf("list dst tables: %v", err)
	}

	fmt.Printf("migrate: %d source tables, %d destination tables\n", len(tables), len(dstTables))
	if *tenant != "" {
		fmt.Printf("migrate: tenant filter = %q\n", *tenant)
	}
	if *dryRun {
		fmt.Println("migrate: DRY RUN — no writes will be performed")
	}

	var totalRows, totalTables int
	for _, t := range tables {
		if skipTables[t] || strings.HasPrefix(t, "sqlite_") {
			continue
		}
		if strings.HasPrefix(t, "whatsmeow_") {
			fmt.Printf("  skip %-40s (whatsmeow-managed schema)\n", t)
			continue
		}
		if !dstTables[strings.ToLower(t)] {
			fmt.Printf("  skip %-40s (missing on destination — start the server once against MariaDB first)\n", t)
			continue
		}
		n, err := copyTable(ctx, srcDB, dstDB, t, *tenant, *batchSize, *truncate, *dryRun)
		if err != nil {
			log.Fatalf("copy %s: %v", t, err)
		}
		totalRows += n
		totalTables++
		fmt.Printf("  ok   %-40s %d rows\n", t, n)
	}
	fmt.Printf("migrate: done — %d tables, %d rows copied\n", totalTables, totalRows)
}

func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func listMariaTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SHOW TABLES`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[strings.ToLower(n)] = true
	}
	return out, rows.Err()
}

func columnNames(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("`+table+`")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		// cid, name, type, notnull, dflt_value, pk
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func copyTable(ctx context.Context, src, dst *sql.DB, table, tenant string, batch int, truncate, dryRun bool) (int, error) {
	cols, err := columnNames(ctx, src, table)
	if err != nil {
		return 0, fmt.Errorf("columns: %w", err)
	}
	if len(cols) == 0 {
		return 0, nil
	}

	where := ""
	args := []any{}
	if tenant != "" {
		for _, tc := range tenantColumns {
			if hasColumn(cols, tc) {
				where = " WHERE " + quoteIdent(tc) + " = ?"
				args = append(args, tenant)
				break
			}
		}
	}

	q := `SELECT ` + joinIdents(cols) + ` FROM "` + table + `"` + where
	rows, err := src.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	if !dryRun && truncate && tenant == "" {
		if _, err := dst.ExecContext(ctx, "TRUNCATE TABLE "+quoteIdent(table)); err != nil {
			// Foreign-key constraints may prevent TRUNCATE; fall back to DELETE.
			if _, err2 := dst.ExecContext(ctx, "DELETE FROM "+quoteIdent(table)); err2 != nil {
				return 0, fmt.Errorf("truncate: %w", err)
			}
		}
	}

	insertHead := "INSERT INTO " + quoteIdent(table) + " (" + joinIdents(cols) + ") VALUES "
	placeholder := "(" + strings.Repeat("?,", len(cols)-1) + "?)"

	buffer := make([][]any, 0, batch)
	total := 0
	scanDest := make([]any, len(cols))
	scanVals := make([]sql.RawBytes, len(cols))
	for i := range scanVals {
		scanDest[i] = &scanVals[i]
	}
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			return total, fmt.Errorf("scan: %w", err)
		}
		row := make([]any, len(cols))
		for i, v := range scanVals {
			if v == nil {
				row[i] = nil
			} else {
				b := make([]byte, len(v))
				copy(b, v)
				row[i] = b
			}
		}
		buffer = append(buffer, row)
		if len(buffer) >= batch {
			if err := flush(ctx, dst, insertHead, placeholder, cols, buffer, dryRun); err != nil {
				return total, err
			}
			total += len(buffer)
			buffer = buffer[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if len(buffer) > 0 {
		if err := flush(ctx, dst, insertHead, placeholder, cols, buffer, dryRun); err != nil {
			return total, err
		}
		total += len(buffer)
	}
	return total, nil
}

func flush(ctx context.Context, db *sql.DB, head, ph string, cols []string, rows [][]any, dryRun bool) error {
	if len(rows) == 0 || dryRun {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(head)
	args := make([]any, 0, len(rows)*len(cols))
	for i, r := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(ph)
		args = append(args, r...)
	}
	// Upsert semantics: re-running the migration is idempotent.
	sb.WriteString(" ON DUPLICATE KEY UPDATE ")
	for i, c := range cols {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(quoteIdent(c))
		sb.WriteString("=VALUES(")
		sb.WriteString(quoteIdent(c))
		sb.WriteString(")")
	}
	if _, err := db.ExecContext(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	return nil
}

func hasColumn(cols []string, target string) bool {
	for _, c := range cols {
		if strings.EqualFold(c, target) {
			return true
		}
	}
	return false
}

func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func joinIdents(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = quoteIdent(c)
	}
	return strings.Join(out, ",")
}