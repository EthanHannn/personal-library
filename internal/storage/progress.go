package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/EthanHannn/personal-library/internal/catalog"
	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type ProgressConflict struct {
	Code    string
	Current *catalog.Progress
}

func (e *ProgressConflict) Error() string { return e.Code }

func readProgress(ctx context.Context, q *dbgen.Queries, readerID string, bookID int64) (*catalog.Progress, error) {
	row, err := q.ReadingProgress(ctx, dbgen.ReadingProgressParams{ReaderID: readerID, BookID: bookID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &catalog.Progress{Position: catalog.Position{ChapterIdx: row.ChapterIdx, Percent: row.Percent, ContentVersion: row.ContentVersion.String}, Revision: row.Revision, UpdatedAt: row.UpdatedAt}
	if row.Anchor.Valid {
		if err := json.Unmarshal([]byte(row.Anchor.String), &p.Anchor); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (s *Repository) Progress(ctx context.Context, readerID string, bookID int64) (*catalog.Progress, error) {
	return readProgress(ctx, s.Queries, readerID, bookID)
}

// The receipt, device sequence and progress advance atomically. Validation runs
// after replay/conflict detection under the writer lock; index publication cannot
// race it. It performs one bounded block read, never a scan of the book.
func (s *Repository) SaveProgress(ctx context.Context, readerID string, bookID int64, m catalog.ProgressMutation, validate func() error) (*catalog.Progress, error) {
	tx, err := s.DB.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := s.Queries.WithTx(tx)
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	current, err := readProgress(ctx, q, readerID, bookID)
	if err != nil {
		return nil, err
	}
	receipt, err := q.ProgressReceipt(ctx, dbgen.ProgressReceiptParams{ReaderID: readerID, BookID: bookID, MutationID: m.MutationID})
	if err == nil {
		if receipt.RequestHash != hash {
			return nil, &ProgressConflict{"mutation_reused", current}
		}
		var result catalog.Progress
		if err := json.Unmarshal([]byte(receipt.Response), &result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	seq, err := q.ProgressSequence(ctx, dbgen.ProgressSequenceParams{ReaderID: readerID, BookID: bookID, DeviceID: m.DeviceID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if m.ClientSeq <= seq {
		return nil, &ProgressConflict{"stale_sequence", current}
	}
	revision := int64(0)
	if current != nil {
		revision = current.Revision
	}
	if m.BaseRevision != revision {
		return nil, &ProgressConflict{"progress_conflict", current}
	}
	if err := validate(); err != nil {
		return nil, err
	}
	p := &catalog.Progress{Position: m.Position, Revision: revision + 1, UpdatedAt: time.Now().UnixMilli()}
	anchor, _ := json.Marshal(m.Anchor)
	if err := q.SaveProgress(ctx, dbgen.SaveProgressParams{ID: rand.Text(), ReaderID: readerID, BookID: bookID, ChapterIdx: p.ChapterIdx, Percent: p.Percent, UpdatedAt: p.UpdatedAt, ContentVersion: sql.NullString{String: p.ContentVersion, Valid: true}, Anchor: sql.NullString{String: string(anchor), Valid: true}, Revision: p.Revision}); err != nil {
		return nil, err
	}
	if err := q.SaveSequence(ctx, dbgen.SaveSequenceParams{ReaderID: readerID, BookID: bookID, DeviceID: m.DeviceID, LastSeq: m.ClientSeq}); err != nil {
		return nil, err
	}
	response, _ := json.Marshal(p)
	if err := q.SaveReceipt(ctx, dbgen.SaveReceiptParams{ReaderID: readerID, BookID: bookID, MutationID: m.MutationID, RequestHash: hash, Revision: p.Revision, Response: string(response)}); err != nil {
		return nil, err
	}
	if err := q.PruneReceipts(ctx, dbgen.PruneReceiptsParams{ReaderID: readerID, BookID: bookID, Revision: p.Revision - 128}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}
