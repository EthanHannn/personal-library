package storage

import (
	"context"
	"database/sql"
	"fmt"
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

func TestUpgradeV3PreservesJobsAndSupportsMultipleSources(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3.db")
	db, err := Connect(ctx, path, "rwc", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 3; version++ {
		raw, checksum, e := migration(version)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(raw)); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(`INSERT INTO schema_migrations VALUES(?,?,0)`, version, checksum); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = db.Exec(`INSERT INTO books VALUES(1,'书','作者','','','','','APPROVED',NULL,NULL,NULL,1,1,1,0,0);
INSERT INTO book_sources VALUES(1,'qidian','123','https://www.qidian.com/book/123/',0);
INSERT INTO library_jobs(id,kind,dedupe,status,payload,message,created_at,updated_at) VALUES('old','collect','hash','queued','{}','confirm',1,1);
INSERT INTO job_chapters VALUES('old',1,'第一章',0,100);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = Migrate(ctx, path); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var message, title string
	if err = upgraded.Read.QueryRow(`SELECT message,title FROM library_jobs JOIN job_chapters ON job_id=id WHERE id='old'`).Scan(&message, &title); err != nil || message != "confirm" || title != "第一章" {
		t.Fatal(message, title, err)
	}
	if _, err = upgraded.Write.Exec(`INSERT INTO book_sources(book_id,source,source_id,source_url,complete) VALUES(1,'another','456','https://example.com/book',1)`); err != nil {
		t.Fatal("second source", err)
	}
	if _, err = upgraded.Write.Exec(`INSERT INTO job_chapters VALUES('missing',1,'坏外键',0,100)`); err == nil {
		t.Fatal("lost job foreign key")
	}
	rows, err := upgraded.Read.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("broken foreign keys")
	}
}

func TestUpgradeV4FileEncodingAndFailedMigrationRollback(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "v4.db")
			db, err := Connect(ctx, path, "rwc", 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`); err != nil {
				t.Fatal(err)
			}
			for version := 1; version <= 4; version++ {
				raw, checksum, err := migration(version)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(string(raw)); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?,0)`, version, checksum); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO books VALUES(1,'书','作者','','','','','APPROVED',NULL,NULL,NULL,1,1,1,0,0);
INSERT INTO files VALUES(7,'original.txt','gb18030',100,456,printf('%064d',0));
INSERT INTO book_contents VALUES(1,1,7,1);
INSERT INTO chapters VALUES(42,1,1,1,'原章名',0,100);`); err != nil {
				t.Fatal(err)
			}
			if corrupt {
				if _, err := db.Exec(`PRAGMA foreign_keys=OFF; UPDATE book_contents SET file_id=999`); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			err = Migrate(ctx, path)
			if corrupt {
				if err == nil {
					t.Fatal("invalid foreign key accepted")
				}
				db, err := Connect(ctx, path, "ro", 1)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				var version, files int
				if err := db.QueryRow(`SELECT max(version),(SELECT count(*) FROM files) FROM schema_migrations`).Scan(&version, &files); err != nil || version != 4 || files != 1 {
					t.Fatal("failed migration did not roll back", version, files, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			upgraded, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer upgraded.Close()
			var title, encoding string
			var id, size, stamp int64
			if err := upgraded.Read.QueryRow(`SELECT c.id,c.title,f.encoding,f.size,f.modified_ns FROM chapters c JOIN book_contents bc ON bc.book_id=c.book_id JOIN files f ON f.id=bc.file_id`).Scan(&id, &title, &encoding, &size, &stamp); err != nil || id != 42 || title != "原章名" || encoding != "gb18030" || size != 100 || stamp != 456 {
				t.Fatal("original file or chapter changed", err)
			}
			if _, err := upgraded.Write.Exec(`INSERT INTO files VALUES(8,'utf16.txt','utf-16le',120,789,printf('%064d',0))`); err != nil {
				t.Fatal("UTF-16 rejected", err)
			}
			if _, err := upgraded.Write.Exec(`DELETE FROM files WHERE id=7`); err == nil {
				t.Fatal("file foreign key lost")
			}
		})
	}
}
