// Package httpapi serves bounded reading responses for either storage backend.
package httpapi

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
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EthanHannn/personal-library/internal/catalog"
	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/reader"
)

type App struct {
	store    Repository
	reader   *reader.Reader
	key      [32]byte
	requests chan struct{}
}

type Repository interface {
	Ping(context.Context) error
	Book(context.Context, int64) (catalog.Book, error)
	Chapter(context.Context, int64, int64) (catalog.Book, catalog.Chapter, reader.File, error)
	Chapters(context.Context, int64, int64, int) ([]catalog.Chapter, error)
}

func New(s Repository, r *reader.Reader) (*App, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return NewWithKey(s, r, key)
}

func NewWithKey(s Repository, r *reader.Reader, key []byte) (*App, error) {
	if len(key) != 32 {
		return nil, errors.New("reader signing key must be 32 bytes")
	}
	a := &App{store: s, reader: r, requests: make(chan struct{}, 8)}
	copy(a.key[:], key)
	return a, nil
}

var ErrCursor = errors.New("invalid block position")
var ErrPosition = errors.New("invalid paragraph position")

type Content struct {
	Book           catalog.Book    `json:"book"`
	Chapter        catalog.Chapter `json:"chapter"`
	Text           string          `json:"text"`
	ContentVersion string          `json:"contentVersion"`
	NextCursor     *string         `json:"nextCursor"`
	BlockToken     string          `json:"blockToken"`
	BlockStart     int64           `json:"blockStart"`
	BlockEnd       int64           `json:"blockEnd"`
	ChapterStart   int64           `json:"chapterStart"`
	ChapterEnd     int64           `json:"chapterEnd"`
}

func (a *App) Handler(config gateway.Config, mux *http.ServeMux, mode string) http.Handler {
	mux.HandleFunc("GET /api/v1/books/{id}/chapters/{idx}/content", a.content)
	mux.HandleFunc("GET /api/v1/books/{id}/chapters", a.chapters)
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
			writeJSON(w, map[string]string{"status": "ok", "mode": mode})
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
	block, err := a.Content(r.Context(), id, idx, r.URL.Query().Get("cursor"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, block)
}

func (a *App) Content(ctx context.Context, id, idx int64, token string) (*Content, error) {
	book, chapter, f, err := a.store.Chapter(ctx, id, idx)
	if err != nil {
		return nil, err
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
	if token != "" {
		offset, err = a.verify(token, c)
		if err != nil {
			return nil, ErrCursor
		}
	}
	block, err := a.reader.Read(ctx, f, chapter.Start, chapter.End, offset, reader.MaxBlockBytes)
	if err != nil {
		return nil, err
	}
	c.Offset = offset
	blockToken := a.sign(c)
	var next *string
	if block.Next != nil {
		c.Offset = *block.Next
		token := a.sign(c)
		next = &token
	}
	return &Content{book, chapter, block.Text, version, next, blockToken, block.Start, block.End, chapter.Start, chapter.End}, nil
}

func (a *App) ValidatePosition(ctx context.Context, id int64, p catalog.Position) error {
	if p.Anchor == nil || p.ContentVersion == "" {
		return ErrPosition
	}
	block, err := a.Content(ctx, id, p.ChapterIdx, p.Anchor.Block)
	if err != nil {
		return err
	}
	if block.ContentVersion != p.ContentVersion {
		return ErrCursor
	}
	for _, paragraph := range reader.Paragraphs(block.Text) {
		if paragraph.Start == p.Anchor.Paragraph && p.Anchor.Character >= 0 && p.Anchor.Character <= utf8.RuneCountInString(paragraph.Text) {
			return nil
		}
	}
	return ErrPosition
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
	case errors.Is(err, ErrCursor):
		gateway.Error(w, 400, "invalid_cursor", "正文位置已失效，请重新打开章节")
	case errors.Is(err, ErrPosition):
		gateway.Error(w, 400, "invalid_position", "阅读位置无效")
	case errors.Is(err, catalog.ErrNotFound):
		gateway.Error(w, 404, "not_found", "作品或章节不存在")
	case errors.Is(err, catalog.ErrUnindexed):
		gateway.Error(w, 409, "unindexed_file", "正文编码和文件版本尚未验证")
	case errors.Is(err, reader.ErrChanged):
		gateway.Error(w, 409, "source_changed", "正文文件已变化，需要重新索引")
	case errors.Is(err, reader.ErrPath):
		gateway.Error(w, 409, "source_unavailable", "正文文件不可用")
	case errors.Is(err, reader.ErrEncoding):
		gateway.Error(w, 422, "invalid_encoding", "正文编码异常，需要重新检查")
	case errors.Is(err, reader.ErrRange), errors.Is(err, catalog.ErrAmbiguous):
		gateway.Error(w, 409, "invalid_index", "章节索引异常，需要重新检查")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		gateway.Error(w, 503, "request_cancelled", "请求已取消或超时")
	default:
		gateway.Error(w, 503, "unavailable", "暂时无法读取，请重试")
	}
}

func RespondError(w http.ResponseWriter, err error) { fail(w, err) }

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(data)
}
