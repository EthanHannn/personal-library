// Package management runs one durable background task at a time. Network and
// indexing work never runs in the interactive HTTP request or a DB transaction.
package management

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
)

var ErrConflict = errors.New("内容已变化或重复，请重新预览")

type Metadata struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	Tag1   string `json:"tag1"`
	Tag2   string `json:"tag2"`
	Intro  string `json:"intro"`
}
type Input struct {
	Metadata
	Path            string           `json:"path,omitempty"`
	Encoding        string           `json:"encoding,omitempty"`
	BookID          int64            `json:"bookId,omitempty"`
	URL             string           `json:"url,omitempty"`
	MaxChapters     int              `json:"maxChapters,omitempty"`
	SourceID        string           `json:"sourceId,omitempty"`
	SourceVersion   int              `json:"sourceVersion,omitempty"`
	SourceIDs       []string         `json:"sourceIds,omitempty"`
	SourceSnapshots []SourceSnapshot `json:"sourceSnapshots,omitempty"`
	Query           string           `json:"query,omitempty"`
}
type Job struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Status    string          `json:"status"`
	Input     Input           `json:"input"`
	Result    json.RawMessage `json:"result"`
	Message   string          `json:"message"`
	Attempts  int             `json:"attempts"`
	NextAt    int64           `json:"nextAt"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}
type Manager struct {
	DB        *storage.Database
	Reader    *reader.Reader
	Novels    string
	Covers    *os.Root
	Collected *os.Root
	Remote    *Remote
	mutex     sync.Mutex
}

func Open(db *storage.Database, r *reader.Reader, novels, covers, collected string) (*Manager, error) {
	c, err := os.OpenRoot(covers)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenRoot(collected)
	if err != nil {
		c.Close()
		return nil, err
	}
	return &Manager{DB: db, Reader: r, Novels: novels, Covers: c, Collected: f, Remote: NewRemote(db)}, nil
}
func (m *Manager) Close() { m.Covers.Close(); m.Collected.Close() }
func (m *Manager) Start(parent context.Context) (func(), error) {
	ctx, cancel := context.WithCancel(parent)
	if _, err := m.DB.Write.ExecContext(ctx, `UPDATE library_jobs SET status='queued' WHERE status='running'`); err != nil {
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				m.RunOne(ctx)
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}
func ValidMetadata(b Metadata) bool {
	return strings.TrimSpace(b.Title) != "" && len([]rune(b.Title)) <= 200 && len([]rune(b.Author)) <= 200 && len([]rune(b.Tag1)) <= 100 && len([]rune(b.Tag2)) <= 100 && len(b.Intro) <= 12000 && !strings.ContainsAny(b.Title+b.Author+b.Tag1+b.Tag2, "\x00\r\n") && !strings.ContainsRune(b.Intro, 0)
}
func (m *Manager) Enqueue(ctx context.Context, kind string, in Input) (Job, error) {
	in.SourceSnapshots = nil
	switch kind {
	case "import":
		if !ValidMetadata(in.Metadata) || (in.Encoding != "utf-8" && in.Encoding != "gb18030") || !strings.EqualFold(filepath.Ext(in.Path), ".txt") {
			return Job{}, fmt.Errorf("请输入书名、TXT 相对路径和编码")
		}
		if _, err := m.Reader.Inspect(in.Path, in.Encoding); err != nil {
			return Job{}, fmt.Errorf("无法读取小说目录内的 TXT 文件")
		}
	case "collect":
		if err := m.bindSources(ctx, kind, &in); err != nil {
			return Job{}, err
		}
		if in.MaxChapters < 1 || in.MaxChapters > 2000 {
			return Job{}, fmt.Errorf("每次采集 1–2000 章")
		}
	case "search", "source-check":
		if err := m.bindSources(ctx, kind, &in); err != nil {
			return Job{}, err
		}
	case "cover":
		if in.BookID < 1 {
			return Job{}, fmt.Errorf("作品编号无效")
		}
		var n int
		if err := m.DB.Read.QueryRowContext(ctx, `SELECT count(*) FROM books WHERE id=?`, in.BookID).Scan(&n); err != nil {
			return Job{}, err
		} else if n != 1 {
			return Job{}, sql.ErrNoRows
		}
	case "scan":
	default:
		return Job{}, fmt.Errorf("任务类型无效")
	}
	data, _ := json.Marshal(in)
	sum := sha256.Sum256(append([]byte(kind), data...))
	dedupe := hex.EncodeToString(sum[:])
	if kind == "cover" {
		dedupe = fmt.Sprint(in.BookID)
	}
	tx, err := m.DB.Write.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM library_jobs WHERE kind=? AND dedupe=? AND status IN ('queued','running','review')`, kind, dedupe).Scan(&existing)
	if err == nil {
		tx.Rollback()
		return m.Job(ctx, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM library_jobs WHERE status IN ('queued','running','review')`).Scan(&count); err != nil {
		return Job{}, err
	}
	if count >= 40 {
		return Job{}, fmt.Errorf("待处理任务已满，请先确认或取消预览")
	}
	// Repeated failed lookups cannot avoid the retry delay by creating new jobs.
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(next_at),0) FROM library_jobs WHERE kind=? AND dedupe=?`, kind, dedupe).Scan(&next); err != nil {
		return Job{}, err
	}
	id := rand.Text()
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO library_jobs(id,kind,dedupe,status,payload,next_at,created_at,updated_at) VALUES(?,?,?,'queued',?,?,?,?)`, id, kind, dedupe, string(data), next, now, now); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return m.Job(ctx, id)
}

const jobColumns = `id,kind,status,payload,result,message,attempts,next_at,created_at,updated_at`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var input, result string
	err := row.Scan(&j.ID, &j.Kind, &j.Status, &input, &result, &j.Message, &j.Attempts, &j.NextAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return j, err
	}
	err = json.Unmarshal([]byte(input), &j.Input)
	j.Result = json.RawMessage(result)
	return j, err
}
func (m *Manager) Job(ctx context.Context, id string) (Job, error) {
	return scanJob(m.DB.Read.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM library_jobs WHERE id=?`, id))
}
func (m *Manager) Jobs(ctx context.Context, cursor string) ([]Job, error) {
	at := int64(0)
	if cursor != "" {
		if err := m.DB.Read.QueryRowContext(ctx, `SELECT created_at FROM library_jobs WHERE id=?`, cursor).Scan(&at); err != nil {
			return nil, err
		}
	}
	rows, err := m.DB.Read.QueryContext(ctx, `SELECT `+jobColumns+` FROM library_jobs WHERE ?='' OR created_at<? OR (created_at=? AND id<?) ORDER BY created_at DESC,id DESC LIMIT 41`, cursor, at, at, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]Job, 0)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
