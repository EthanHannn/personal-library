package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitMigrationsAndConnectionPolicy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if db, err := Open(ctx, path); err == nil {
		db.Close()
		t.Fatal("created an unmigrated database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read-only open created a file")
	}
	for range 2 {
		if err := Migrate(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ready(ctx, "owner"); err == nil {
		t.Fatal("empty import is ready")
	}
	// Hold both read connections simultaneously, then force replacements.
	for range 2 {
		var held []*sql.Conn
		for range 2 {
			conn, err := db.Read.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, conn)
			for pragma, want := range map[string]int{"foreign_keys": 1, "query_only": 1, "busy_timeout": 2000, "mmap_size": 0} {
				var got int
				if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
					t.Fatalf("%s = %d: %v", pragma, got, err)
				}
			}
		}
		for _, conn := range held {
			conn.Close()
		}
		db.Read.SetMaxIdleConns(0)
		db.Read.SetMaxIdleConns(2)
	}
	var sync int
	if err := db.Write.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil || sync != 2 {
		t.Fatalf("durability %d: %v", sync, err)
	}
	if _, err := db.Read.Exec(`DELETE FROM schema_migrations`); err == nil {
		t.Fatal("reader permitted a write")
	}
	if _, err := db.Write.Exec(`INSERT INTO shelf_entries VALUES('x','missing',1,0,0)`); err == nil {
		t.Fatal("foreign key accepted")
	}
	if _, err := db.Write.Exec(`UPDATE schema_migrations SET checksum='bad'`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, path); err == nil {
		t.Fatal("changed migration accepted")
	}
	if other, err := Open(ctx, path); err == nil {
		other.Close()
		t.Fatal("changed schema opened")
	}
}

func TestRefuseForeignDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := Connect(ctx, path, "rwc", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE books(id INTEGER); INSERT INTO books VALUES(42)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, path); err == nil {
		t.Fatal("legacy schema migrated")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("foreign database changed")
	}
}

func TestUpgradePreservesV1DataAndKey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := Connect(ctx, path, "rwc", 1)
	if err != nil {
		t.Fatal(err)
	}
	v1, checksum, err := migration(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(v1)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT;
INSERT INTO schema_migrations VALUES(1,?,0);
INSERT INTO readers VALUES('r','old','昵称','USER',NULL,123,456);
INSERT INTO books VALUES(1,'书','作者','','','','','APPROVED',NULL,NULL,NULL,1,1,1,0,0);
INSERT INTO reading_progress VALUES('original-id','r',1,1,0.71,789,NULL,NULL,0);`, checksum); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if other, err := Open(ctx, path); err == nil {
		other.Close()
		t.Fatal("v1 silently opened before upgrade")
	}
	if err := Migrate(ctx, path); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var id string
	var percent float64
	var updated, revision int64
	if err := upgraded.Read.QueryRow(`SELECT id,percent,updated_at,revision FROM reading_progress`).Scan(&id, &percent, &updated, &revision); err != nil || id != "original-id" || percent != 0.71 || updated != 789 || revision != 0 {
		t.Fatal("legacy progress changed", err)
	}
	var before, after []byte
	if err := upgraded.Read.QueryRow(`SELECT signing_key FROM reader_keys`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Read.QueryRow(`SELECT signing_key FROM reader_keys`).Scan(&after); err != nil || string(before) != string(after) {
		t.Fatal("key replaced", err)
	}
	if _, err := upgraded.Write.Exec(`DELETE FROM schema_migrations WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, path); err == nil {
		t.Fatal("missing history accepted")
	}
}
