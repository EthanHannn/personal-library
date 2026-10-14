package service

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestRecommendationsUseLocalRelevantReadableBooks(t *testing.T) {
	h := catalogFixture(t)
	w := call(h, "GET", "/api/v1/books/101/recommendations", "owner")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Entries []struct {
			Book   Book
			Reason string
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Book.ID != 102 || result.Entries[0].Reason != "同一作者" {
		t.Fatalf("irrelevant, duplicate or unreadable recommendations: %+v", result)
	}
	if w := call(h, "GET", "/api/v1/books/103/recommendations", "owner"); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"entries":[]}` {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/v1/books/88/recommendations", "/api/v1/books/999/recommendations"} {
		if w := call(h, "GET", path, "owner"); w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestProfileVotesIsolationFiltersAndStablePagination(t *testing.T) {
	h, db := serviceFixture(t)
	_, err := db.Write.Exec(`INSERT INTO readers VALUES('other','other',NULL,'USER',NULL,0,0);
UPDATE books SET status='APPROVED' WHERE id=88;
INSERT INTO votes VALUES('mine-1','XIANCAO',NULL,'default-reader',42,10),('mine-2','KUCAO',NULL,'default-reader',88,10),('other-vote','DUCAO',NULL,'other',42,20);
INSERT INTO shelf_entries VALUES('shelf','default-reader',42,0,0);
INSERT INTO reading_progress(id,reader_id,book_id,chapter_idx,percent,updated_at) VALUES('progress','default-reader',42,1,0.4,5);`)
	if err != nil {
		t.Fatal(err)
	}
	w := call(h, "GET", "/api/v1/profile", "owner")
	var profile struct {
		Nickname, AccountURL string
		Counts               struct{ Shelf, Reading, Votes int }
		Reading              []collectionEntry
	}
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	if profile.Counts.Votes != 2 || profile.Counts.Shelf != 1 || profile.Counts.Reading != 1 || len(profile.Reading) != 1 || profile.Nickname != "我的昵称" || profile.AccountURL != "https://portal.test/settings" {
		t.Fatalf("profile %+v", profile)
	}
	var page struct {
		Entries []struct {
			Book Book
			Type string
		}
		NextCursor *string
	}
	read := func(query string) {
		t.Helper()
		w := call(h, "GET", "/api/v1/votes?"+query, "owner")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
	}
	read("limit=1")
	if len(page.Entries) != 1 || page.Entries[0].Book.ID != 42 || page.NextCursor == nil {
		t.Fatalf("first %+v", page)
	}
	cursor := *page.NextCursor
	read("limit=1&cursor=" + url.QueryEscape(cursor))
	if len(page.Entries) != 1 || page.Entries[0].Book.ID != 88 || page.NextCursor != nil {
		t.Fatalf("next %+v", page)
	}
	read("type=KUCAO")
	if len(page.Entries) != 1 || page.Entries[0].Book.ID != 88 {
		t.Fatalf("filter %+v", page)
	}
	if _, err := db.Write.Exec(`UPDATE votes SET type='LIANGCAO' WHERE id='mine-2'`); err != nil {
		t.Fatal(err)
	}
	read("type=KUCAO")
	if len(page.Entries) != 0 {
		t.Fatal("stale vote filter")
	}
	read("type=LIANGCAO")
	if len(page.Entries) != 1 {
		t.Fatal("updated vote missing")
	}
	for _, query := range []string{"type=NOPE", "limit=49", "type=KUCAO&cursor=" + url.QueryEscape(cursor)} {
		if w := call(h, "GET", "/api/v1/votes?"+query, "owner"); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if _, err := db.Write.Exec(`UPDATE books SET status='PENDING' WHERE id=88`); err != nil {
		t.Fatal(err)
	}
	read("")
	if len(page.Entries) != 1 {
		t.Fatal("pending book leaked")
	}
	for _, path := range []string{"/profile", "/api/v1/profile", "/api/v1/votes", "/api/v1/books/42/recommendations"} {
		for user, want := range map[string]int{"": 401, "other": 403} {
			if w := call(h, "GET", path, user); w.Code != want {
				t.Fatal(path, w.Code)
			}
		}
	}
}
