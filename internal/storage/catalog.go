package storage

import (
	"context"
	"database/sql"

	"github.com/EthanHannn/personal-library/internal/storage/dbgen"
)

type CatalogParams struct {
	Search, Author, Tag, Tag1, Tag2, SortBy string
	MinScore, MaxScore                      float64
	MinWords, MaxWords, OnlyContent         int64
	Page, Limit                             int64
}

type CatalogResult struct {
	Books            []dbgen.CatalogPageRow
	Stats            dbgen.CatalogStatsRow
	Page, TotalPages int64
}

func (s *Repository) Catalog(ctx context.Context, p CatalogParams) (CatalogResult, error) {
	var result CatalogResult
	// Count and page share a read snapshot, including during future imports or
	// rating updates. Neither query visits chapter rows.
	tx, err := s.DB.Read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	q := s.Queries.WithTx(tx)
	result.Stats, err = q.CatalogStats(ctx, dbgen.CatalogStatsParams{Search: p.Search, Author: p.Author, Tag: p.Tag, Tag1: p.Tag1, Tag2: p.Tag2, MinScore: p.MinScore, MaxScore: p.MaxScore, MinWords: p.MinWords, MaxWords: p.MaxWords, OnlyContent: p.OnlyContent})
	if err != nil {
		return result, err
	}
	result.TotalPages = (result.Stats.Total + p.Limit - 1) / p.Limit
	result.Page = min(p.Page, max(1, result.TotalPages))
	result.Books, err = q.CatalogPage(ctx, dbgen.CatalogPageParams{Search: p.Search, Author: p.Author, Tag: p.Tag, Tag1: p.Tag1, Tag2: p.Tag2, MinScore: p.MinScore, MaxScore: p.MaxScore, MinWords: p.MinWords, MaxWords: p.MaxWords, OnlyContent: p.OnlyContent, SortBy: p.SortBy, PageLimit: p.Limit, PageOffset: (result.Page - 1) * p.Limit})
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
