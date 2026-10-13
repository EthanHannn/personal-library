package legacy

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT, author TEXT, chapterCount INTEGER, filePath TEXT, hasContent INTEGER);
CREATE TABLE chapters (id INTEGER PRIMARY KEY, bookId INTEGER, idx INTEGER, title TEXT, startOffset INTEGER, endOffset INTEGER);
CREATE INDEX chapters_bookId_idx_idx ON chapters(bookId,idx);
INSERT INTO books VALUES(1,'测试','作者',10000,'one.txt',1);
WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000)
INSERT INTO chapters SELECT x,1,x,'章节',0,100 FROM n;`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("DELETE FROM chapters"); err == nil {
		t.Fatal("snapshot is writable")
	}
	plan, err := s.QueryPlan(context.Background())
	if err != nil || !strings.Contains(strings.Join(plan, " "), "chapters_bookId_idx_idx") {
		t.Fatalf("plan %v: %v", plan, err)
	}
	book, chapter, err := s.Chapter(context.Background(), 1, 9999)
	if err != nil || book.Title != "测试" || chapter.Index != 9999 {
		t.Fatalf("%v %+v %+v", err, book, chapter)
	}
	window, err := s.Chapters(context.Background(), 1, 9950, 200)
	if err != nil || len(window) != 50 || window[0].Index != 9951 {
		t.Fatalf("window %d: %v", len(window), err)
	}
	if _, _, err := s.Chapter(context.Background(), 1, 10001); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.Chapter(ctx, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Chapters(context.Background(), 1, 0, 201); err == nil {
		t.Fatal("unbounded window accepted")
	}
}

func TestRejectDuplicateChapter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT, author TEXT, chapterCount INTEGER, filePath TEXT, hasContent INTEGER);
CREATE TABLE chapters (id INTEGER PRIMARY KEY, bookId INTEGER, idx INTEGER, title TEXT, startOffset INTEGER, endOffset INTEGER);
INSERT INTO books VALUES(1,'测试','作者',1,'one.txt',1);
INSERT INTO chapters VALUES(1,1,1,'一',0,1),(2,1,1,'二',1,2);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, err := s.Chapter(context.Background(), 1, 1); !errors.Is(err, ErrAmbiguous) {
		t.Fatal(err)
	}
}