func (m *Manager) Action(ctx context.Context, id, action string) error {
	var q string
	switch action {
	case "cancel":
		q = `UPDATE library_jobs SET status='cancelled',message='已取消',updated_at=? WHERE id=? AND status IN ('queued','running','review','failed')`
	case "retry":
		q = `UPDATE library_jobs SET status='queued',message='',updated_at=? WHERE id=? AND status='failed'`
	case "confirm":
		q = `UPDATE library_jobs SET status='queued',message='confirm',updated_at=? WHERE id=? AND status='review' AND kind IN ('import','collect')`
	default:
		return fmt.Errorf("任务操作无效")
	}
	r, err := m.DB.Write.ExecContext(ctx, q, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		if j, e := m.Job(ctx, id); e == nil {
			if action == "confirm" && (j.Status == "completed" || ((j.Status == "queued" || j.Status == "running") && j.Message == "confirm")) {
				return nil
			}
			if action == "cancel" && j.Status == "cancelled" {
				return nil
			}
			if action == "retry" && (j.Status == "queued" || j.Status == "running") {
				return nil
			}
		}
		return ErrConflict
	}
	return nil
}
func (m *Manager) RunOne(ctx context.Context) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	j, err := scanJob(m.DB.Read.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM library_jobs WHERE status='queued' AND next_at<=? ORDER BY created_at,id LIMIT 1`, time.Now().UnixMilli()))
	if err != nil {
		return
	}
	claimed, err := m.DB.Write.ExecContext(ctx, `UPDATE library_jobs SET status='running',attempts=attempts+1,updated_at=? WHERE id=? AND status='queued'`, time.Now().UnixMilli(), j.ID)
	if err != nil {
		return
	}
	if n, _ := claimed.RowsAffected(); n != 1 {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				var status string
				if err := m.DB.Read.QueryRowContext(ctx, `SELECT status FROM library_jobs WHERE id=?`, j.ID).Scan(&status); err == nil && status == "cancelled" {
					cancel()
					return
				}
			}
		}
	}()
	var result any
	status := "completed"
	if j.Message == "confirm" {
		result, err = m.confirm(ctx, j)
	} else {
		switch j.Kind {
		case "import":
			result, err = m.preview(ctx, j, j.Input)
			status = "review"
		case "collect":
			result, err = m.collect(ctx, j)
			status = "review"
		case "cover":
			result, err = m.cover(ctx, j)
		case "scan":
			result, err = m.scan(ctx)
		case "search":
			result, err = m.searchSources(ctx, j)
		case "source-check":
			result, err = m.checkSource(ctx, j)
		}
	}
	if ctx.Err() != nil {
		return
	} // Startup requeues this interrupted running job.
	message := ""
	next := int64(0)
	if err != nil {
		status = "failed"
		result = nil
		if latest, e := m.Job(ctx, j.ID); e == nil {
			j.Result = latest.Result
		}
		message = err.Error()
		if len(message) > 500 {
			message = message[:500]
		}
		next = time.Now().Add(time.Minute).UnixMilli()
		var blocked *BlockedError
		if errors.As(err, &blocked) {
			next = blocked.Until
		}
	}
	data := j.Result
	if result != nil {
		data, _ = json.Marshal(result)
	}
	_, _ = m.DB.Write.ExecContext(ctx, `UPDATE library_jobs SET status=?,result=?,message=?,next_at=?,updated_at=? WHERE id=? AND status='running'`, status, string(data), message, next, time.Now().UnixMilli(), j.ID)
}
