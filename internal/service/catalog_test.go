package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func catalogFixture(t *testing.T) http.Handler {
	t.Helper()
	h, db := serviceFixture(t)
	if _, err := db.Write.Exec(`UPDATE books SET status='PENDING',tag1='秘密分类',author='秘密作者'`); err != nil {
		t.Fatal(err)
	}
	for _, b := range []struct {
		id                        int
		title, author, tag1, tag2 string
		words, content, created   int
		score                     float64
		popularity, xiancao       int
	}{
		{101, "星河", "作者甲", "奇幻", "仙侠", 500000, 1, 100, 9, 40, 10},
		{102, "星海", "作者甲", "奇幻", "史诗", 1000000, 1, 300, 9, 80, 8},
		{103, "100%_行者", "O'Neil/中文? #", "科幻", "赛博", 2000001, 1, 200, 0, 1, 0},
		{104, "alpha", "Alice", "奇幻", "仙侠", 0, 0, 400, 8, 60, 9},
		{105, "星河", "作者甲", "奇幻", "仙侠", 500000, 1, 100, 9, 40, 10},
	} {
		if _, err := db.Write.Exec(`INSERT INTO books(id,title,author,tag1,tag2,size,intro,status,word_count,chapter_count,has_content,created_at,updated_at)
VALUES(?,?,?,?,?,'','介绍','APPROVED',?,1,?,?,0)`, b.id, b.title, b.author, b.tag1, b.tag2, b.words, b.content, b.created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Write.Exec(`INSERT INTO rating_snapshots VALUES(?,?,0,?,?,0,0,0,0)`, b.id, b.popularity, b.score, b.xiancao); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

type catalogResponse struct {
	Books      []catalogBook
	Pagination struct{ Page, Limit, Total, TotalPages int64 }
	Summary    struct{ Authors, Readable int64 }
}

func catalogResult(t *testing.T, h http.Handler, query string) catalogResponse {
	t.Helper()
	w := call(h, "GET", "/api/v1/books?"+query, "owner")
	if w.Code != 200 {
		t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
	}
	var result catalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func catalogIDs(r catalogResponse) []int64 {
	ids := make([]int64, 0, len(r.Books))
	for _, b := range r.Books {
		ids = append(ids, b.ID)
	}
	return ids
}

func TestCatalogLiteralSearchAndFilters(t *testing.T) {
	h := catalogFixture(t)
	for _, tc := range []struct {
		query string
		want  []int64
	}{
		{"search=" + url.QueryEscape("星"), []int64{105, 102, 101}},
		{"search=" + url.QueryEscape("星河"), []int64{105, 101}},
		{"search=" + url.QueryEscape("%"), []int64{103}},
		{"search=" + url.QueryEscape("_"), []int64{103}},
		{"search=" + url.QueryEscape("  o'NEIL  "), []int64{103}},
		{"search=" + url.QueryEscape("%' OR 1=1 --"), []int64{}},
		{"author=" + url.QueryEscape("O'Neil/中文? #"), []int64{103}},
		{"author=" + url.QueryEscape("作者"), []int64{}},
		{"tag=" + url.QueryEscape("仙侠"), []int64{105, 101, 104}},
		{"tag1=" + url.QueryEscape("奇幻") + "&tag2=" + url.QueryEscape("史诗"), []int64{102}},
		{"minScore=7.5&onlyContent=1", []int64{105, 102, 101}},
		{"minScore=0&maxScore=0&onlyContent=1", []int64{103}},
		{"minWords=500001&maxWords=1000000", []int64{102}},
		{"minWords=0&maxWords=0", []int64{104}},
		{"minScore=8&maxScore=8&onlyContent=0", []int64{104}},
		{"search=" + url.QueryEscape("秘密"), []int64{}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			r := catalogResult(t, h, "page=1&"+tc.query)
			if !reflect.DeepEqual(catalogIDs(r), tc.want) || r.Pagination.Total != int64(len(tc.want)) {
				t.Fatalf("got %+v; want %v", r, tc.want)
			}
		})
	}
	r := catalogResult(t, h, "page=1")
	if r.Summary.Authors != 3 || r.Summary.Readable != 4 || r.Pagination.Total != 5 {
		t.Fatalf("wrong approved-only summary: %+v", r)
	}
	// A zero-score book is still collectable; discovery filters are not policy.
	if w := shelfRequest(h, "PUT", "/api/v1/books/103/shelf", "{}", "owner", "https://library.test"); w.Code != 200 {
		t.Fatalf("low score shelf: %d %s", w.Code, w.Body.String())
	}
}

func TestCatalogSortsAndStablePages(t *testing.T) {
	h := catalogFixture(t)
	for sort, want := range map[string][]int64{
		"score": {105, 102, 101, 104, 103}, "xiancao": {105, 101, 104, 102, 103},
		"popularity": {102, 104, 105, 101, 103}, "latest": {104, 102, 103, 105, 101},
		"words": {103, 102, 105, 101, 104}, "title": {103, 104, 105, 101, 102},
	} {
		t.Run(sort, func(t *testing.T) {
			var ids []int64
			for page := 1; page <= 3; page++ {
				r := catalogResult(t, h, fmt.Sprintf("page=%d&limit=2&sortBy=%s", page, sort))
				if r.Pagination.TotalPages != 3 || r.Pagination.Total != 5 || r.Pagination.Page != int64(page) {
					t.Fatalf("pagination: %+v", r.Pagination)
				}
				ids = append(ids, catalogIDs(r)...)
			}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("got %v; want %v", ids, want)
			}
		})
	}
	if r := catalogResult(t, h, "page=10000&limit=2"); r.Pagination.Page != 3 || !reflect.DeepEqual(catalogIDs(r), []int64{103}) {
		t.Fatalf("out of range must settle on last page: %+v", r)
	}
	if r := catalogResult(t, h, "page=10&search=missing"); r.Books == nil || len(r.Books) != 0 || r.Pagination.TotalPages != 0 || r.Pagination.Page != 1 {
		t.Fatalf("empty page: %+v", r)
	}
}

func TestCatalogValidationAndCategories(t *testing.T) {
	h := catalogFixture(t)
	for _, query := range []string{
		"page=0", "page=10001", "limit=49&page=1", "after=0&page=1", "search=x&after=1",
		"minWords=-1", "minWords=10&maxWords=9", "maxWords=9007199254740992",
		"minScore=NaN", "maxScore=Inf", "minScore=-1", "maxScore=10.1", "minScore=8&maxScore=7.5",
		"sortBy=score;DROP", "onlyContent=2", "search=a&search=b", "status=PENDING", "foo=bar",
		"search=%FF", "search=%00", "search=%0A", "search=" + url.QueryEscape(strings.Repeat("字", 101)),
		"author=" + url.QueryEscape(strings.Repeat("字", 201)), "search=" + strings.Repeat("x", 8193),
	} {
		if w := call(h, "GET", "/api/v1/books?"+query, "owner"); w.Code != 400 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("invalid query %s: %d %s", query, w.Code, w.Body.String())
		}
	}
	w := call(h, "GET", "/api/v1/categories", "owner")
	if w.Code != 200 || strings.Contains(w.Body.String(), "秘密") {
		t.Fatalf("categories: %d %s", w.Code, w.Body.String())
	}
	var groups struct {
		Categories []struct {
			Name          string
			Count         int
			Subcategories []struct {
				Name  string
				Count int
			}
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups.Categories) != 2 || groups.Categories[0].Name != "奇幻" || groups.Categories[0].Count != 4 || groups.Categories[0].Subcategories[0].Name != "仙侠" || groups.Categories[0].Subcategories[0].Count != 3 {
		t.Fatalf("category counts: %s", w.Body.String())
	}
	for _, path := range []string{"/api/v1/books?page=1", "/api/v1/categories", "/author/中文", "/category/仙侠"} {
		for user, code := range map[string]int{"": 401, "stranger": 403} {
			if w := call(h, "GET", path, user); w.Code != code {
				t.Fatalf("identity %s: %d", path, w.Code)
			}
		}
	}
}

func TestCatalogPagePaths(t *testing.T) {
	h, _ := serviceFixture(t)
	for _, name := range []string{"作者甲", "O'Neil/中文? #", "%2F", "甲 & 乙"} {
		for _, prefix := range []string{"/author/", "/category/"} {
			for _, method := range []string{"GET", "HEAD"} {
				w := call(h, method, prefix+url.PathEscape(name), "owner")
				if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
					t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
				}
			}
		}
	}
	for _, path := range []string{"/author/", "/category/", "/author/a/b", "/author/%00", "/category/%FF", "/author/%2E", "/author/" + url.PathEscape(strings.Repeat("字", 201))} {
		if w := call(h, "GET", path, "owner"); w.Code != 404 {
			t.Fatalf("bad path %s: %d", path, w.Code)
		}
	}
}
