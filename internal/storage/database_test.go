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
