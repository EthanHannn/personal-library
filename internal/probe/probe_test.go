package probe

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/legacy"
	"github.com/EthanHannn/personal-library/internal/reader"
)

func testApp(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	text := strings.Repeat("正文𠀀😀\r\n", 50000)
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := reader.Open(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	file, err := r.Inspect("sample.txt", "utf-8")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "snapshot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT, author TEXT, chapterCount INTEGER, filePath TEXT, hasContent INTEGER);
CREATE TABLE chapters (id INTEGER PRIMARY KEY, bookId INTEGER, idx INTEGER, title TEXT, startOffset INTEGER, endOffset INTEGER);
CREATE INDEX chapters_bookId_idx_idx ON chapters(bookId,idx);
INSERT INTO books VALUES(1,'样本','作者',2,'sample.txt',1),(2,'缺失','作者',1,'missing.txt',1);`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO chapters VALUES(1,1,1,'第一章',0,?),(2,1,2,'第二章',0,?),(3,2,1,'第一章',0,10)", len(text), len(text))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := legacy.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := New(s, r, Manifest{Version: 1, Files: []reader.File{file}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := gateway.Parse("owner", "https://library.test", "https://auth.test")
	if err != nil {
		t.Fatal(err)
	}
	return a.Handler(c), text, path
}

func request(h http.Handler, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if user != "" {
		r.Header.Set("Remote-User", user)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestBoundedContentAndSignedCursors(t *testing.T) {
	h, want, path := testApp(t)
	var got strings.Builder
	next := ""
	firstCursor := ""
	for {
		w := request(h, "/api/v1/books/1/chapters/1/content?cursor="+url.QueryEscape(next), "owner")
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var body struct {
			Text       string
			NextCursor *string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Text) > reader.MaxBlockBytes {
			t.Fatal("unbounded text")
		}
		got.WriteString(body.Text)
		if body.NextCursor == nil {
			break
		}
		next = *body.NextCursor
		if firstCursor == "" {
			firstCursor = next
		}
	}
	if got.String() != want {
		t.Fatal("body was truncated or corrupted")
	}
	for _, target := range []string{
		"/api/v1/books/1/chapters/1/content?cursor=broken",
		"/api/v1/books/1/chapters/1/content?cursor=" + firstCursor + "x",
		"/api/v1/books/1/chapters/2/content?cursor=" + firstCursor,
	} {
		w := request(h, target, "owner")
		if w.Code != 400 {
			t.Fatalf("cursor accepted: %d", w.Code)
		}
	}
	changed := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	w := request(h, "/api/v1/books/1/chapters/1/content", "owner")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "source_changed") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestAPIContract(t *testing.T) {
	h, _, _ := testApp(t)
	for _, tc := range []struct {
		path, user string
		code       int
	}{
		{"/api/health", "", 200},
		{"/api/v1/books/1/chapters/1/content", "", 401},
		{"/api/v1/books/1/chapters/1/content", "stranger", 403},
		{"/api/v1/books/1/chapters?after=1&limit=1", "owner", 200},
		{"/api/v1/books/1/chapters?limit=201", "owner", 400},
		{"/api/v1/books/1/chapters?after=-1", "owner", 400},
		{"/api/v1/books/1/chapters/1x/content", "owner", 400},
		{"/api/v1/books/999/chapters/1/content", "owner", 404},
		{"/api/v1/books/2/chapters/1/content", "owner", 409},
		{"/api/v1/me", "owner", 200},
		{"/api/missing", "owner", 404},
		{"/assets/missing.js", "owner", 404},
	} {
		w := request(h, tc.path, tc.user)
		if w.Code != tc.code || !json.Valid(w.Body.Bytes()) || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}

type waitingWriter struct {
	*httptest.ResponseRecorder
	entered chan<- struct{}
	release <-chan struct{}
}

func (w waitingWriter) Write(data []byte) (int, error) {
	w.entered <- struct{}{}
	<-w.release
	return w.ResponseRecorder.Write(data)
}

func TestSlowResponsesBoundConcurrency(t *testing.T) {
	h, _, _ := testApp(t)
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	for range 8 {
		wg.Go(func() {
			r := httptest.NewRequest("GET", "/api/v1/me", nil)
			r.Header.Set("Remote-User", "owner")
			h.ServeHTTP(waitingWriter{httptest.NewRecorder(), entered, release}, r)
		})
	}
	for range 8 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("requests did not reach response writer")
		}
	}
	w := request(h, "/api/v1/me", "owner")
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Errorf("overload response: %d %s", w.Code, w.Body.String())
	}
}
