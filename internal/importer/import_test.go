package importer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
)

// Full legacy shapes, mixed timestamp representations, an uncollected progress
// record, two readers, votes included in historical totals, and out-of-order
// replies. These are synthetic personal-data fixtures, never production data.
const fixtureSQL = `
CREATE TABLE users(id TEXT PRIMARY KEY,username TEXT,nickname TEXT,role TEXT,createdAt DATETIME,updatedAt DATETIME);
CREATE TABLE books(id INTEGER PRIMARY KEY,title TEXT,author TEXT,tag1 TEXT,tag2 TEXT,size TEXT,intro TEXT,status TEXT,submittedById TEXT,postId INTEGER,filePath TEXT,chapterCount INTEGER,wordCount INTEGER,hasContent BOOLEAN,createdAt DATETIME,updatedAt DATETIME,popularity INTEGER,totalScore INTEGER,score REAL,xiancaoCount INTEGER,liangcaoCount INTEGER,gancaoCount INTEGER,kucaoCount INTEGER,ducaoCount INTEGER,coverPath TEXT,coverSource TEXT,coverSourceUrl TEXT,coverFetchedAt DATETIME);
CREATE TABLE chapters(id INTEGER PRIMARY KEY,bookId INTEGER,idx INTEGER,title TEXT,startOffset INTEGER,endOffset INTEGER);
CREATE INDEX chapters_book_idx ON chapters(bookId,idx);
CREATE TABLE bookshelf_entries(id TEXT PRIMARY KEY,userId TEXT,bookId INTEGER,addedAt DATETIME,updatedAt DATETIME);
CREATE TABLE reading_progress(id TEXT PRIMARY KEY,userId TEXT,bookId INTEGER,chapterIdx INTEGER,percent REAL,updatedAt DATETIME);
CREATE TABLE votes(id TEXT PRIMARY KEY,type TEXT,userId TEXT,bookId INTEGER,createdAt DATETIME);
CREATE TABLE comments(id TEXT PRIMARY KEY,content TEXT,userId TEXT,bookId INTEGER,parentId TEXT,likeCount INTEGER,dislikeCount INTEGER,createdAt DATETIME,updatedAt DATETIME);
INSERT INTO users VALUES('default-reader','default-reader','我','ADMIN',1791849600000,'2026-10-13 00:00:00'),('historical-reader','old','原读者','USER','2026-10-13T08:00:00+08:00',1791849600000);
INSERT INTO books VALUES(42,'山间读书','作者','分类','副类','短篇','简介<不执行>','APPROVED','historical-reader',123,'中文\书.txt',2,8,1,1791849600000,'2026-10-13 00:00:00',100,750,7.5,50,30,10,5,5,'/covers/42.jpg','legacy','https://example.test/cover','2026-10-13T00:00:00.123Z');
INSERT INTO books SELECT 88,'共用正文',author,tag1,tag2,size,intro,status,NULL,124,'中文/书.txt',2,8,1,createdAt,updatedAt,0,0,0,0,0,0,0,0,NULL,NULL,NULL,NULL FROM books WHERE id=42;
INSERT INTO chapters VALUES(10,42,1,'第一章',0,6),(20,42,2,'第二章',6,12),(21,88,1,'第一章',0,6),(30,88,2,'第二章',6,12);
INSERT INTO bookshelf_entries VALUES('shelf','historical-reader',42,'2026-10-13 00:00:00',1791849600000);
INSERT INTO reading_progress VALUES('progress','default-reader',88,2,0.625,'2026-10-13T00:00:00.123Z');
INSERT INTO votes VALUES('v1','XIANCAO','default-reader',42,1791849600000),('v2','DUCAO','historical-reader',42,1791849600000);
INSERT INTO comments VALUES('reply','回复😀','default-reader',42,'parent',2,1,1791849600000,'2026-10-13 00:00:00'),('parent','旧书评','historical-reader',42,NULL,5,0,1791849600000,1791849600000);
`

