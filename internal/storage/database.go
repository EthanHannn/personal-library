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

const Version = 1

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

func migration() ([]byte, string, error) {
	data, err := migrations.ReadFile("migrations/001_library.sql")
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), err
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
	data, checksum, err := migration()
	if err != nil {
		return err
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != Version {
		return fmt.Errorf("unsupported schema version %d", version)
	}
	if version == 0 {
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES(?,?,?)`, Version, checksum, time.Now().UnixMilli()); err != nil {
			return err
		}
	} else {
		var got string
		if err := tx.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=?`, Version).Scan(&got); err != nil {
			return err
		}
		if got != checksum {
			return fmt.Errorf("schema migration checksum mismatch")
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
	_, checksum, err := migration()
	if err != nil {
		return fail(err)
	}
	var version, count int
	var got, journal string
	if err := read.QueryRowContext(ctx, `SELECT count(*), COALESCE(max(version),0), COALESCE(max(checksum),'') FROM schema_migrations`).Scan(&count, &version, &got); err != nil {
		return fail(fmt.Errorf("run library migrate first: %w", err))
	}
	if count != 1 || version != Version || got != checksum {
		return fail(fmt.Errorf("unsupported or modified schema; run the matching release's migrate command"))
	}
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
