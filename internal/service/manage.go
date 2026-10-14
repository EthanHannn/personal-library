package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/management"
	"github.com/EthanHannn/personal-library/internal/storage"
)

type manageAPI struct {
	store *storage.Repository
	jobs  *management.Manager
}

func decodeBody(w http.ResponseWriter, r *http.Request, value any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(value) != nil || d.Decode(&extra) != io.EOF {
		gateway.Error(w, 400, "invalid_request", "请求格式无效")
		return false
	}
	return true
}
func manageError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, err)
		return
	}
	if errors.Is(err, management.ErrConflict) {
		gateway.Error(w, 409, "changed", "数据已经变化，请刷新或重新预览")
		return
	}
	gateway.Error(w, 400, "management_failed", err.Error())
}
func (a manageAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/manage/books", a.books)
	mux.HandleFunc("POST /api/v1/manage/books", a.saveBook)
	mux.HandleFunc("PUT /api/v1/manage/books/{id}", a.saveBook)
	mux.HandleFunc("GET /api/v1/manage/jobs", a.listJobs)
	mux.HandleFunc("GET /api/v1/manage/jobs/{id}", a.job)
	mux.HandleFunc("POST /api/v1/manage/jobs", a.enqueue)
	mux.HandleFunc("POST /api/v1/manage/cover-batch", a.coverBatch)
	mux.HandleFunc("POST /api/v1/manage/jobs/{id}/{action}", a.action)
	mux.HandleFunc("GET /api/v1/books/{id}/cover", a.cover)
	mux.HandleFunc("POST /api/v1/books/{id}/cover-jobs", a.enqueueCover)
}
func (a manageAPI) available(w http.ResponseWriter) bool {
	if a.jobs == nil {
		gateway.Error(w, 503, "unavailable", "管理任务尚未配置")
		return false
	}
	return true
}
func (a manageAPI) listJobs(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 64 {
		gateway.Error(w, 400, "invalid_page", "翻页位置无效")
		return
	}
	jobs, err := a.jobs.Jobs(r.Context(), cursor)
	if err != nil {
		apiError(w, err)
		return
	}
	var next *string
	if len(jobs) > 40 {
		jobs = jobs[:40]
		next = &jobs[39].ID
	}
	jsonResponse(w, map[string]any{"jobs": jobs, "nextCursor": next})
}
func (a manageAPI) job(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	j, err := a.jobs.Job(r.Context(), r.PathValue("id"))
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, j)
}
func (a manageAPI) enqueue(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var body struct {
		Kind  string           `json:"kind"`
		Input management.Input `json:"input"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	j, err := a.jobs.Enqueue(r.Context(), body.Kind, body.Input)
	if err != nil {
		manageError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(202)
	jsonResponse(w, j)
}
func (a manageAPI) action(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var body struct{}
	if !decodeBody(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if err := a.jobs.Action(r.Context(), id, r.PathValue("action")); err != nil {
		manageError(w, err)
		return
	}
	j, err := a.jobs.Job(r.Context(), id)
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, j)
}
func (a manageAPI) enqueueCover(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var body struct{}
	if !decodeBody(w, r, &body) {
		return
	}
	id, err := integer(r.PathValue("id"), 0, 1, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return
	}
	j, err := a.jobs.Enqueue(r.Context(), "cover", management.Input{BookID: id})
	if err != nil {
		manageError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(202)
	jsonResponse(w, j)
}
func (a manageAPI) cover(w http.ResponseWriter, r *http.Request) {
	if a.jobs == nil {
		gateway.Error(w, 404, "not_found", "暂无真实封面")
		return
	}
	id, err := integer(r.PathValue("id"), 0, 1, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_book", "作品编号无效")
		return
	}
	f, media, err := a.jobs.Cover(r.Context(), id)
	if err != nil {
		gateway.Error(w, 404, "not_found", "暂无真实封面")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", media)
	http.ServeContent(w, r, "cover", time.Time{}, f)
}

func (a manageAPI) coverBatch(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var body struct{}
	if !decodeBody(w, r, &body) {
		return
	}
	rows, err := a.store.DB.Read.QueryContext(r.Context(), `SELECT b.id FROM books b WHERE b.status='APPROVED' AND trim(b.author)<>'' AND NOT EXISTS(SELECT 1 FROM cover_assets WHERE book_id=b.id) AND NOT EXISTS(SELECT 1 FROM covers WHERE book_id=b.id AND legacy_path LIKE '/covers/%' AND legacy_path NOT LIKE '%.svg') AND NOT EXISTS(SELECT 1 FROM library_jobs WHERE kind='cover' AND dedupe=CAST(b.id AS TEXT) AND (status IN ('queued','running') OR next_at>?)) ORDER BY b.has_content DESC,b.id LIMIT 10`, time.Now().UnixMilli())
	if err != nil {
		apiError(w, err)
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			apiError(w, err)
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		apiError(w, err)
		return
	}
	jobs := []management.Job{}
	for _, id := range ids {
		j, err := a.jobs.Enqueue(r.Context(), "cover", management.Input{BookID: id})
		if err != nil {
			if len(jobs) == 0 {
				manageError(w, err)
				return
			}
			break
		}
		jobs = append(jobs, j)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(202)
	jsonResponse(w, map[string]any{"jobs": jobs})
}

type managedBook struct {
	management.Metadata
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	HasContent bool   `json:"hasContent"`
	UpdatedAt  int64  `json:"updatedAt"`
	HasCover   bool   `json:"hasCover"`
}

func (a manageAPI) books(w http.ResponseWriter, r *http.Request) {
	after, err := integer(r.URL.Query().Get("after"), 0, 0, 1<<53-1)
	if err != nil {
		gateway.Error(w, 400, "invalid_page", "翻页位置无效")
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if !validCatalogText(search, 200) {
		gateway.Error(w, 400, "invalid_search", "检索词过长")
		return
	}
	filter := ""
	switch r.URL.Query().Get("filter") {
	case "", "all":
	case "pending":
		filter = " AND b.status='PENDING'"
	case "no-text":
		filter = " AND b.has_content=0"
	case "no-cover":
		filter = " AND NOT EXISTS(SELECT 1 FROM cover_assets WHERE book_id=b.id) AND NOT EXISTS(SELECT 1 FROM covers WHERE book_id=b.id AND legacy_path IS NOT NULL AND legacy_path<>'' AND legacy_path NOT LIKE '%.svg')"
	default:
		gateway.Error(w, 400, "invalid_filter", "筛选条件无效")
		return
	}
	rows, err := a.store.DB.Read.QueryContext(r.Context(), `SELECT b.id,b.title,b.author,b.tag1,b.tag2,b.intro,b.status,b.has_content,b.updated_at,EXISTS(SELECT 1 FROM cover_assets WHERE book_id=b.id) OR EXISTS(SELECT 1 FROM covers WHERE book_id=b.id AND legacy_path IS NOT NULL AND legacy_path<>'' AND legacy_path NOT LIKE '%.svg') FROM books b WHERE b.id>? AND (?='' OR instr(b.title,?)>0 OR instr(b.author,?)>0)`+filter+` ORDER BY b.id LIMIT 41`, after, search, search, search)
	if err != nil {
		apiError(w, err)
		return
	}
	defer rows.Close()
	books := make([]managedBook, 0)
	for rows.Next() {
		var b managedBook
		if err := rows.Scan(&b.ID, &b.Title, &b.Author, &b.Tag1, &b.Tag2, &b.Intro, &b.Status, &b.HasContent, &b.UpdatedAt, &b.HasCover); err != nil {
			apiError(w, err)
			return
		}
		books = append(books, b)
	}
	if err := rows.Err(); err != nil {
		apiError(w, err)
		return
	}
	var next *int64
	if len(books) > 40 {
		books = books[:40]
		n := books[39].ID
		next = &n
	}
	jsonResponse(w, map[string]any{"books": books, "next": next})
}
func (a manageAPI) saveBook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		management.Metadata
		Status          string `json:"status"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if !management.ValidMetadata(body.Metadata) || (body.Status != "APPROVED" && body.Status != "PENDING") {
		gateway.Error(w, 400, "invalid_metadata", "书目信息或状态无效")
		return
	}
	tx, err := a.store.DB.Write.BeginTx(r.Context(), nil)
	if err != nil {
		apiError(w, err)
		return
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	id := int64(0)
	if r.Method == http.MethodPut {
		id, err = integer(r.PathValue("id"), 0, 1, 1<<53-1)
		if err != nil {
			gateway.Error(w, 400, "invalid_book", "作品编号无效")
			return
		}
	}
	var count int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM books WHERE title=? AND author=? AND id<>?`, body.Title, body.Author, id).Scan(&count); err != nil {
		apiError(w, err)
		return
	}
	if count > 0 {
		gateway.Error(w, 409, "duplicate", "同名同作者作品已经存在")
		return
	}
	if id == 0 {
		result, err := tx.ExecContext(r.Context(), `INSERT INTO books(title,author,tag1,tag2,size,intro,status,chapter_count,word_count,has_content,created_at,updated_at) VALUES(?,?,?,?,'',?,?,0,0,0,?,?)`, body.Title, body.Author, body.Tag1, body.Tag2, body.Intro, body.Status, now, now)
		if err != nil {
			apiError(w, err)
			return
		}
		id, _ = result.LastInsertId()
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO rating_snapshots VALUES(?,0,0,0,0,0,0,0,0)`, id); err != nil {
			apiError(w, err)
			return
		}
	} else {
		result, err := tx.ExecContext(r.Context(), `UPDATE books SET title=?,author=?,tag1=?,tag2=?,intro=?,status=?,updated_at=? WHERE id=? AND updated_at=?`, body.Title, body.Author, body.Tag1, body.Tag2, body.Intro, body.Status, max(now, body.ExpectedVersion+1), id, body.ExpectedVersion)
		if err != nil {
			apiError(w, err)
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			manageError(w, management.ErrConflict)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, map[string]any{"bookId": id})
}