func fixture(t *testing.T, extra string) Options {
	t.Helper()
	dir := t.TempDir()
	novels := filepath.Join(dir, "novels")
	if err := os.MkdirAll(filepath.Join(novels, "中文"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(novels, "中文", "书.txt"), []byte("甲乙丙丁"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := reader.Open(novels, 1)
	if err != nil {
		t.Fatal(err)
	}
	f, err := r.Inspect("中文/书.txt", "utf-8")
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fixtureSQL + extra); err != nil {
		t.Fatal(err)
	}
	db.Close()
	target := filepath.Join(dir, "library.db")
	if err := storage.Migrate(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	return Options{Source: source, Target: target, Novels: novels, Files: []reader.File{f}, PortalUser: "owner", SourceVersion: "synthetic-m1", BatchSize: 2}
}

func TestResumablePersonalDataMigration(t *testing.T) {
	ctx := context.Background()
	o := fixture(t, "")
	before, err := os.ReadFile(o.Source)
	if err != nil {
		t.Fatal(err)
	}
	o.MaxBatches = 1
	resumes := 0
	for {
		report, err := Import(ctx, o)
		if err != nil {
			t.Fatalf("%+v: %v", report, err)
		}
		resumes++
		if report.Complete {
			if len(report.Tables) != len(mappings) || report.Integrity != "ok" || report.Legacy.Chapters != 4 || report.Legacy.Problems != 0 {
				t.Fatalf("incomplete reconciliation: %+v", report)
			}
			for _, check := range report.Tables {
				if check.SourceSHA256 != check.TargetSHA256 || check.SourceRows != check.TargetRows {
					t.Fatal(check)
				}
			}
			break
		}
		db, err := storage.Open(ctx, o.Target)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Ready(ctx, "owner"); err == nil {
			t.Fatal("unfinished database served")
		}
		db.Close()
		if resumes > 30 {
			t.Fatal("resume never completed")
		}
	}
	if resumes < 8 {
		t.Fatal("fixture did not exercise checkpoints")
	}
	// Completed replay is idempotent; no duplicate records or aggregate changes.
	report, err := Import(ctx, o)
	if err != nil || !report.Complete {
		t.Fatalf("%+v: %v", report, err)
	}
	after, err := os.ReadFile(o.Source)
	if err != nil || string(before) != string(after) {
		t.Fatal("source was modified")
	}
	db, err := storage.Open(ctx, o.Target)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ready(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.Ready(ctx, "stranger"); err == nil {
		t.Fatal("wrong portal mapping")
	}
	var id, parent, content, original string
	var percent float64
	var stamp, total, files, books, rows int64
	if err := db.Read.QueryRow(`SELECT reader_id,percent,updated_at FROM reading_progress WHERE id='progress'`).Scan(&id, &percent, &stamp); err != nil {
		t.Fatal(err)
	}
	if id != "default-reader" || percent != 0.625 || stamp != 1791849600123 {
		t.Fatalf("progress %s %f %d", id, percent, stamp)
	}
	if err := db.Read.QueryRow(`SELECT parent_id,content FROM comments WHERE id='reply'`).Scan(&parent, &content); err != nil {
		t.Fatal(err)
	}
	if parent != "parent" || content != "回复😀" {
		t.Fatal("reply changed")
	}
	if err := db.Read.QueryRow(`SELECT total_score FROM rating_snapshots WHERE book_id=42`).Scan(&total); err != nil || total != 750 {
		t.Fatal("vote totals changed", err)
	}
	if err := db.Read.QueryRow(`SELECT imported_type FROM votes WHERE id='v1'`).Scan(&original); err != nil || original != "XIANCAO" {
		t.Fatal("vote baseline lost", err)
	}
	if err := db.Read.QueryRow(`SELECT (SELECT count(*) FROM files),(SELECT count(*) FROM book_contents),(SELECT count(*) FROM shelf_entries)`).Scan(&files, &books, &rows); err != nil {
		t.Fatal(err)
	}
	if files != 1 || books != 2 || rows != 1 {
		t.Fatal("shared file or shelf relation changed")
	}
}

func TestInvalidSourcesNeverBecomeReady(t *testing.T) {
	for name, sql := range map[string]string{
		"orphan reader":          `UPDATE reading_progress SET userId='missing';`,
		"bad progress":           `UPDATE reading_progress SET chapterIdx=99;`,
		"percent":                `UPDATE reading_progress SET percent=1.1;`,
		"offset":                 `UPDATE chapters SET endOffset=999 WHERE id=30;`,
		"overlap":                `UPDATE chapters SET startOffset=3 WHERE id=20;`,
		"duplicate chapter":      `UPDATE chapters SET idx=1 WHERE id=20;`,
		"missing file":           `UPDATE books SET filePath='missing.txt' WHERE id=42;`,
		"bad date":               `UPDATE users SET createdAt='not-a-date';`,
		"missing parent":         `UPDATE comments SET parentId='missing' WHERE id='reply';`,
		"cycle":                  `UPDATE comments SET parentId='reply' WHERE id='parent';`,
		"cross-book reply":       `UPDATE comments SET bookId=88 WHERE id='reply';`,
		"missing default reader": `DELETE FROM users WHERE id='default-reader';`,
	} {
		t.Run(name, func(t *testing.T) {
			o := fixture(t, sql)
			report, err := Import(context.Background(), o)
			if err == nil || report.Complete {
				t.Fatalf("bad source accepted: %+v", report)
			}
			db, err := storage.Open(context.Background(), o.Target)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.Ready(context.Background(), "owner"); err == nil {
				t.Fatal("bad import is ready")
			}
		})
	}
}

func TestResumeRejectsChangedInputs(t *testing.T) {
	for _, kind := range []string{"snapshot", "file", "manifest", "identity", "release"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			o := fixture(t, "")
			o.MaxBatches = 1
			if _, err := Import(ctx, o); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "snapshot":
				db, err := sql.Open("sqlite", o.Source)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`UPDATE books SET title='changed' WHERE id=42`); err != nil {
					t.Fatal(err)
				}
				db.Close()
			case "file":
				path := filepath.Join(o.Novels, "中文", "书.txt")
				if err := os.WriteFile(path, []byte("戊己庚辛"), 0600); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(0, o.Files[0].ModifiedNS)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				o.Files[0].Encoding = "gb18030"
			case "identity":
				o.PortalUser = "another"
			case "release":
				o.SourceVersion = "another"
			}
			if report, err := Import(ctx, o); err == nil || report.Complete {
				t.Fatalf("changed %s accepted", kind)
			}
		})
	}
}

