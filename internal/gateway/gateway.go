// Package gateway enforces the trusted nginx + Authelia identity contract.
package gateway

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type Config struct{ User, Origin, AuthOrigin string }

var validUsername = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func Parse(user, publicURL, authURL string) (Config, error) {
	if !validUsername.MatchString(user) {
		return Config{}, fmt.Errorf("PORTAL_ALLOWED_USER is missing or invalid")
	}
	origin, err := originURL(publicURL)
	if err != nil {
		return Config{}, fmt.Errorf("LIBRARY_PUBLIC_URL: %w", err)
	}
	auth, err := originURL(authURL)
	if err != nil {
		return Config{}, fmt.Errorf("PORTAL_AUTH_URL: %w", err)
	}
	return Config{User: user, Origin: origin, AuthOrigin: auth}, nil
}

func originURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", fmt.Errorf("must be a plain HTTPS origin")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

func Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// Wrap assumes only the trusted gateway can reach the backend. It deliberately
// does not interpret cookies, bearer tokens, or alternate forwarded identities.
func (c Config) Wrap(next http.Handler) http.Handler {
	_, configErr := Parse(c.User, c.Origin, c.AuthOrigin)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if configErr != nil {
			Error(w, 503, "gateway_unconfigured", "认证配置不完整")
			return
		}
		users := r.Header.Values("Remote-User")
		if len(users) == 0 || (len(users) == 1 && users[0] == "") {
			Error(w, 401, "unauthenticated", "请重新登录")
			return
		}
		if len(users) != 1 || users[0] != c.User {
			Error(w, 403, "forbidden", "无权访问")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origins := r.Header.Values("Origin"); len(origins) != 1 || origins[0] != c.Origin {
				Error(w, 403, "invalid_origin", "请求来源不受信任")
				return
			}
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				Error(w, 415, "invalid_media_type", "仅接受 JSON 请求")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		next.ServeHTTP(w, r)
	})
}
