package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/EthanHannn/personal-library/internal/storage"
)

func putVote(h http.Handler, bookID int, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/books/%d/vote", bookID), strings.NewReader(body))
	r.Header.Set("Remote-User", "owner")
	r.Header.Set("Origin", "https://library.test")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeVote(t *testing.T, w *httptest.ResponseRecorder) storage.VoteSummary {
	t.Helper()
	var result storage.VoteSummary
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("vote: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("private vote response is cacheable")
	}
	return result
}

func TestPersonalVoteChoicesRetriesAndDetail(t *testing.T) {
	h, db := serviceFixture(t)
	base := decodeVote(t, call(h, "GET", "/api/v1/books/42/vote", "owner"))
	if base.MyVote != nil || base.Current.Popularity != 1 || base.Current.Score != 10 {
		t.Fatalf("baseline: %+v", base)
	}
	for _, tc := range []struct {
		choice string
		counts storage.VoteCounts
		score  float64
	}{
		{"XIANCAO", storage.VoteCounts{Xiancao: 2}, 10},
		{"LIANGCAO", storage.VoteCounts{Xiancao: 1, Liangcao: 1}, 8.5},
		{"GANCAO", storage.VoteCounts{Xiancao: 1, Gancao: 1}, 7.5},
		{"KUCAO", storage.VoteCounts{Xiancao: 1, Kucao: 1}, 6.5},
		{"DUCAO", storage.VoteCounts{Xiancao: 1, Ducao: 1}, 5.5},
	} {
		for range 3 { // Lost acknowledgements and repeated clicks cannot add votes.
			got := decodeVote(t, putVote(h, 42, fmt.Sprintf(`{"type":%q}`, tc.choice)))
			if got.MyVote == nil || string(*got.MyVote) != tc.choice || got.Current.Counts != tc.counts || got.Current.Score != tc.score || got.Current.Popularity != 2 || got.Source != base.Source {
				t.Fatalf("%s: %+v", tc.choice, got)
			}
		}
	}
	var votes, shelf, progress int
	if err := db.Read.QueryRow(`SELECT (SELECT count(*) FROM votes), (SELECT count(*) FROM shelf_entries), (SELECT count(*) FROM reading_progress)`).Scan(&votes, &shelf, &progress); err != nil || votes != 1 || shelf != 0 || progress != 0 {
		t.Fatal("vote affected other personal relationships", votes, shelf, progress, err)
	}
	var detail struct {
		Vote storage.VoteSummary `json:"vote"`
	}
	w := call(h, "GET", "/api/v1/books/42", "owner")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || !reflect.DeepEqual(detail.Vote, decodeVote(t, call(h, "GET", "/api/v1/books/42/vote", "owner"))) {
		t.Fatal("detail omitted saved vote", w.Body.String())
	}
}

func TestImportedVoteUsesDeltaAndPreservesHistoricalRecords(t *testing.T) {
	h, db := serviceFixture(t)
	_, err := db.Write.Exec(`
INSERT INTO readers VALUES('old-reader','old-reader',NULL,'USER',NULL,0,0);
UPDATE rating_snapshots SET popularity=19,total_score=149,score=7.84,xiancao_count=10,liangcao_count=5,gancao_count=2,kucao_count=1,ducao_count=1 WHERE book_id=42;
INSERT INTO votes VALUES('original-owner','XIANCAO','XIANCAO','default-reader',42,123),('other-vote','LIANGCAO','LIANGCAO','old-reader',42,456);
INSERT INTO comments VALUES('old-comment','历史记录','old-reader',42,NULL,0,0,123,123);`)
	if err != nil {
		t.Fatal(err)
	}
	base := decodeVote(t, call(h, "GET", "/api/v1/books/42/vote", "owner"))
	if base.MyVote == nil || *base.MyVote != "XIANCAO" || base.Current.Popularity != 19 || base.Current.Counts != base.Source.Counts {
		t.Fatalf("import doubled: %+v", base)
	}
	for range 3 {
		got := decodeVote(t, putVote(h, 42, `{"type":"DUCAO"}`))
		if got.Source != base.Source || got.Current.Popularity != 19 || got.Current.TotalScore != 140 || got.Current.Counts != (storage.VoteCounts{Xiancao: 9, Liangcao: 5, Gancao: 2, Kucao: 1, Ducao: 2}) {
			t.Fatalf("changed imported vote: %+v", got)
		}
	}
	restored := decodeVote(t, putVote(h, 42, `{"type":"XIANCAO"}`))
	if !reflect.DeepEqual(restored, base) {
		t.Fatalf("return to original drifted: %+v", restored)
	}
	var kept int
	if err := db.Read.QueryRow(`SELECT
 (SELECT count(*) FROM votes WHERE id='original-owner' AND imported_type='XIANCAO' AND created_at=123) +
 (SELECT count(*) FROM votes WHERE id='other-vote' AND type='LIANGCAO' AND imported_type='LIANGCAO' AND created_at=456) +
 (SELECT count(*) FROM comments WHERE id='old-comment' AND content='历史记录')`).Scan(&kept); err != nil || kept != 3 {
		t.Fatal("historical data changed", kept, err)
	}
	// Discovery keeps the source rating, independently of personal choices.
	if !strings.Contains(call(h, "GET", "/api/v1/books/42", "owner").Body.String(), `"score":7.84`) {
		t.Fatal("source score was rewritten")
	}
}

func TestVotingDoesNotRequireTextOrAScore(t *testing.T) {
	h, db := serviceFixture(t)
	if _, err := db.Write.Exec(`UPDATE books SET status='APPROVED' WHERE id=88`); err != nil {
		t.Fatal(err)
	}
	base := decodeVote(t, call(h, "GET", "/api/v1/books/88/vote", "owner"))
	if base.Current.Popularity != 0 || base.Current.Score != 0 || base.MyVote != nil {
		t.Fatal(base)
	}
	got := decodeVote(t, putVote(h, 88, `{"type":"GANCAO"}`))
	if got.Current.Popularity != 1 || got.Current.Score != 5 || got.Source.Score != 0 {
		t.Fatal(got)
	}
}

func TestVoteValidationAndGatewayBoundaries(t *testing.T) {
	h, db := serviceFixture(t)
	for _, body := range []string{``, `null`, `{}`, `[]`, `{"type":null}`, `{"type":1}`, `{"type":"xiancao"}`, `{"type":"OTHER"}`, `{"type":"XIANCAO","readerId":"other"}`, `{"type":"XIANCAO"} {}`, `{"type":"` + strings.Repeat("X", 1100) + `"}`} {
		if w := putVote(h, 42, body); w.Code != 400 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct{ id, code int }{{88, 404}, {999, 404}, {0, 400}, {-1, 400}} {
		if w := putVote(h, tc.id, `{"type":"XIANCAO"}`); w.Code != tc.code {
			t.Fatal(tc, w.Code)
		}
	}
	for _, tc := range []struct {
		method, path, user, origin, media string
		code                              int
	}{
		{"GET", "/api/v1/books/42/vote", "", "", "", 401},
		{"GET", "/api/v1/books/42/vote", "stranger", "", "", 403},
		{"GET", "/api/v1/books/88/vote", "owner", "", "", 404},
		{"GET", "/api/v1/books/42/vote?reader=other", "owner", "", "", 400},
		{"PUT", "/api/v1/books/42/vote", "", "https://library.test", "application/json", 401},
		{"PUT", "/api/v1/books/42/vote", "stranger", "https://library.test", "application/json", 403},
		{"PUT", "/api/v1/books/42/vote", "owner", "https://evil.test", "application/json", 403},
		{"PUT", "/api/v1/books/42/vote", "owner", "https://library.test", "text/plain", 415},
		{"POST", "/api/v1/books/42/vote", "owner", "https://library.test", "application/json", 405},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"type":"XIANCAO"}`))
		r.Header.Set("Remote-User", tc.user)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.media)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	var count int
	if err := db.Read.QueryRow(`SELECT count(*) FROM votes`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected request wrote a vote", count, err)
	}
}

func TestVoteFailureRollsBack(t *testing.T) {
	h, db := serviceFixture(t)
	if _, err := db.Write.Exec(`INSERT INTO votes VALUES('broken-baseline','LIANGCAO','LIANGCAO','default-reader',42,123)`); err != nil {
		t.Fatal(err)
	}
	// No historical liangcao exists to subtract. Refuse to save a negative tally.
	if w := putVote(h, 42, `{"type":"DUCAO"}`); w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	var choice string
	if err := db.Read.QueryRow(`SELECT type FROM votes WHERE id='broken-baseline'`).Scan(&choice); err != nil || choice != "LIANGCAO" {
		t.Fatal("failed transaction changed selection", choice, err)
	}
}

func TestConcurrentVotesRemainOneChoice(t *testing.T) {
	h, db := serviceFixture(t)
	var wg sync.WaitGroup
	failures := make(chan string, 30)
	// Stay within the service's eight-request admission limit. Exercise thirty
	// writes with five callers rather than testing the independent 429 guard.
	callers := make(chan struct{}, 5)
	for i := range 30 {
		wg.Go(func() {
			callers <- struct{}{}
			defer func() { <-callers }()
			choice := []string{"XIANCAO", "DUCAO", "GANCAO"}[i%3]
			w := putVote(h, 42, fmt.Sprintf(`{"type":%q}`, choice))
			var got storage.VoteSummary
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.MyVote == nil || string(*got.MyVote) != choice || got.Current.Popularity != 2 {
				failures <- fmt.Sprintf("%s: %d %s", choice, w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	var count int
	if err := db.Read.QueryRow(`SELECT count(*) FROM votes`).Scan(&count); err != nil || count != 1 {
		t.Fatal("concurrent votes duplicated", count, err)
	}
	got := decodeVote(t, call(h, "GET", "/api/v1/books/42/vote", "owner"))
	if got.Current.Popularity != 2 || got.Source.Popularity != 1 {
		t.Fatal(got)
	}
}
