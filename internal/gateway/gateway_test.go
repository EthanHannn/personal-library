package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayContract(t *testing.T) {
	c, err := Parse("owner", "https://library.example.test", "https://auth.example.test")
	if err != nil {
		t.Fatal(err)
	}
	h := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		name, user, method, origin, media string
		status                            int
	}{
		{"anonymous", "", "GET", "", "", 401},
		{"wrong user", "stranger", "GET", "", "", 403},
		{"trusted", "owner", "GET", "", "", 204},
		{"missing origin", "owner", "POST", "", "application/json", 403},
		{"sibling subdomain", "owner", "PUT", "https://blog.example.test", "application/json", 403},
		{"bad media", "owner", "POST", c.Origin, "text/plain", 415},
		{"missing media", "owner", "DELETE", c.Origin, "", 415},
		{"write", "owner", "PUT", c.Origin, "application/json; charset=utf-8", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/v1/me", nil)
			if tc.user != "" {
				r.Header.Set("Remote-User", tc.user)
			}
			r.Header.Set("X-Forwarded-User", "owner")
			r.Header.Set("Cookie", "retired-session=owner")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.media)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("Remote-User", "owner")
	r.Header.Add("Remote-User", "owner")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	(Config{}).Wrap(h).ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestConfigRejectsInvalidOrigins(t *testing.T) {
	for _, origin := range []string{"", "http://library.test", "https://user:pass@library.test", "https://library.test/path", "https://library.test/?x=y", "https://library.test/#x", "https://library.test/?"} {
		if _, err := Parse("owner", origin, "https://auth.test"); err == nil {
			t.Fatalf("accepted %q", origin)
		}
	}
}
