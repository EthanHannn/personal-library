package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EthanHannn/personal-library/internal/management"
	"github.com/EthanHannn/personal-library/internal/storage"
)

func TestSourceAPIAuthVersionAndUnsupportedRules(t *testing.T) {
	_, db := serviceFixture(t)
	mux := http.NewServeMux()
	manageAPI{store: storage.NewRepository(db), jobs: &management.Manager{DB: db}}.register(mux)
	// The production service wraps these routes in the same gateway middleware.
	h, _ := serviceFixture(t)
	for user, want := range map[string]int{"": 401, "other": 403} {
		if got := call(h, "GET", "/api/v1/manage/sources", user); got.Code != want {
			t.Fatal(got.Code)
		}
	}
	if got := shelfRequest(h, "POST", "/api/v1/manage/sources", `{}`, "owner", "https://other.test"); got.Code != 403 {
		t.Fatal(got.Code)
	}
	config := management.SourceTemplates()[0].Config
	body, _ := json.Marshal(map[string]any{"config": config, "enabled": true})
	request := func(method, path string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	got := request("POST", "/api/v1/manage/sources", body)
	if got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	var source management.BookSource
	json.Unmarshal(got.Body.Bytes(), &source)
	body, _ = json.Marshal(map[string]any{"config": config, "enabled": false, "expectedVersion": source.Version})
	got = request("PUT", "/api/v1/manage/sources/"+source.ID, body)
	if got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	if got = request("PUT", "/api/v1/manage/sources/"+source.ID, body); got.Code != 409 {
		t.Fatal("stale", got.Code)
	}
	bad := strings.Replace(string(body), `"kind":"html"`, `"kind":"html","js":"fetch()"`, 1)
	if got = request("POST", "/api/v1/manage/sources", []byte(bad)); got.Code != 400 {
		t.Fatal("executable unknown field accepted", got.Code)
	}
	got = request("GET", "/api/v1/manage/sources", nil)
	if got.Code != 200 || !strings.Contains(got.Body.String(), source.ID) || !strings.Contains(got.Body.String(), "templates") {
		t.Fatal(got.Body.String())
	}
	// Built-in rules can be disabled but cannot be replaced by a custom adapter.
	manager := &management.Manager{DB: db}
	builtin, _ := manager.Source(context.Background(), "qidian")
	builtin.Config.BaseURL = "https://example.com"
	if _, err := manager.SaveSource(context.Background(), "qidian", 1, false, builtin.Config); err == nil {
		t.Fatal("built-in replaced")
	}
}
