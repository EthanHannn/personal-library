package service

import (
	"bytes"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/EthanHannn/personal-library/internal/gateway"
)

func newStatic(assets fs.FS) (http.Handler, error) {
	if assets == nil {
		return nil, fmt.Errorf("built frontend assets are required")
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("frontend index: %w", err)
	}
	if len(index) == 0 {
		return nil, fmt.Errorf("frontend index is empty")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			gateway.Error(w, 405, "method_not_allowed", "请求方法不支持")
			return
		}
		url := r.URL.Path
		if url == "/login" || url == "/register" || url == "/auth/signin" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		// A closed route list prevents missing API, cover, and asset requests
		// from succeeding with index.html. All of this sits inside gateway.Wrap.
		if url == "/" || url == "/library" || url == "/bookshelf" || url == "/profile" || url == "/manage" || pageID(url, "/book/") || pageID(url, "/read/") || catalogPage(r.URL.EscapedPath()) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
			w.Header().Set("Referrer-Policy", "same-origin")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		}
		if strings.HasPrefix(url, "/assets/") && fs.ValidPath(strings.TrimPrefix(url, "/")) {
			name := strings.TrimPrefix(url, "/")
			info, err := fs.Stat(assets, name)
			if err == nil && info.Mode().IsRegular() && info.Size() <= 8*1024*1024 {
				data, err := fs.ReadFile(assets, name)
				if err == nil {
					media := mime.TypeByExtension(path.Ext(name))
					if media == "" {
						media = "application/octet-stream"
					}
					w.Header().Set("Content-Type", media)
					http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(data))
					return
				}
			}
		}
		gateway.Error(w, 404, "not_found", "页面或接口不存在")
	}), nil
}

func pageID(url, prefix string) bool {
	if !strings.HasPrefix(url, prefix) {
		return false
	}
	_, err := integer(strings.TrimPrefix(url, prefix), 0, 1, 1<<53-1)
	return err == nil
}

func catalogPage(escapedPath string) bool {
	for _, prefix := range []string{"/category/", "/author/"} {
		if !strings.HasPrefix(escapedPath, prefix) {
			continue
		}
		segment := strings.TrimPrefix(escapedPath, prefix)
		if segment == "" || strings.Contains(segment, "/") {
			return false
		}
		name, err := url.PathUnescape(segment)
		return err == nil && strings.TrimSpace(name) != "" && name != "." && name != ".." && validCatalogText(name, 200)
	}
	return false
}
