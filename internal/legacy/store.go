// Package legacy opens an existing zhixuan-library schema without modifying it.
// M0 uses synthetic snapshots; production migration is a separate later step.
package legacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/EthanHannn/personal-library/internal/catalog"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound  = catalog.ErrNotFound
	ErrAmbiguous = catalog.ErrAmbiguous
)

type Book = catalog.Book
type Chapter = catalog.Chapter

type Store struct{ db *sql.DB }

func Open(ctx context.Context, filename string) (*Store, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "foreign_keys(1)", "busy_timeout(1000)", "cache_size(-8192)", "trusted_schema(0)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	// Check the shape without scanning the catalog or chapter index at startup.
	rows, err := db.QueryContext(ctx, `SELECT b.id, b.title, b.author, b.filePath, b.chapterCount, c.idx, c.startOffset, c.endOffset FROM books b JOIN chapters c ON c.bookId = b.id WHERE 0`)
	if err != nil {
		db.Close()
		return nil, err
	}
	rows.Close()
	return &Store{db: db}, nil
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Book(ctx context.Context, id int64) (Book, error) {
	var b Book
	err := s.db.QueryRowContext(ctx, `SELECT id, title, author, chapterCount, COALESCE(filePath, '') FROM books WHERE id = ? AND hasContent = 1`, id).Scan(&b.ID, &b.Title, &b.Author, &b.ChapterCount, &b.FilePath)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}

const chapterSQL = `SELECT b.id, b.title, b.author, b.chapterCount, b.filePath, c.idx, c.title, c.startOffset, c.endOffset
FROM books b JOIN chapters c ON c.bookId = b.id
WHERE b.id = ? AND c.idx = ? AND b.hasContent = 1 LIMIT 2`

func (s *Store) Chapter(ctx context.Context, id, idx int64) (Book, Chapter, error) {
	var b Book
	var c Chapter
	rows, err := s.db.QueryContext(ctx, chapterSQL, id, idx)
	if err != nil {
		return b, c, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return b, c, err
		}
		return b, c, ErrNotFound
	}
	if err := rows.Scan(&b.ID, &b.Title, &b.Author, &b.ChapterCount, &b.FilePath, &c.Index, &c.Title, &c.Start, &c.End); err != nil {
		return b, c, err
	}
	if rows.Next() {
		return b, c, ErrAmbiguous
	}
	return b, c, rows.Err()
}

func (s *Store) Chapters(ctx context.Context, id, after int64, limit int) ([]Chapter, error) {
	if id < 1 || after < 0 || limit < 1 || limit > 200 {
		return nil, fmt.Errorf("invalid chapter window")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT idx, title FROM chapters WHERE bookId = ? AND idx > ? ORDER BY idx LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chapters := make([]Chapter, 0, limit)
	for rows.Next() {
		var c Chapter
		if err := rows.Scan(&c.Index, &c.Title); err != nil {
			return nil, err
		}
		chapters = append(chapters, c)
	}
	return chapters, rows.Err()
}

func (s *Store) QueryPlan(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+chapterSQL, 1, 1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			return nil, err
		}
		plans = append(plans, detail)
	}
	return plans, rows.Err()
}
