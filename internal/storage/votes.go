package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type VoteType string

func (v VoteType) Valid() bool {
	switch v {
	case "XIANCAO", "LIANGCAO", "GANCAO", "KUCAO", "DUCAO":
		return true
	}
	return false
}

type VoteCounts struct {
	Xiancao  int64 `json:"XIANCAO"`
	Liangcao int64 `json:"LIANGCAO"`
	Gancao   int64 `json:"GANCAO"`
	Kucao    int64 `json:"KUCAO"`
	Ducao    int64 `json:"DUCAO"`
}

type VoteTotals struct {
	Score      float64    `json:"score"`
	Popularity int64      `json:"popularity"`
	TotalScore int64      `json:"totalScore"`
	Counts     VoteCounts `json:"counts"`
}

type VoteSummary struct {
	BookID  int64      `json:"bookId"`
	MyVote  *VoteType  `json:"myVote"`
	Source  VoteTotals `json:"source"`
	Current VoteTotals `json:"current"`
}

// VoteSummary reads the source, current choice and deltas from one snapshot.
// Source statistics remain immutable, including any historical rounding or
// mismatch between source popularity and the sum of its five counters.
func (s *Repository) VoteSummary(ctx context.Context, readerID string, bookID int64) (VoteSummary, error) {
	tx, err := s.DB.Read.BeginTx(ctx, nil)
	if err != nil {
		return VoteSummary{}, err
	}
	defer tx.Rollback()
	return voteSummary(ctx, s.Queries.WithTx(tx), readerID, bookID)
}

func voteSummary(ctx context.Context, q *dbgen.Queries, readerID string, bookID int64) (VoteSummary, error) {
	base, err := q.VoteSnapshot(ctx, dbgen.VoteSnapshotParams{ReaderID: readerID, BookID: bookID})
	if err != nil {
		return VoteSummary{}, err
	}
	delta, err := q.VoteDeltas(ctx, bookID)
	if err != nil {
		return VoteSummary{}, err
	}
	result := VoteSummary{BookID: bookID, Source: VoteTotals{
		Score: base.Score, Popularity: base.Popularity, TotalScore: base.TotalScore,
		Counts: VoteCounts{base.XiancaoCount, base.LiangcaoCount, base.GancaoCount, base.KucaoCount, base.DucaoCount},
	}}
	if base.MyVote.Valid {
		choice := VoteType(base.MyVote.String)
		result.MyVote = &choice
	}
	counts := VoteCounts{base.XiancaoCount + delta.Xiancao, base.LiangcaoCount + delta.Liangcao,
		base.GancaoCount + delta.Gancao, base.KucaoCount + delta.Kucao, base.DucaoCount + delta.Ducao}
	if delta.Invalid != 0 || counts.Xiancao < 0 || counts.Liangcao < 0 || counts.Gancao < 0 || counts.Kucao < 0 || counts.Ducao < 0 {
		return VoteSummary{}, errors.New("invalid historical vote baseline")
	}
	// Same weights as the old site. Use current minus imported choices exactly
	// once: imported votes already belong to the historical counters.
	result.Current.Counts = counts
	result.Current.Popularity = counts.Xiancao + counts.Liangcao + counts.Gancao + counts.Kucao + counts.Ducao
	result.Current.TotalScore = counts.Xiancao*10 + counts.Liangcao*7 + counts.Gancao*5 + counts.Kucao*3 + counts.Ducao
	if result.Current.Popularity > 0 {
		result.Current.Score = float64(result.Current.TotalScore) / float64(result.Current.Popularity)
	}
	return result, nil
}

// SetVote expresses the desired choice, never a toggle or increment. The
// unique reader/book key and write transaction serialize concurrent changes;
// an upsert retains imported_type, the original ID and its creation time.
func (s *Repository) SetVote(ctx context.Context, readerID string, bookID int64, choice VoteType) (VoteSummary, error) {
	if !choice.Valid() {
		return VoteSummary{}, errors.New("invalid vote type")
	}
	tx, err := s.DB.Write.BeginTx(ctx, nil)
	if err != nil {
		return VoteSummary{}, err
	}
	defer tx.Rollback()
	q := s.Queries.WithTx(tx)
	if _, err := q.BookDetail(ctx, bookID); err != nil {
		return VoteSummary{}, err
	}
	if err := q.SetVote(ctx, dbgen.SetVoteParams{ID: rand.Text(), Type: string(choice), ReaderID: readerID, BookID: bookID, CreatedAt: time.Now().UTC().UnixMilli()}); err != nil {
		return VoteSummary{}, err
	}
	result, err := voteSummary(ctx, q, readerID, bookID)
	if err != nil {
		return VoteSummary{}, err
	}
	return result, tx.Commit()
}
