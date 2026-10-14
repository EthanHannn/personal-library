package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/storage"
	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type collectionAPI struct {
	store    *storage.Repository
	readerID string
}

type progressSummary struct {
	ChapterIdx int64   `json:"chapterIdx"`
	Percent    float64 `json:"percent"`
	UpdatedAt  int64   `json:"updatedAt"`
}

type collectionEntry struct {
	Book     Book             `json:"book"`
	InShelf  bool             `json:"inShelf"`
	AddedAt  *int64           `json:"addedAt"`
	Progress *progressSummary `json:"progress"`
}

type collectionCounts struct {
	Shelf   int64 `json:"shelf"`
	Reading int64 `json:"reading"`
}

type collectionPage struct {
	Entries    []collectionEntry `json:"entries"`
	NextCursor *string           `json:"nextCursor"`
	Counts     collectionCounts  `json:"counts"`
}

type collectionCursor struct {
	Kind string `json:"kind"`
	At   int64  `json:"at"`
	ID   string `json:"id"`
}

func parseCollectionCursor(raw, kind string) (collectionCursor, error) {
	c := collectionCursor{Kind: kind}
	if raw == "" {
		return c, nil
	}
	if len(raw) > 2048 {
		return c, errors.New("cursor too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Kind != kind || c.ID == "" || len(c.ID) > 512 || c.At < 0 || c.At > 1<<53-1 {
		return c, errors.New("invalid cursor")
	}
	return c, nil
}

func nextCollectionCursor(kind string, at int64, id string) *string {
	b, _ := json.Marshal(collectionCursor{Kind: kind, At: at, ID: id})
	value := base64.RawURLEncoding.EncodeToString(b)
	return &value
}

func (a collectionAPI) page(ctx context.Context, kind string, cursor collectionCursor, limit int64) (collectionPage, error) {
	page := collectionPage{Entries: make([]collectionEntry, 0)}
	counts, err := a.store.Queries.CollectionCounts(ctx, a.readerID)
	if err != nil {
		return page, err
	}
	page.Counts = collectionCounts{Shelf: counts.Shelf, Reading: counts.Reading}
	hasCursor := 0
	if cursor.ID != "" {
		hasCursor = 1
	}
	if kind == "shelf" {
		rows, err := a.store.Queries.ShelfWindow(ctx, dbgen.ShelfWindowParams{ReaderID: a.readerID, HasCursor: hasCursor, AfterTime: cursor.At, AfterID: cursor.ID, PageLimit: limit + 1})
		if err != nil {
			return page, err
		}
		if int64(len(rows)) > limit {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			page.NextCursor = nextCollectionCursor(kind, last.AddedAt, last.EntryID)
		}
		for _, b := range rows {
			entry := collectionEntry{Book: Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score}, InShelf: true, AddedAt: &b.AddedAt}
			if b.ProgressChapter > 0 {
				entry.Progress = &progressSummary{ChapterIdx: b.ProgressChapter, Percent: b.ProgressPercent, UpdatedAt: b.ProgressAt}
			}
			page.Entries = append(page.Entries, entry)
		}
	} else {
		rows, err := a.store.Queries.ReadingWindow(ctx, dbgen.ReadingWindowParams{ReaderID: a.readerID, HasCursor: hasCursor, AfterTime: cursor.At, AfterID: cursor.ID, PageLimit: limit + 1})
		if err != nil {
			return page, err
		}
		if int64(len(rows)) > limit {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			page.NextCursor = nextCollectionCursor(kind, last.UpdatedAt, last.EntryID)
		}
		for _, b := range rows {
			page.Entries = append(page.Entries, collectionEntry{
				Book:    Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score},
				InShelf: b.InShelf != 0, Progress: &progressSummary{ChapterIdx: b.ChapterIdx, Percent: b.Percent, UpdatedAt: b.UpdatedAt},
			})
		}
	}
	return page, nil
}

func (a collectionAPI) list(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cursor, err := parseCollectionCursor(r.URL.Query().Get("cursor"), kind)
		if err != nil {
			gateway.Error(w, 400, "invalid_cursor", "翻页位置无效，请返回第一页")
			return
		}
		limit, err := integer(r.URL.Query().Get("limit"), 24, 1, 48)
		if err != nil {
			gateway.Error(w, 400, "invalid_window", "每次最多加载 48 本书")
			return
		}
		page, err := a.page(r.Context(), kind, cursor, limit)
		if err != nil {
			apiError(w, err)
			return
		}
		jsonResponse(w, page)
	}
}

func (a collectionAPI) setShelf(w http.ResponseWriter, r *http.Request) {
	id, err := integer(r.PathValue("id"), 0, 1, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return
	}
	// No reader ID or toggle instruction comes from the browser. PUT and DELETE
	// express the desired state, so a lost acknowledgement can be retried safely.
	var body map[string]json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	var trailing any
	if dec.Decode(&body) != nil || body == nil || len(body) != 0 || dec.Decode(&trailing) != io.EOF {
		gateway.Error(w, 400, "invalid_shelf", "收藏请求格式无效")
		return
	}
	added := r.Method == http.MethodPut
	if err := a.store.SetShelf(r.Context(), a.readerID, id, added); err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, map[string]any{"bookId": id, "inShelf": added})
}

type featuredBook struct {
	Book
	XiancaoCount int64 `json:"xiancaoCount"`
}

type homeResponse struct {
	Stats struct {
		Books      int64 `json:"books"`
		Authors    int64 `json:"authors"`
		Categories int64 `json:"categories"`
		Readable   int64 `json:"readable"`
	} `json:"stats"`
	Featured  []featuredBook    `json:"featured"`
	HighRated []Book            `json:"highRated"`
	Latest    []Book            `json:"latest"`
	Reading   []collectionEntry `json:"reading"`
}

func (a collectionAPI) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := a.store.Queries
	stats, err := q.HomeStats(ctx)
	if err != nil {
		apiError(w, err)
		return
	}
	featured, err := q.FeaturedCandidates(ctx)
	if err != nil {
		apiError(w, err)
		return
	}
	high, err := q.HighRatedBooks(ctx)
	if err != nil {
		apiError(w, err)
		return
	}
	latest, err := q.LatestBooks(ctx)
	if err != nil {
		apiError(w, err)
		return
	}
	reading, err := a.page(ctx, "reading", collectionCursor{}, 3)
	if err != nil {
		apiError(w, err)
		return
	}
	response := homeResponse{Featured: make([]featuredBook, 0), HighRated: make([]Book, 0), Latest: make([]Book, 0), Reading: reading.Entries}
	response.Stats.Books, response.Stats.Authors, response.Stats.Categories, response.Stats.Readable = stats.Books, stats.Authors, stats.Categories, stats.Readable
	// Sample only the bounded top-vote pool; never ORDER BY random() over the library.
	rand.Shuffle(len(featured), func(i, j int) { featured[i], featured[j] = featured[j], featured[i] })
	for _, b := range featured[:min(5, len(featured))] {
		response.Featured = append(response.Featured, featuredBook{Book: Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score, Intro: b.Intro}, XiancaoCount: b.XiancaoCount})
	}
	for _, b := range high {
		response.HighRated = append(response.HighRated, Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score})
	}
	for _, b := range latest {
		response.Latest = append(response.Latest, Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score})
	}
	jsonResponse(w, response)
}