func TestAtomicBatchAndReconciliation(t *testing.T) {
	ctx := context.Background()
	o := fixture(t, `UPDATE chapters SET idx=1 WHERE id=20;`)
	if _, err := Import(ctx, o); err == nil {
		t.Fatal("duplicate accepted")
	}
	db, err := storage.Open(ctx, o.Target)
	if err != nil {
		t.Fatal(err)
	}
	var n, last int
	if err := db.Read.QueryRow(`SELECT count(*) FROM chapters`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := db.Read.QueryRow(`SELECT last_rowid FROM import_runs`).Scan(&last); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if n != 0 || last != 0 {
		t.Fatal("failed batch was partially committed")
	}

	o = fixture(t, "")
	o.MaxBatches = 1
	if _, err := Import(ctx, o); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(ctx, o.Target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write.Exec(`UPDATE readers SET nickname='changed' WHERE id='historical-reader'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	o.MaxBatches = 0
	if report, err := Import(ctx, o); err == nil || !strings.Contains(err.Error(), "reconciliation") || report.Complete {
		t.Fatalf("changed fields missed: %+v %v", report, err)
	}
}

func TestRejectSameFileAndActiveWAL(t *testing.T) {
	ctx := context.Background()
	o := fixture(t, "")
	same := o
	same.Target = o.Source
	if _, err := Import(ctx, same); err == nil {
		t.Fatal("same file accepted")
	}
	db, err := sql.Open("sqlite", o.Source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; UPDATE users SET nickname='test'`); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, o); err == nil || !strings.Contains(err.Error(), "offline backup") {
		t.Fatalf("active WAL accepted: %v", err)
	}
}

func TestCancelledImportResumes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	o := fixture(t, "")
	cancel()
	if _, err := Import(ctx, o); err == nil {
		t.Fatal("cancel ignored")
	}
	if report, err := Import(context.Background(), o); err != nil || !report.Complete {
		t.Fatalf("cancel prevented resume: %v", err)
	}
}
