package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/EthanHannn/personal-library/internal/catalog"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type Repository struct {
	DB      *Database
	Queries *dbgen.Queries
}

func NewRepository(db *Database) *Repository         { return &Repository{db, dbgen.New(db.Read)} }
func (s *Repository) Ping(ctx context.Context) error { return s.DB.Read.PingContext(ctx) }

func (s *Repository) Book(ctx context.Context, id int64) (catalog.Book, error) {
	b, err := s.Queries.ReadableBook(ctx, id)
	return catalog.Book{ID: b.ID, Title: b.Title, Author: b.Author, ChapterCount: b.ChapterCount}, notFound(err)
}

func (s *Repository) Chapters(ctx context.Context, id, after int64, limit int) ([]catalog.Chapter, error) {
	rows, err := s.Queries.ChapterWindow(ctx, dbgen.ChapterWindowParams{BookID: id, AfterIdx: after, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	chapters := make([]catalog.Chapter, 0, len(rows))
	for _, c := range rows {
		chapters = append(chapters, catalog.Chapter{Index: c.Idx, Title: c.Title})
	}
	return chapters, nil
}

func (s *Repository) Chapter(ctx context.Context, id, idx int64) (catalog.Book, catalog.Chapter, reader.File, error) {
	row, err := s.Queries.ReadChapter(ctx, dbgen.ReadChapterParams{BookID: id, ChapterIdx: idx})
	b := catalog.Book{ID: row.ID, Title: row.Title, Author: row.Author, ChapterCount: row.ChapterCount}
	c := catalog.Chapter{Index: row.Idx, Title: row.ChapterTitle, Start: row.StartOffset, End: row.EndOffset}
	f := reader.File{Path: row.Path, Encoding: row.Encoding, Size: row.Size, ModifiedNS: row.ModifiedNs, SHA256: row.Sha256}
	return b, c, f, notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return catalog.ErrNotFound
	}
	return err
}
