package storage

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

// SetShelf changes only the collection relationship. It never starts reading,
// deletes progress, or rewrites an existing entry's identity and ordering key.
func (s *Repository) SetShelf(ctx context.Context, readerID string, bookID int64, added bool) error {
	tx, err := s.DB.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.Queries.WithTx(tx)
	if _, err := q.BookDetail(ctx, bookID); err != nil {
		return err
	}
	if added {
		now := time.Now().UTC().UnixMilli()
		err = q.AddToShelf(ctx, dbgen.AddToShelfParams{ID: rand.Text(), ReaderID: readerID, BookID: bookID, AddedAt: now, UpdatedAt: now})
	} else {
		err = q.RemoveFromShelf(ctx, dbgen.RemoveFromShelfParams{ReaderID: readerID, BookID: bookID})
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
