// Package storage owns the versioned business database. Migrations are explicit;
// opening a service never creates tables, imports data, or scans the catalog.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

const Version = 4

var migrationFiles = []string{"001_library.sql", "002_reading.sql", "003_management.sql", "004_sources.sql"}

type Database struct {
	Read  *sql.DB
	Write *sql.DB
}

// Connect applies connection-local settings through the driver on every new
// connection, including replacement connections after cancellation or expiry.
func Connect(ctx context.Context, filename, mode string, count int) (*sql.DB, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	path := filepath.ToSlash(abs)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path}
	pragmas := []string{"foreign_keys(1)", "busy_timeout(2000)", "cache_size(-8192)", "trusted_schema(0)", "mmap_size(0)", "synchronous(FULL)", "temp_store(FILE)"}
	if mode == "ro" {
		pragmas = append(pragmas, "query_only(1)")
	}
	lock := "immediate"
	if mode == "ro" {
		lock = "deferred"
	}
	u.RawQuery = url.Values{"mode": {mode}, "_pragma": pragmas, "_txlock": {lock}}.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(count)
	db.SetMaxIdleConns(count)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migration(version int) ([]byte, string, error) {
	data, err := migrations.ReadFile("migrations/" + migrationFiles[version-1])
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), err
}

// Validate the entire contiguous history, not just the latest checksum.
func schemaVersion(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	version := 0
	for rows.Next() {
		var next int
		var got string
		if err := rows.Scan(&next, &got); err != nil {
			return 0, err
		}
		if next != version+1 || next > Version {
			return 0, fmt.Errorf("unsupported or incomplete schema history")
		}
		_, checksum, err := migration(next)
		if err != nil {
			return 0, err
		}
		if got != checksum {
			return 0, fmt.Errorf("schema migration %d checksum mismatch", next)
		}
		version = next
	}
	return version, rows.Err()
}

// Migrate refuses populated foreign databases, including the original Prisma
// database. The schema and its checksum are committed in the same transaction.
func Migrate(ctx context.Context, filename string) error {
	db, err := Connect(ctx, filename, "rwc", 1)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var total, own int
	if err := tx.QueryRowContext(ctx, `SELECT count(*), COALESCE(sum(name='schema_migrations'),0) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'`).Scan(&total, &own); err != nil {
		return err
	}
	if total > 0 && own != 1 {
		return fmt.Errorf("refusing to migrate a non-library database")
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		return err
	}
	version, err := schemaVersion(ctx, tx)
	if err != nil {
		return err
	}
	for next := version + 1; next <= Version; next++ {
		data, checksum, err := migration(next)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES(?,?,?)`, next, checksum, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	var journal string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode=WAL`).Scan(&journal); err != nil {
		return err
	}
	if journal != "wal" {
		return fmt.Errorf("WAL mode unavailable")
	}
	return nil
}

func Open(ctx context.Context, filename string) (*Database, error) {
	read, err := Connect(ctx, filename, "ro", 2)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Database, error) { read.Close(); return nil, err }
	version, err := schemaVersion(ctx, read)
	if err != nil {
		return fail(fmt.Errorf("run library migrate first: %w", err))
	}
	if version != Version {
		return fail(fmt.Errorf("unsupported or modified schema; run the matching release's migrate command"))
	}
	var journal string
	if err := read.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
		return fail(err)
	}
	if journal != "wal" {
		return fail(fmt.Errorf("database must use WAL; run library migrate"))
	}
	write, err := Connect(ctx, filename, "rw", 1)
	if err != nil {
		return fail(err)
	}
	return &Database{Read: read, Write: write}, nil
}

func (d *Database) Close() error {
	first := d.Read.Close()
	if err := d.Write.Close(); first == nil {
		first = err
	}
	return first
}

func (d *Database) Ready(ctx context.Context, portalUser string) error {
	var status string
	if err := d.Read.QueryRowContext(ctx, `SELECT status FROM import_runs WHERE id=1`).Scan(&status); err != nil {
		return fmt.Errorf("database has no completed import: %w", err)
	}
	if status != "complete" {
		return fmt.Errorf("legacy import has not passed verification")
	}
	var id string
	if err := d.Read.QueryRowContext(ctx, `SELECT id FROM readers WHERE portal_user=?`, portalUser).Scan(&id); err != nil {
		return fmt.Errorf("portal reader is not mapped: %w", err)
	}
	if id != "default-reader" {
		return fmt.Errorf("unexpected portal reader mapping")
	}
	return nil
}
