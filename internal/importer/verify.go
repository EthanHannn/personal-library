package importer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EthanHannn/personal-library/internal/legacy"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
)

func verify(ctx context.Context, source *sql.Tx, target *storage.Database, r *reader.Reader, o Options, report *Report) error {
	for _, m := range mappings {
		rows, err := source.QueryContext(ctx, "SELECT "+m.fields+" FROM "+m.source+" WHERE "+m.predicate+" ORDER BY id")
		if err != nil {
			return err
		}
		count, hash, err := digest(rows, m.normalize)
		if err != nil {
			return err
		}
		dest, err := target.Read.QueryContext(ctx, "SELECT "+m.targetFields+" FROM "+m.target+" ORDER BY 1")
		if err != nil {
			return err
		}
		targetCount, targetHash, err := digest(dest, nil)
		if err != nil {
			return err
		}
		report.Tables = append(report.Tables, Comparison{m.name, count, targetCount, hash, targetHash})
		if count != targetCount || hash != targetHash {
			return fmt.Errorf("%s normalized field reconciliation failed", m.name)
		}
	}
	// Validate the whole source index using the already-tested bounded scanner.
	s, err := legacy.Open(ctx, o.Source)
	if err != nil {
		return err
	}
	defer s.Close()
	files := make(map[string]reader.File)
	for _, f := range report.Files {
		files[f.Path] = f.File
	}
	report.Legacy, err = s.Verify(ctx, func(path string) (int64, error) {
		f, ok := files[strings.ReplaceAll(path, "\\", "/")]
		if !ok {
			return 0, fmt.Errorf("missing manifest entry")
		}
		observed, err := r.Inspect(f.Path, f.Encoding)
		if err != nil {
			return 0, err
		}
		if f != observed {
			return 0, reader.ErrChanged
		}
		return f.Size, nil
	})
	if err != nil {
		return err
	}
	if err := report.Legacy.Error(); err != nil {
		return err
	}
	checks := []struct{ name, query string }{
		{"foreign keys", `SELECT count(*) FROM pragma_foreign_key_check`},
		{"content versions", `SELECT count(*) FROM book_contents WHERE version<>1 OR is_current<>1`},
		{"chapter versions", `SELECT count(*) FROM chapters WHERE content_version<>1`},
		{"vote baselines", `SELECT count(*) FROM votes WHERE imported_type IS NOT type`},
		{"progress references", `SELECT count(*) FROM reading_progress p WHERE NOT EXISTS (SELECT 1 FROM chapters c WHERE c.book_id=p.book_id AND c.content_version=1 AND c.idx=p.chapter_idx)`},
		{"cross-book replies", `SELECT count(*) FROM comments c JOIN comments p ON c.parent_id=p.id WHERE c.book_id<>p.book_id`},
		{"reply cycles", `WITH RECURSIVE ancestry(id,parent) AS (SELECT id,parent_id FROM comments WHERE parent_id IS NOT NULL UNION SELECT a.id,c.parent_id FROM ancestry a JOIN comments c ON c.id=a.parent WHERE c.parent_id IS NOT NULL) SELECT count(*) FROM ancestry WHERE id=parent`},
	}
	for _, check := range checks {
		var n int64
		if err := target.Read.QueryRowContext(ctx, check.query).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("%s: %d invalid records (no rows discarded)", check.name, n)
		}
	}
	if err := target.Read.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&report.Integrity); err != nil {
		return err
	}
	if report.Integrity != "ok" {
		return fmt.Errorf("target integrity check failed")
	}
	return nil
}

// One row at a time, with canonical types and JSON framing. Counts alone cannot
// detect wrong IDs, lost Unicode, rounded percentages, or changed relationships.
func digest(rows *sql.Rows, normalize func([]any) error) (int64, string, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return 0, "", err
	}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	var count int64
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, "", err
		}
		if normalize != nil {
			if err := normalize(values); err != nil {
				return 0, "", err
			}
		}
		if err := encoder.Encode(values); err != nil {
			return 0, "", err
		}
		count++
	}
	return count, hex.EncodeToString(hash.Sum(nil)), rows.Err()
}
