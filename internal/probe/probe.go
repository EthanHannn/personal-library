// Package probe is the read-only M0 HTTP application. It is not a production
// migration, a complete reader, or a substitute for the portal gateway.
package probe

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/legacy"
	"github.com/EthanHannn/personal-library/internal/reader"
)

type Manifest struct {
	Version int           `json:"version"`
	Files   []reader.File `json:"files"`
}

type App struct {
	store    *legacy.Store
	reader   *reader.Reader
	files    map[string]reader.File
	key      [32]byte
	requests chan struct{}
}

func LoadManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8*1024*1024+1))
	d.DisallowUnknownFields()
	var manifest Manifest
	if err := d.Decode(&manifest); err != nil {
		return manifest, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return manifest, fmt.Errorf("manifest has trailing data or exceeds limit")
	}
	if manifest.Version != 1 {
		return manifest, fmt.Errorf("unsupported manifest version")
	}
	return manifest, nil
}

func New(s *legacy.Store, r *reader.Reader, m Manifest) (*App, error) {
	a := &App{store: s, reader: r, files: make(map[string]reader.File), requests: make(chan struct{}, 8)}
	if m.Version != 1 || len(m.Files) == 0 {
		return nil, fmt.Errorf("invalid file manifest")
	}
	for _, f := range m.Files {
		if f.Path == "" || f.Size < 0 || (f.Encoding != "utf-8" && f.Encoding != "gb18030") {
			return nil, fmt.Errorf("invalid file metadata")
		}
		f.Path = strings.ReplaceAll(f.Path, "\\", "/")
		if _, exists := a.files[f.Path]; exists {
			return nil, fmt.Errorf("duplicate manifest path")
		}
		a.files[f.Path] = f
	}
	if _, err := rand.Read(a.key[:]); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) Handler(config gateway.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"user": map[string]string{"id": "default-reader", "nickname": "我"}, "logoutUrl": config.AuthOrigin + "/logout", "mode": "read-only-probe"})
	})
	mux.HandleFunc("GET /api/v1/books/{id}/chapters/{idx}/content", a.content)
	mux.HandleFunc("GET /api/v1/books/{id}/chapters", a.chapters)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { gateway.Error(w, 404, "not_found", "接口不存在") })
	protected := config.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case a.requests <- struct{}{}:
			defer func() { <-a.requests }()
		default:
			w.Header().Set("Retry-After", "1")
			gateway.Error(w, 429, "busy", "正在处理其他请求，请稍后重试")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" && (r.Method == "GET" || r.Method == "HEAD") {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if err := a.store.Ping(ctx); err != nil {
				gateway.Error(w, 503, "unhealthy", "服务不可用")
				return
			}
			writeJSON(w, map[string]string{"status": "ok", "mode": "read-only-probe"})
			return
		}
		protected.ServeHTTP(w, r)
	})
}

type cursor struct {
	Book    int64  `json:"b"`
	Chapter int64  `json:"c"`
	Offset  int64  `json:"o"`
	Version string `json:"v"`
}

func (a *App) sign(c cursor) string {
	data, _ := json.Marshal(c)
	h := hmac.New(sha256.New, a.key[:])
	h.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (a *App) verify(token string, expected cursor) (int64, error) {
	if len(token) > 1024 {
		return 0, errors.New("invalid cursor")
	}
	payload, sig, ok := strings.Cut(token, ".")
	if !ok {
		return 0, errors.New("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return 0, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return 0, err
	}
	h := hmac.New(sha256.New, a.key[:])
	h.Write(data)
	if !hmac.Equal(signature, h.Sum(nil)) {
		return 0, errors.New("invalid cursor")
	}
	var c cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return 0, err
	}
	if c.Book != expected.Book || c.Chapter != expected.Chapter || c.Version != expected.Version {
		return 0, errors.New("cursor is from another chapter or file version")
	}
	return c.Offset, nil
}

func (a *App) content(w http.ResponseWriter, r *http.Request) {
	id, err := positive(r.PathValue("id"))
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return
	}
	idx, err := positive(r.PathValue("idx"))
	if err != nil {
		gateway.Error(w, 400, "invalid_chapter", "章节编号无效")
		return
	}
	book, chapter, err := a.store.Chapter(r.Context(), id, idx)
	if err != nil {
		fail(w, err)
		return
	}
	f, ok := a.files[strings.ReplaceAll(book.FilePath, "\\", "/")]
	if !ok {
		gateway.Error(w, 409, "unindexed_file", "正文编码和文件版本尚未验证")
		return
	}
	data, _ := json.Marshal(struct {
		Decoder    string
		File       reader.File
		Start, End int64
	}{reader.DecoderVersion, f, chapter.Start, chapter.End})
	hash := sha256.Sum256(data)
	version := hex.EncodeToString(hash[:])
	c := cursor{Book: id, Chapter: idx, Version: version}
	offset := chapter.Start
	if token := r.URL.Query().Get("cursor"); token != "" {
		offset, err = a.verify(token, c)
		if err != nil {
			gateway.Error(w, 400, "invalid_cursor", "正文位置已失效，请重新打开章节")
			return
		}
	}
	block, err := a.reader.Read(r.Context(), f, chapter.Start, chapter.End, offset, reader.MaxBlockBytes)
	if err != nil {
		fail(w, err)
		return
	}
	var next *string
	if block.Next != nil {
		c.Offset = *block.Next
		token := a.sign(c)
		next = &token
	}
	writeJSON(w, map[string]any{"book": book, "chapter": chapter, "text": block.Text, "contentVersion": version, "nextCursor": next})
}

func (a *App) chapters(w http.ResponseWriter, r *http.Request) {
	id, err := positive(r.PathValue("id"))
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return
	}
	after, limit := int64(0), int64(100)
	if raw := r.URL.Query().Get("after"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			gateway.Error(w, 400, "invalid_window", "目录起点无效")
			return
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = positive(raw)
		if err != nil || limit > 200 {
			gateway.Error(w, 400, "invalid_window", "目录每次最多加载 200 章")
			return
		}
	}
	b, err := a.store.Book(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	chapters, err := a.store.Chapters(r.Context(), id, after, int(limit))
	if err != nil {
		fail(w, err)
		return
	}
	var next *int64
	if len(chapters) == int(limit) {
		last := chapters[len(chapters)-1].Index
		next = &last
	}
	writeJSON(w, map[string]any{"book": b, "chapters": chapters, "nextAfter": next})
}

func positive(raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("expected a positive integer")
	}
	return n, nil
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, legacy.ErrNotFound):
		gateway.Error(w, 404, "not_found", "作品或章节不存在")
	case errors.Is(err, reader.ErrChanged):
		gateway.Error(w, 409, "source_changed", "正文文件已变化，需要重新索引")
	case errors.Is(err, reader.ErrPath):
		gateway.Error(w, 409, "source_unavailable", "正文文件不可用")
	case errors.Is(err, reader.ErrEncoding):
		gateway.Error(w, 422, "invalid_encoding", "正文编码异常，需要重新检查")
	case errors.Is(err, reader.ErrRange), errors.Is(err, legacy.ErrAmbiguous):
		gateway.Error(w, 409, "invalid_index", "章节索引异常，需要重新检查")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		gateway.Error(w, 503, "request_cancelled", "请求已取消或超时")
	default:
		gateway.Error(w, 503, "unavailable", "暂时无法读取，请重试")
	}
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(data)
}
