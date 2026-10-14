package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
)

func serviceFixture(t *testing.T) (http.Handler, *storage.Database) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "library.db")
	if err := storage.Migrate(ctx, path); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	text := "第一章\n这是正文😀。"
	if err := os.WriteFile(filepath.Join(dir, "novel.txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := reader.Open(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	file, err := r.Inspect("novel.txt", "utf-8")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Write.Exec(`
INSERT INTO readers VALUES('default-reader','default-reader','我的昵称','ADMIN','owner',0,0);
INSERT INTO books VALUES(42,'书名','作者','分类','副类','短篇','简介','APPROVED',NULL,1,'novel.txt',1,8,1,0,0);
INSERT INTO books SELECT 88,'待审书',author,tag1,tag2,size,intro,'PENDING',NULL,NULL,NULL,0,0,0,0,0 FROM books WHERE id=42;
INSERT INTO rating_snapshots VALUES(42,1,10,10,1,0,0,0,0),(88,0,0,0,0,0,0,0,0);
INSERT INTO files VALUES(1,'novel.txt','utf-8',?,?,?);
INSERT INTO book_contents VALUES(42,1,1,1);
INSERT INTO chapters VALUES(10,42,1,1,'第一章',0,?);
INSERT INTO import_runs(id,source_sha256,manifest_sha256,source_version,portal_user,status,started_at) VALUES(1,'test','test','synthetic','owner','complete',0);
`, file.Size, file.ModifiedNS, strings.Repeat("a", 64), file.Size)
	if err != nil {
		t.Fatal(err)
	}
	config, err := gateway.Parse("owner", "https://library.test", "https://auth.test", "https://portal.test")
	if err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>书屋</title><div id=app></div>")}, "assets/app-test.js": {Data: []byte("console.log('shell')")}}
	h, err := New(ctx, db, r, config, assets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write.Exec(`UPDATE import_runs SET status='running'`); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ctx, db, r, config, assets); err == nil {
		t.Fatal("unfinished import served")
	}
	if _, err := db.Write.Exec(`UPDATE import_runs SET status='complete'`); err != nil {
		t.Fatal(err)
	}
	return h, db
}

func call(h http.Handler, method, url, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, nil)
	if user != "" {
		r.Header.Set("Remote-User", user)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestStaticAndAPIBoundaries(t *testing.T) {
	h, _ := serviceFixture(t)
	for _, url := range []string{"/", "/library", "/book/42", "/read/42?c=1", "/assets/app-test.js", "/covers/42.jpg", "/api/v1/me"} {
		for user, code := range map[string]int{"": 401, "stranger": 403} {
			w := call(h, "GET", url, user)
			if w.Code != code || !json.Valid(w.Body.Bytes()) {
				t.Fatalf("%s user %s: %d %s", url, user, w.Code, w.Body.String())
			}
		}
	}
	for _, tc := range []struct {
		url   string
		code  int
		media string
	}{
		{"/", 200, "text/html"}, {"/book/42", 200, "text/html"}, {"/read/42?c=1", 200, "text/html"},
		{"/assets/app-test.js", 200, "javascript"}, {"/assets/missing.js", 404, "application/json"},
		{"/assets/", 404, "application/json"}, {"/api", 404, "application/json"}, {"/api/missing", 404, "application/json"},
		{"/covers/42.jpg", 404, "application/json"}, {"/missing", 404, "application/json"}, {"/book/nope", 404, "application/json"},
		{"/api/v1/books/88", 404, "application/json"}, {"/api/v1/books?limit=49", 400, "application/json"},
		{"/api/v1/books?after=-1", 400, "application/json"}, {"/api/v1/books/42/chapters", 200, "application/json"},
	} {
		w := call(h, "GET", tc.url, "owner")
		if w.Code != tc.code || !strings.Contains(w.Header().Get("Content-Type"), tc.media) || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s: %d %v %s", tc.url, w.Code, w.Header(), w.Body.String())
		}
	}
	if w := call(h, "GET", "/api/health", ""); w.Code != 200 || strings.Contains(w.Body.String(), "novel") {
		t.Fatal("health contract", w.Body.String())
	}
	if w := call(h, "GET", "/login?return=https://evil.test", "owner"); w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal("old login redirect")
	}
	// Gateway origin/media checks still surround unknown and future write APIs.
	for _, tc := range []struct {
		origin, media string
		want          int
	}{
		{"https://sibling.test", "application/json", 403}, {"https://library.test", "text/plain", 415}, {"https://library.test", "application/json", 405},
	} {
		req := httptest.NewRequest("POST", "/api/v1/books/42", strings.NewReader("{}"))
		req.Header.Set("Remote-User", "owner")
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", tc.media)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("write: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestBusinessReadsAndIdentity(t *testing.T) {
	h, _ := serviceFixture(t)
	checks := map[string][]string{
		"/api/v1/me":                          {`"id":"default-reader"`, `"nickname":"我的昵称"`, `"logoutUrl":"https://auth.test/logout"`},
		"/api/v1/profile":                     {`"accountUrl":"https://portal.test/settings"`, `"nickname":"我的昵称"`},
		"/api/v1/books?limit=1":               {`"id":42`, `"nextAfter":null`},
		"/api/v1/books/42":                    {`"intro":"简介"`, `"hasContent":true`},
		"/api/v1/books/42/chapters/1/content": {`"text":"第一章\n这是正文😀。"`, `"nextCursor":null`, `"idx":1`},
	}
	for url, parts := range checks {
		w := call(h, "GET", url, "owner")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, part := range parts {
			if !strings.Contains(w.Body.String(), part) {
				t.Fatal(fmt.Sprintf("%s missing %s: %s", url, part, w.Body.String()))
			}
		}
		if strings.Contains(w.Body.String(), "novel.txt") || strings.Contains(w.Body.String(), "待审书") {
			t.Fatal("private storage or pending metadata leaked")
		}
	}
}
