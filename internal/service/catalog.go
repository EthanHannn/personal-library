package service

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/storage"
	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type catalogAPI struct{ store *storage.Repository }

// Keep the M1 ID-window contract for callers without catalog parameters. Page
// requests and filter links use the richer contract; the two cannot be mixed.
func (a catalogAPI) books(w http.ResponseWriter, r *http.Request) {
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > 8192 {
		gateway.Error(w, 400, "invalid_filter", "检索参数无效或过长")
		return
	}
	pageMode := false
	for key, values := range v {
		if len(values) != 1 {
			err = errors.New("duplicate parameter")
		}
		switch key {
		case "after", "limit":
		case "page", "search", "author", "tag", "tag1", "tag2", "minScore", "maxScore", "minWords", "maxWords", "onlyContent", "sortBy":
			pageMode = true
		default:
			err = errors.New("unknown parameter")
		}
	}
	if err != nil || (pageMode && v.Has("after")) {
		gateway.Error(w, 400, "invalid_filter", "检索参数无效，请重置筛选后重试")
		return
	}
	if !pageMode {
		a.window(w, r, v)
		return
	}
	p, err := catalogParameters(v)
	if err != nil {
		gateway.Error(w, 400, "invalid_filter", err.Error())
		return
	}
	result, err := a.store.Catalog(r.Context(), p)
	if err != nil {
		apiError(w, err)
		return
	}
	books := make([]catalogBook, 0, len(result.Books))
	for _, b := range result.Books {
		books = append(books, catalogBook{Book: Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score}, Popularity: b.Popularity, XiancaoCount: b.XiancaoCount})
	}
	jsonResponse(w, map[string]any{"books": books, "pagination": map[string]int64{"page": result.Page, "limit": p.Limit, "total": result.Stats.Total, "totalPages": result.TotalPages}, "summary": map[string]int64{"authors": result.Stats.Authors, "readable": result.Stats.Readable}})
}

type catalogBook struct {
	Book
	Popularity   int64 `json:"popularity"`
	XiancaoCount int64 `json:"xiancaoCount"`
}

func catalogParameters(v url.Values) (storage.CatalogParams, error) {
	var p storage.CatalogParams
	texts := []struct {
		key string
		to  *string
		max int
	}{{"search", &p.Search, 100}, {"author", &p.Author, 200}, {"tag", &p.Tag, 200}, {"tag1", &p.Tag1, 200}, {"tag2", &p.Tag2, 200}}
	for _, item := range texts {
		raw := v.Get(item.key)
		if !validCatalogText(raw, item.max) {
			return p, errors.New("搜索词最多 100 字，作者或分类最多 200 字，且不能包含控制字符")
		}
		*item.to = raw
	}
	p.Search = strings.TrimSpace(p.Search)
	for _, item := range []struct {
		key                string
		to                 *int64
		fallback, min, max int64
	}{{"page", &p.Page, 1, 1, 10000}, {"limit", &p.Limit, 24, 1, 48}, {"minWords", &p.MinWords, 0, 0, 1<<53 - 1}, {"maxWords", &p.MaxWords, 1<<53 - 1, 0, 1<<53 - 1}, {"onlyContent", &p.OnlyContent, 0, 0, 1}} {
		n, err := integer(v.Get(item.key), item.fallback, item.min, item.max)
		if err != nil {
			return p, errors.New("页码、字数或正文筛选无效，每页最多 48 本")
		}
		*item.to = n
	}
	for _, item := range []struct {
		key      string
		to       *float64
		fallback float64
	}{{"minScore", &p.MinScore, 0}, {"maxScore", &p.MaxScore, 10}} {
		*item.to = item.fallback
		if raw := v.Get(item.key); raw != "" {
			n, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 10 {
				return p, errors.New("评分范围须在 0 至 10 分之间")
			}
			*item.to = n
		}
	}
	if p.MinScore > p.MaxScore || p.MinWords > p.MaxWords {
		return p, errors.New("筛选范围的下限不能高于上限")
	}
	p.SortBy = v.Get("sortBy")
	if p.SortBy == "" {
		p.SortBy = "score"
	}
	switch p.SortBy {
	case "score", "xiancao", "popularity", "latest", "words", "title":
	default:
		return p, errors.New("排序方式无效")
	}
	return p, nil
}

func validCatalogText(s string, limit int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit && strings.IndexFunc(s, unicode.IsControl) == -1
}

func (a catalogAPI) categories(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.Queries.CatalogCategories(r.Context())
	if err != nil {
		apiError(w, err)
		return
	}
	// Counts cover all approved holdings; they do not pretend to describe the
	// active filters. No cache needs invalidation after future metadata imports.
	type subcategory struct {
		Name  string `json:"name"`
		Count int64  `json:"count"`
	}
	type category struct {
		Name          string        `json:"name"`
		Count         int64         `json:"count"`
		Subcategories []subcategory `json:"subcategories"`
	}
	groups := make([]category, 0)
	for _, row := range rows {
		if len(groups) == 0 || groups[len(groups)-1].Name != row.Tag1 {
			groups = append(groups, category{Name: row.Tag1, Subcategories: []subcategory{}})
		}
		g := &groups[len(groups)-1]
		g.Count += row.Total
		if row.Tag2 != "" {
			g.Subcategories = append(g.Subcategories, subcategory{Name: row.Tag2, Count: row.Total})
		}
	}
	jsonResponse(w, map[string]any{"categories": groups})
}

func (a catalogAPI) window(w http.ResponseWriter, r *http.Request, v url.Values) {
	after, err := integer(v.Get("after"), 0, 0, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_window", "书目起点无效")
		return
	}
	limit, err := integer(v.Get("limit"), 24, 1, 48)
	if err != nil {
		gateway.Error(w, 400, "invalid_window", "每次最多加载 48 本书")
		return
	}
	rows, err := a.store.Queries.ListBooks(r.Context(), dbgen.ListBooksParams{AfterID: after, PageLimit: limit + 1})
	if err != nil {
		apiError(w, err)
		return
	}
	var next *int64
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1].ID
		next = &last
	}
	books := make([]Book, 0, len(rows))
	for _, b := range rows {
		books = append(books, Book{ID: b.ID, CoverURL: coverURL(b.ID, b.HasCover), Title: b.Title, Author: b.Author, Tag1: b.Tag1, Tag2: b.Tag2, WordCount: b.WordCount, ChapterCount: b.ChapterCount, HasContent: b.HasContent == 1, Score: b.Score})
	}
	jsonResponse(w, map[string]any{"books": books, "nextAfter": next})
}
