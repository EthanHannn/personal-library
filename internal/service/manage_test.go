package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestManagedMetadataApprovalAndConcurrentChanges(t *testing.T) {
	h, db := serviceFixture(t)
	for _, path := range []string{"/manage", "/api/v1/manage/books", "/api/v1/manage/jobs", "/api/v1/books/42/cover"} {
		for user, want := range map[string]int{"": 401, "other": 403} {
			if w := call(h, "GET", path, user); w.Code != want {
				t.Fatal(path, w.Code)
			}
		}
	}
	w := call(h, "GET", "/api/v1/manage/books?filter=pending", "owner")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "待审书") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{"title":"bad","status":"APPROVED","hasContent":true}`, `{"title":"","status":"APPROVED"}`, `{"title":"bad","status":"PUBLIC"}`, `{} {}`} {
		w := shelfRequest(h, "POST", "/api/v1/manage/books", body, "owner", "https://library.test")
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	body := `{"title":"新书","author":"新作者","status":"PENDING"}`
	if w := shelfRequest(h, "POST", "/api/v1/manage/books", body, "owner", "https://other.test"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = shelfRequest(h, "POST", "/api/v1/manage/books", body, "owner", "https://library.test")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var id struct{ BookID int64 }
	json.Unmarshal(w.Body.Bytes(), &id)
	if w := call(h, "GET", fmt.Sprintf("/api/v1/books/%d", id.BookID), "owner"); w.Code != 404 {
		t.Fatal("pending published", w.Code)
	}
	var version int64
	db.Read.QueryRow(`SELECT updated_at FROM books WHERE id=?`, id.BookID).Scan(&version)
	body = fmt.Sprintf(`{"title":"新书","author":"新作者","intro":"补充简介","status":"APPROVED","expectedVersion":%d}`, version)
	path := fmt.Sprintf("/api/v1/manage/books/%d", id.BookID)
	if w := shelfRequest(h, "PUT", path, body, "owner", "https://library.test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := shelfRequest(h, "PUT", path, body, "owner", "https://library.test"); w.Code != 409 {
		t.Fatal("stale edit", w.Code, w.Body.String())
	}
	if w := call(h, "GET", fmt.Sprintf("/api/v1/books/%d", id.BookID), "owner"); w.Code != 200 || !strings.Contains(w.Body.String(), "补充简介") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := shelfRequest(h, "PUT", "/api/v1/manage/books/42", `{"title":"书名改","author":"作者","status":"APPROVED","expectedVersion":0}`, "owner", "https://library.test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var score float64
	var chapters, content int
	db.Read.QueryRow(`SELECT score,chapter_count,has_content FROM books JOIN rating_snapshots ON book_id=id WHERE id=42`).Scan(&score, &chapters, &content)
	if score != 10 || chapters != 1 || content != 1 {
		t.Fatal("metadata edit modified source/content", score, chapters, content)
	}
}
