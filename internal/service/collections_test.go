package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/EthanHannn/personal-library/internal/catalog"
)

func shelfRequest(h http.Handler, method, path, body, user, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Remote-User", user)
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func collectionResult(t *testing.T, h http.Handler, path string) collectionPage {
	t.Helper()
	w := call(h, "GET", path, "owner")
	var page collectionPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Entries == nil {
		t.Fatal(path, w.Code, w.Body.String())
	}
	return page
}

func TestShelfKeepsUnstartedBooksAndIndependentReading(t *testing.T) {
	h, db := serviceFixture(t)
	if _, err := db.Write.Exec(`INSERT INTO books SELECT 89,'未读低分书',author,tag1,tag2,size,intro,'APPROVED',NULL,NULL,NULL,0,0,0,0,0 FROM books WHERE id=42;
INSERT INTO rating_snapshots VALUES(89,0,0,0,0,0,0,0,0);`); err != nil {
		t.Fatal(err)
	}
	write := func(method string, id int) {
		t.Helper()
		w := shelfRequest(h, method, fmt.Sprintf("/api/v1/books/%d/shelf", id), "{}", "owner", "https://library.test")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	write("PUT", 89)
	page := collectionResult(t, h, "/api/v1/shelf")
	if len(page.Entries) != 1 || page.Entries[0].Book.HasContent || page.Entries[0].Progress != nil || page.Counts != (collectionCounts{Shelf: 1}) {
		t.Fatal(page)
	}
	// A lost acknowledgement or simultaneous add must neither duplicate nor reorder.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			w := shelfRequest(h, "PUT", "/api/v1/books/89/shelf", "{}", "owner", "https://library.test")
			if w.Code != 200 {
				t.Errorf("concurrent add: %d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	if again := collectionResult(t, h, "/api/v1/shelf"); !reflect.DeepEqual(page, again) {
		t.Fatal("repeated collection changed its time or created progress", again)
	}
	s := initial(t, h, "/api/v1/books/42/reader")
	m := catalog.ProgressMutation{Position: catalog.Position{ChapterIdx: 1, Percent: .6, ContentVersion: s.Content.ContentVersion, Anchor: &catalog.Anchor{Block: s.Content.BlockToken, Paragraph: 4, Character: 5}}, BaseRevision: 0, MutationID: "shelf-mutation", DeviceID: "shelf-device", ClientSeq: 1}
	if w := mutate(h, m); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := call(h, "GET", "/api/v1/books/42/progress", "owner").Body.String()
	reading := collectionResult(t, h, "/api/v1/reading")
	if len(reading.Entries) != 1 || reading.Entries[0].Book.ID != 42 || reading.Entries[0].InShelf || reading.Counts.Reading != 1 {
		t.Fatal("reading must not require collection", reading)
	}
	write("PUT", 42)
	if !initial(t, h, "/api/v1/books/42/reader").InShelf {
		t.Fatal("reader did not see shelf addition")
	}
	write("DELETE", 42)
	write("DELETE", 42)
	if after := call(h, "GET", "/api/v1/books/42/progress", "owner").Body.String(); after != before {
		t.Fatal("removal changed exact persisted progress", before, after)
	}
	reading = collectionResult(t, h, "/api/v1/reading")
	if reading.Entries[0].InShelf || reading.Counts.Shelf != 1 || reading.Counts.Reading != 1 {
		t.Fatal(reading)
	}
	if w := call(h, "GET", "/api/v1/books/89", "owner"); !strings.Contains(w.Body.String(), `"inShelf":true`) || !strings.Contains(w.Body.String(), `"progress":null`) {
		t.Fatal("detail collection state", w.Body.String())
	}
}

func TestCollectionPaginationTiesAndReaderIsolation(t *testing.T) {
	h, db := serviceFixture(t)
	if _, err := db.Write.Exec(`INSERT INTO readers VALUES('historical','historical',NULL,'USER',NULL,0,0);
INSERT INTO shelf_entries VALUES('other-shelf','historical',42,9000,9000);
INSERT INTO reading_progress(id,reader_id,book_id,chapter_idx,percent,updated_at) VALUES('other-progress','historical',42,1,.9,9000);
INSERT INTO shelf_entries VALUES('hidden-shelf','default-reader',88,9000,9000);
INSERT INTO reading_progress(id,reader_id,book_id,chapter_idx,percent,updated_at) VALUES('hidden-progress','default-reader',88,1,.9,9000);`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 7; i++ {
		id := 100 + i
		for _, insert := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO books SELECT ?,?,author,tag1,tag2,size,intro,status,NULL,NULL,NULL,0,0,0,0,0 FROM books WHERE id=42`, []any{id, fmt.Sprintf("书 %d", i)}},
			{`INSERT INTO rating_snapshots VALUES(?,0,0,0,0,0,0,0,0)`, []any{id}},
			{`INSERT INTO shelf_entries VALUES(?,'default-reader',?,1000,1000)`, []any{fmt.Sprintf("s-%d", i), id}},
			{`INSERT INTO reading_progress(id,reader_id,book_id,chapter_idx,percent,updated_at) VALUES(?,'default-reader',?,1,.1,1000)`, []any{fmt.Sprintf("p-%d", i), id}},
		} {
			if _, err := db.Write.Exec(insert.sql, insert.args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, kind := range []string{"shelf", "reading"} {
		var got []int64
		path := "/api/v1/" + kind + "?limit=3"
		for {
			page := collectionResult(t, h, path)
			if page.Counts != (collectionCounts{Shelf: 7, Reading: 7}) || len(page.Entries) > 3 {
				t.Fatal("unbounded or non-current reader counts", page)
			}
			for _, entry := range page.Entries {
				got = append(got, entry.Book.ID)
			}
			if page.NextCursor == nil {
				break
			}
			other := "reading"
			if kind == other {
				other = "shelf"
			}
			if w := call(h, "GET", "/api/v1/"+other+"?cursor="+url.QueryEscape(*page.NextCursor), "owner"); w.Code != 400 {
				t.Fatal("cross-list cursor accepted")
			}
			path = "/api/v1/" + kind + "?limit=3&cursor=" + url.QueryEscape(*page.NextCursor)
		}
		if !reflect.DeepEqual(got, []int64{101, 102, 103, 104, 105, 106, 107}) {
			t.Fatal("ties skipped or duplicated", kind, got)
		}
	}
	first := collectionResult(t, h, "/api/v1/shelf?limit=3")
	if _, err := db.Write.Exec(`DELETE FROM shelf_entries WHERE id='s-2'; INSERT INTO shelf_entries VALUES('new-shelf','default-reader',42,2000,2000);`); err != nil {
		t.Fatal(err)
	}
	next := collectionResult(t, h, "/api/v1/shelf?limit=3&cursor="+*first.NextCursor)
	if len(next.Entries) != 3 || next.Entries[0].Book.ID != 104 || next.Entries[2].Book.ID != 106 {
		t.Fatal("add/remove ahead of cursor shifted next page", next)
	}
}

func TestCollectionAuthorizationValidationAndHome(t *testing.T) {
	h, _ := serviceFixture(t)
	for _, tc := range []struct {
		method, path, body, user, origin string
		code                             int
	}{
		{"PUT", "/api/v1/books/42/shelf", "{}", "", "https://library.test", 401},
		{"DELETE", "/api/v1/books/42/shelf", "{}", "stranger", "https://library.test", 403},
		{"PUT", "/api/v1/books/42/shelf", "{}", "owner", "https://sibling.test", 403},
		{"PUT", "/api/v1/books/42/shelf", `{"readerId":"historical"}`, "owner", "https://library.test", 400},
		{"PUT", "/api/v1/books/42/shelf", "{}{}", "owner", "https://library.test", 400},
		{"PUT", "/api/v1/books/42/shelf", "null", "owner", "https://library.test", 400},
		{"PUT", "/api/v1/books/88/shelf", "{}", "owner", "https://library.test", 404},
		{"PUT", "/api/v1/books/9999/shelf", "{}", "owner", "https://library.test", 404},
	} {
		w := shelfRequest(h, tc.method, tc.path, tc.body, tc.user, tc.origin)
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/shelf?cursor=garbage", "/api/v1/reading?limit=49"} {
		if w := call(h, "GET", path, "owner"); w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/api/v1/home", "/api/v1/shelf", "/api/v1/reading", "/bookshelf"} {
		if w := call(h, "GET", path, ""); w.Code != 401 {
			t.Fatal("unprotected route", path)
		}
	}
	var home homeResponse
	w := call(h, "GET", "/api/v1/home", "owner")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &home) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if home.Stats.Books != 1 || home.Stats.Readable != 1 || len(home.Featured) != 1 || home.Featured[0].XiancaoCount != 1 || len(home.HighRated) != 1 || len(home.Latest) != 1 || len(home.Reading) != 0 {
		t.Fatal("home counts or approval filters", home)
	}
	if w := call(h, "GET", "/bookshelf?view=reading", "owner"); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatal("shelf deep link did not serve app")
	}
}
