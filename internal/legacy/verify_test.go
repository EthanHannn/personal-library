package legacy

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestVerificationReportsAnomaliesWithoutDiscardingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE books(id INTEGER PRIMARY KEY,title TEXT,author TEXT,filePath TEXT,chapterCount INTEGER,hasContent INTEGER);
CREATE TABLE chapters(id INTEGER PRIMARY KEY,bookId INTEGER,idx INTEGER,title TEXT,startOffset INTEGER,endOffset INTEGER);
CREATE INDEX chapters_bookId_idx_idx ON chapters(bookId,idx);
INSERT INTO books VALUES(1,'正确','作者','test.txt',2,1),(2,'重复','作者','test.txt',1,1),(3,'缺失','作者','test.txt',1,1);
INSERT INTO chapters VALUES(1,1,1,'一',0,5),(2,1,2,'二',5,10),(3,2,1,'一',0,5),(4,2,1,'重复',4,200),(5,99,1,'孤儿',0,10);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	report, err := s.Verify(context.Background(), func(string) (int64, error) { return 10, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Chapters != 5 || report.Books != 3 || report.Error() == nil {
		t.Fatalf("%+v", report)
	}
	codes := map[string]bool{}
	for _, p := range report.Examples {
		codes[p.Code] = true
		if p.Book == 1 {
			t.Fatalf("correct book reported: %+v", p)
		}
	}
	for _, code := range []string{"chapter_sequence", "invalid_offset", "overlapping_chapters", "orphan_chapter", "chapter_count_mismatch", "missing_chapters"} {
		if !codes[code] {
			t.Errorf("missing %s: %+v", code, report)
		}
	}
}
