package service

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/EthanHannn/personal-library/internal/catalog"
	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/httpapi"
	"github.com/EthanHannn/personal-library/internal/storage"
	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type readingAPI struct {
	store    *storage.Repository
	content  *httpapi.App
	readerID string
}

func (a readingAPI) bookID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := integer(r.PathValue("id"), 0, 1, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return 0, false
	}
	if _, err := a.store.Book(r.Context(), id); err != nil {
		httpapi.RespondError(w, err)
		return 0, false
	}
	return id, true
}

func (a readingAPI) progress(w http.ResponseWriter, r *http.Request) {
	id, ok := a.bookID(w, r)
	if !ok {
		return
	}
	p, err := a.store.Progress(r.Context(), a.readerID, id)
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, map[string]any{"progress": p})
}

type readingState struct {
	Book             catalog.Book      `json:"book"`
	Content          *httpapi.Content  `json:"content"`
	Progress         *catalog.Progress `json:"progress"`
	InShelf          bool              `json:"inShelf"`
	Previous         *catalog.Chapter  `json:"previous"`
	Next             *catalog.Chapter  `json:"next"`
	RecoveryRequired bool              `json:"recoveryRequired"`
}

func (a readingAPI) directory(w http.ResponseWriter, r *http.Request) {
	id, ok := a.bookID(w, r)
	if !ok {
		return
	}
	after, err := integer(r.URL.Query().Get("after"), 0, 0, 1<<53-1)
	query := r.URL.Query().Get("q")
	if err != nil || utf8.RuneCountInString(query) > 80 {
		gateway.Error(w, 400, "invalid_window", "目录查询无效")
		return
	}
	rows, err := a.store.Queries.SearchChapters(r.Context(), dbgen.SearchChaptersParams{BookID: id, AfterIdx: after, Query: query, PageLimit: 101})
	if err != nil {
		apiError(w, err)
		return
	}
	var next *int64
	if len(rows) > 100 {
		rows = rows[:100]
		idx := rows[99].Idx
		next = &idx
	}
	chapters := make([]catalog.Chapter, 0, len(rows))
	for _, row := range rows {
		chapters = append(chapters, catalog.Chapter{Index: row.Idx, Title: row.Title})
	}
	jsonResponse(w, map[string]any{"chapters": chapters, "nextAfter": next})
}

func (a readingAPI) initialize(w http.ResponseWriter, r *http.Request) {
	id, ok := a.bookID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	p, err := a.store.Progress(ctx, a.readerID, id)
	if err != nil {
		apiError(w, err)
		return
	}
	idx := int64(1)
	if p != nil {
		idx = p.ChapterIdx
	}
	explicit := r.URL.Query().Has("chapter")
	if explicit {
		idx, err = integer(r.URL.Query().Get("chapter"), 0, 1, 1<<53-1)
		if err != nil {
			gateway.Error(w, 400, "invalid_chapter", "章节编号无效")
			return
		}
	}
	token := ""
	if !explicit && p != nil && p.Anchor != nil {
		token = p.Anchor.Block
	}
	block, err := a.content.Content(ctx, id, idx, token)
	recovery := false
	if errors.Is(err, httpapi.ErrCursor) && !explicit {
		// Preserve the old record. The browser must ask before saving a new
		// position when a revised text can no longer resolve its anchor.
		recovery = true
		block, err = a.content.Content(ctx, id, idx, "")
	}
	if errors.Is(err, catalog.ErrNotFound) && !explicit && p != nil {
		recovery = true
		block = nil
	} else if err != nil {
		httpapi.RespondError(w, err)
		return
	}
	book, err := a.store.Book(ctx, id)
	if err != nil {
		httpapi.RespondError(w, err)
		return
	}
	shelf, err := a.store.Queries.InShelf(ctx, dbgen.InShelfParams{ReaderID: a.readerID, BookID: id})
	if err != nil {
		apiError(w, err)
		return
	}
	state := readingState{Book: book, Content: block, Progress: p, InShelf: shelf != 0, RecoveryRequired: recovery}
	previous, err := a.store.Queries.PreviousChapter(ctx, dbgen.PreviousChapterParams{BookID: id, ChapterIdx: idx})
	if err != nil {
		apiError(w, err)
		return
	}
	if len(previous) > 0 {
		state.Previous = &catalog.Chapter{Index: previous[0].Idx, Title: previous[0].Title}
	}
	next, err := a.store.Chapters(ctx, id, idx, 1)
	if err != nil {
		apiError(w, err)
		return
	}
	if len(next) > 0 {
		state.Next = &next[0]
	}
	jsonResponse(w, state)
}

var mutationID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func (a readingAPI) save(w http.ResponseWriter, r *http.Request) {
	id, ok := a.bookID(w, r)
	if !ok {
		return
	}
	var m catalog.ProgressMutation
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	err := dec.Decode(&m)
	var trailing any
	if err != nil || dec.Decode(&trailing) != io.EOF || !mutationID.MatchString(m.MutationID) || !mutationID.MatchString(m.DeviceID) || m.ClientSeq < 1 || m.ClientSeq > 1<<53-1 || m.BaseRevision < 0 || m.BaseRevision > 1<<53-2 || m.ChapterIdx < 1 || math.IsNaN(m.Percent) || m.Percent < 0 || m.Percent > 1 || m.Anchor == nil || len(m.Anchor.Block) > 1024 || len(m.ContentVersion) != 64 {
		gateway.Error(w, 400, "invalid_progress", "进度格式无效")
		return
	}
	p, err := a.store.SaveProgress(r.Context(), a.readerID, id, m, func() error { return a.content.ValidatePosition(r.Context(), id, m.Position) })
	var conflict *storage.ProgressConflict
	if errors.As(err, &conflict) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		jsonResponse(w, map[string]any{"error": map[string]string{"code": conflict.Code, "message": "阅读位置已更新，请选择要保留的位置"}, "progress": conflict.Current, "submitted": m.Position})
		return
	}
	if err != nil {
		httpapi.RespondError(w, err)
		return
	}
	jsonResponse(w, map[string]any{"progress": p})
}
