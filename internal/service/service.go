// Package service wires the business database, trusted gateway, and static app.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/httpapi"
	"github.com/EthanHannn/personal-library/internal/management"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type Book struct {
	ID           int64   `json:"id"`
	CoverURL     string  `json:"coverUrl,omitempty"`
	Title        string  `json:"title"`
	Author       string  `json:"author"`
	Tag1         string  `json:"tag1"`
	Tag2         string  `json:"tag2"`
	WordCount    int64   `json:"wordCount"`
	ChapterCount int64   `json:"chapterCount"`
	HasContent   bool    `json:"hasContent"`
	Score        float64 `json:"score"`
	Intro        string  `json:"intro,omitempty"`
}

func coverURL(id, has int64) string {
	if has == 0 {
		return ""
	}
	return "/api/v1/books/" + strconv.FormatInt(id, 10) + "/cover"
}

func New(ctx context.Context, db *storage.Database, r *reader.Reader, config gateway.Config, assets fs.FS, managers ...*management.Manager) (http.Handler, error) {
	if _, err := gateway.Parse(config.User, config.Origin, config.AuthOrigin); err != nil {
		return nil, err
	}
	if err := db.Ready(ctx, config.User); err != nil {
		return nil, err
	}
	static, err := newStatic(assets)
	if err != nil {
		return nil, err
	}
	store := storage.NewRepository(db)
	key, err := store.Queries.ReaderKey(ctx)
	if err != nil {
		return nil, err
	}
	user, err := store.Queries.CurrentReader(ctx, sql.NullString{String: config.User, Valid: true})
	if err != nil {
		return nil, err
	}
	api, err := httpapi.NewWithKey(store, r, key)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	manage := manageAPI{store: store}
	if len(managers) > 0 {
		manage.jobs = managers[0]
	}
	manage.register(mux)
	reading := readingAPI{store: store, content: api, readerID: user.ID}
	collections := collectionAPI{store: store, readerID: user.ID}
	catalog := catalogAPI{store: store}
	votes := voteAPI{store: store, readerID: user.ID}
	profile := profileAPI{store: store, readerID: user.ID, portalUser: config.User, accountURL: config.AuthOrigin}
	mux.HandleFunc("GET /api/v1/profile", profile.profile)
	mux.HandleFunc("GET /api/v1/votes", profile.votes)
	mux.HandleFunc("GET /api/v1/books/{id}/recommendations", profile.related)
	mux.HandleFunc("GET /api/v1/books/{id}/vote", votes.vote)
	mux.HandleFunc("PUT /api/v1/books/{id}/vote", votes.vote)
	mux.HandleFunc("GET /api/v1/books", catalog.books)
	mux.HandleFunc("GET /api/v1/categories", catalog.categories)
	mux.HandleFunc("GET /api/v1/home", collections.home)
	mux.HandleFunc("GET /api/v1/shelf", collections.list("shelf"))
	mux.HandleFunc("GET /api/v1/reading", collections.list("reading"))
	mux.HandleFunc("PUT /api/v1/books/{id}/shelf", collections.setShelf)
	mux.HandleFunc("DELETE /api/v1/books/{id}/shelf", collections.setShelf)
	mux.HandleFunc("GET /api/v1/books/{id}/reader", reading.initialize)
	mux.HandleFunc("GET /api/v1/books/{id}/directory", reading.directory)
	mux.HandleFunc("GET /api/v1/books/{id}/progress", reading.progress)
	mux.HandleFunc("PUT /api/v1/books/{id}/progress", reading.save)
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		user, err := store.Queries.CurrentReader(r.Context(), sql.NullString{String: config.User, Valid: true})
		if err != nil {
			apiError(w, err)
			return
		}
		nickname := user.Nickname.String
		if nickname == "" {
			nickname = user.Username
		}
		jsonResponse(w, map[string]any{"user": map[string]string{"id": user.ID, "nickname": nickname}, "logoutUrl": config.AuthOrigin + "/logout"})
	})
	mux.HandleFunc("GET /api/v1/books/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := integer(r.PathValue("id"), 0, 1, 1<<53-1)
		if err != nil {
			gateway.Error(w, 400, "invalid_book", "作品编号无效")
			return
		}
		b, err := store.Queries.BookDetail(r.Context(), id)
		if err != nil {
			apiError(w, err)
			return
		}
		inShelf, err := store.Queries.InShelf(r.Context(), dbgen.InShelfParams{ReaderID: user.ID, BookID: id})
		if err != nil {
			apiError(w, err)
			return
		}
		progress, err := store.Progress(r.Context(), user.ID, id)
		if err != nil {
			apiError(w, err)
			return
		}
		vote, err := store.VoteSummary(r.Context(), user.ID, id)
		if err != nil {
			apiError(w, err)
			return
		}
		var complete int
		if err := db.Read.QueryRowContext(r.Context(), `SELECT coalesce((SELECT complete FROM book_sources WHERE book_id=?),1)`, id).Scan(&complete); err != nil {
			apiError(w, err)
			return
		}
		jsonResponse(w, map[string]any{"book": Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score, Intro: b.Intro}, "inShelf": inShelf != 0, "progress": progress, "vote": vote, "partialContent": complete == 0})
	})
	mux.Handle("/", static)
	return api.Handler(config, mux, "library"), nil
}

func integer(raw string, fallback, min, max int64) (int64, error) {
	if raw == "" {
		raw = strconv.FormatInt(fallback, 10)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < min || n > max {
		return 0, errors.New("invalid integer")
	}
	return n, nil
}

func apiError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		gateway.Error(w, 404, "not_found", "作品不存在")
		return
	}
	gateway.Error(w, 503, "unavailable", "暂时无法读取，请重试")
}

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}
