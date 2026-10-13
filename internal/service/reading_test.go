package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/EthanHannn/personal-library/internal/catalog"
	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
)

func mutate(h http.Handler, m catalog.ProgressMutation) *httptest.ResponseRecorder {
	body, _ := json.Marshal(m)
	r := httptest.NewRequest("PUT", "/api/v1/books/42/progress", bytes.NewReader(body))
	r.Header.Set("Remote-User", "owner")
	r.Header.Set("Origin", "https://library.test")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func initial(t *testing.T, h http.Handler, url string) readingState {
	t.Helper()
	w := call(h, "GET", url, "owner")
	var state readingState
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return state
}

func TestProgressReplayConflictAndRestart(t *testing.T) {
	h, db := serviceFixture(t)
	s := initial(t, h, "/api/v1/books/42/reader")
	if s.Progress != nil || s.Content == nil || s.InShelf {
		t.Fatal(s)
	}
	p := catalog.Position{ChapterIdx: 1, Percent: 0.6, ContentVersion: s.Content.ContentVersion, Anchor: &catalog.Anchor{Block: s.Content.BlockToken, Paragraph: 4, Character: 5}}
	m := catalog.ProgressMutation{Position: p, BaseRevision: 0, MutationID: "mutation-0001", DeviceID: "device-0001", ClientSeq: 1}
	first := mutate(h, m)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	for range 2 {
		w := mutate(h, m)
		if w.Code != 200 || w.Body.String() != first.Body.String() {
			t.Fatal("replay", w.Code, w.Body.String())
		}
	}
	m.MutationID = "mutation-0002"
	m.ClientSeq = 2
	if w := mutate(h, m); w.Code != 409 {
		t.Fatal("lost update", w.Code, w.Body.String())
	}
	m.BaseRevision = 1
	m.Percent = 0.1 // Going backwards is a valid deliberate reread.
	if w := mutate(h, m); w.Code != 200 {
		t.Fatal("reread", w.Code, w.Body.String())
	}
	m.MutationID = "mutation-0003"
	m.BaseRevision = 2
	m.ClientSeq = 1
	if w := mutate(h, m); w.Code != 409 {
		t.Fatal("stale sequence accepted", w.Code)
	}
	m.MutationID = "mutation-0002"
	m.ClientSeq = 2
	if w := mutate(h, m); w.Code != 409 {
		t.Fatal("id reused with different body", w.Code)
	}
	old := catalog.ProgressMutation{Position: p, BaseRevision: 0, MutationID: "mutation-0001", DeviceID: "device-0001", ClientSeq: 1}
	if w := mutate(h, old); w.Code != 200 || w.Body.String() != first.Body.String() {
		t.Fatal("old replay did not return original receipt", w.Code, w.Body.String())
	}
	var path string
	if err := db.Read.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	r, err := reader.Open(filepath.Dir(path), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	config, _ := gateway.Parse("owner", "https://library.test", "https://auth.test")
	restarted, err := New(context.Background(), db, r, config, fstest.MapFS{"index.html": {Data: []byte("<title>reader</title>")}})
	if err != nil {
		t.Fatal(err)
	}
	s = initial(t, restarted, "/api/v1/books/42/reader")
	if s.RecoveryRequired || s.Progress.Revision != 2 || s.Progress.Percent != 0.1 || s.Content.BlockToken != p.Anchor.Block {
		t.Fatal("restart did not restore", s)
	}
	// File version changes preserve old progress and require a user decision.
	if _, err := db.Write.Exec(`UPDATE files SET sha256=?`, fmt.Sprintf("%064d", 7)); err != nil {
		t.Fatal(err)
	}
	s = initial(t, restarted, "/api/v1/books/42/reader")
	if !s.RecoveryRequired || s.Progress.Revision != 2 {
		t.Fatal("old anchor silently reset", s)
	}
	s = initial(t, restarted, "/api/v1/books/42/reader?chapter=1")
	if s.RecoveryRequired || s.Content == nil || s.Progress.Revision != 2 {
		t.Fatal("explicit chapter lost", s)
	}
}

func TestConcurrentProgressAndValidation(t *testing.T) {
	h, db := serviceFixture(t)
	s := initial(t, h, "/api/v1/books/42/reader")
	base := catalog.ProgressMutation{Position: catalog.Position{ChapterIdx: 1, Percent: 0.5, ContentVersion: s.Content.ContentVersion, Anchor: &catalog.Anchor{Block: s.Content.BlockToken, Paragraph: 4, Character: 6}}, MutationID: "mutation-validate", DeviceID: "device-validate", ClientSeq: 1}
	// Emoji occupies one Unicode scalar, not two UTF-16 units.
	for _, character := range []int{-1, 8} {
		bad := base
		anchor := *base.Anchor
		anchor.Character = character
		bad.Anchor = &anchor
		if w := mutate(h, bad); w.Code != 400 {
			t.Fatal("invalid character accepted", w.Code, w.Body.String())
		}
	}
	bad := base
	bad.Anchor = &catalog.Anchor{Block: base.Anchor.Block + "tampered", Paragraph: 4}
	if w := mutate(h, bad); w.Code != 400 {
		t.Fatal("forged position", w.Code)
	}
	bad = base
	bad.ChapterIdx = 2
	if w := mutate(h, bad); w.Code != 404 {
		t.Fatal("missing chapter", w.Code)
	}
	var count int
	if err := db.Read.QueryRow(`SELECT count(*) FROM reading_progress`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed writes changed progress", err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := base
			m.MutationID = fmt.Sprintf("mutation-%08d", i)
			m.DeviceID = fmt.Sprintf("device-%08d", i)
			codes <- mutate(h, m).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	results := map[int]int{}
	for code := range codes {
		results[code]++
	}
	if results[200] != 1 || results[409] != 1 {
		t.Fatal("concurrent writes", results)
	}
	s = initial(t, h, "/api/v1/books/42/reader")
	if s.Progress.Revision != 1 {
		t.Fatal("revision", s.Progress)
	}
	for _, url := range []string{"/api/v1/books/42/reader?chapter=2", "/api/v1/books/42/reader?chapter=0"} {
		w := call(h, "GET", url, "owner")
		if w.Code == 200 {
			t.Fatal("invalid explicit chapter fell back", url)
		}
	}
}

func TestImportedPercentIsNotOverwrittenByInitialization(t *testing.T) {
	h, db := serviceFixture(t)
	if _, err := db.Write.Exec(`INSERT INTO reading_progress VALUES('old','default-reader',42,1,0.81,123,NULL,NULL,0)`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s := initial(t, h, "/api/v1/books/42/reader")
		if s.Progress.Percent != 0.81 || s.Progress.Anchor != nil || s.Progress.UpdatedAt != 123 || s.Progress.Revision != 0 {
			t.Fatal("read changed progress", s.Progress)
		}
	}
	if _, err := db.Write.Exec(`UPDATE reading_progress SET chapter_idx=999`); err != nil {
		t.Fatal(err)
	}
	s := initial(t, h, "/api/v1/books/42/reader")
	if !s.RecoveryRequired || s.Content != nil || s.Progress.ChapterIdx != 999 {
		t.Fatal("missing old chapter silently reset", s)
	}
}

func TestReceiptRetentionKeepsDeviceOrder(t *testing.T) {
	h, db := serviceFixture(t)
	s := initial(t, h, "/api/v1/books/42/reader")
	m := catalog.ProgressMutation{Position: catalog.Position{ChapterIdx: 1, ContentVersion: s.Content.ContentVersion, Anchor: &catalog.Anchor{Block: s.Content.BlockToken, Paragraph: 4}}, DeviceID: "device-retention"}
	for i := int64(1); i <= 130; i++ {
		m.MutationID = fmt.Sprintf("retention-%08d", i)
		m.ClientSeq = i
		m.BaseRevision = i - 1
		if w := mutate(h, m); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var count int
	if err := db.Read.QueryRow(`SELECT count(*) FROM progress_mutations`).Scan(&count); err != nil || count != 128 {
		t.Fatal("unbounded receipts", count, err)
	}
	m.MutationID = "retention-00000001"
	m.ClientSeq = 1
	m.BaseRevision = 130
	if w := mutate(h, m); w.Code != 409 {
		t.Fatal("expired receipt overrode newer progress", w.Code)
	}
}

func TestSQLiteBusyRetainsProgressAndCanRetry(t *testing.T) {
	h, db := serviceFixture(t)
	s := initial(t, h, "/api/v1/books/42/reader")
	var path string
	if err := db.Read.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	other, err := storage.Connect(context.Background(), path, "rw", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	lock, err := other.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	m := catalog.ProgressMutation{Position: catalog.Position{ChapterIdx: 1, Percent: 0.5, ContentVersion: s.Content.ContentVersion, Anchor: &catalog.Anchor{Block: s.Content.BlockToken, Paragraph: 4}}, DeviceID: "busy-device", MutationID: "busy-mutation", ClientSeq: 1}
	started := time.Now()
	if w := mutate(h, m); w.Code != 503 {
		t.Fatal("busy write", w.Code, w.Body.String())
	}
	if time.Since(started) > 4*time.Second {
		t.Fatal("busy write exceeded bounded wait")
	}
	if state := initial(t, h, "/api/v1/books/42/reader"); state.Progress != nil {
		t.Fatal("failed save changed progress")
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	if w := mutate(h, m); w.Code != 200 {
		t.Fatal("retry did not succeed", w.Code, w.Body.String())
	}
	if state := initial(t, h, "/api/v1/books/42/reader"); state.Progress.Revision != 1 {
		t.Fatal("failed attempt consumed revision")
	}
}
