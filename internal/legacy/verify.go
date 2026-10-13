package legacy

import (
	"context"
	"database/sql"
	"fmt"
)

type Problem struct {
	Code    string `json:"code"`
	Book    int64  `json:"book,omitempty"`
	Chapter int64  `json:"chapter,omitempty"`
}

type Verification struct {
	Complete      bool      `json:"complete"`
	Books         int64     `json:"books"`
	ReadableBooks int64     `json:"readableBooks"`
	Chapters      int64     `json:"chapters"`
	Problems      int64     `json:"problemCount"`
	Examples      []Problem `json:"examples"`
	QuickCheck    string    `json:"quickCheck"`
}

// Verify scans chapters using a bounded cursor. fileSize validates the captured
// file version and returns its size; no source tables or files are changed.
// Examples are capped, while the total problem count remains exact.
func (s *Store) Verify(ctx context.Context, fileSize func(string) (int64, error)) (report Verification, err error) {
	add := func(code string, book, chapter int64) {
		report.Problems++
		if len(report.Examples) < 100 {
			report.Examples = append(report.Examples, Problem{code, book, chapter})
		}
	}
	if err = s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&report.QuickCheck); err != nil {
		return report, err
	}
	if report.QuickCheck != "ok" {
		add("sqlite_quick_check", 0, 0)
	}
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM books").Scan(&report.Books); err != nil {
		return report, err
	}
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM books WHERE hasContent=1").Scan(&report.ReadableBooks); err != nil {
		return report, err
	}
	// Starting with chapters retains orphan rows. Index order keeps memory
	// independent of chapter count, including multiple books sharing one file.
	rows, err := s.db.QueryContext(ctx, `SELECT c.bookId,c.idx,c.startOffset,c.endOffset,b.id,b.filePath,b.chapterCount,b.hasContent
FROM chapters c LEFT JOIN books b ON b.id=c.bookId ORDER BY c.bookId,c.idx`)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	var current, actual, expected, previousIdx, previousEnd, size int64
	var seen, validFile bool
	finish := func() {
		if seen && actual != expected {
			add("chapter_count_mismatch", current, 0)
		}
	}
	for rows.Next() {
		var book, idx, start, end int64
		var bookID, count, hasContent sql.NullInt64
		var path sql.NullString
		if err := rows.Scan(&book, &idx, &start, &end, &bookID, &path, &count, &hasContent); err != nil {
			return report, err
		}
		report.Chapters++
		if !seen || book != current {
			finish()
			current, actual, expected, previousIdx, previousEnd = book, 0, count.Int64, 0, 0
			seen, validFile = true, false
			if !bookID.Valid {
				add("orphan_chapter", book, idx)
			} else if hasContent.Int64 != 1 {
				add("content_flag_mismatch", book, 0)
			}
			if !path.Valid || path.String == "" {
				add("missing_file_reference", book, 0)
			} else {
				size, err = fileSize(path.String)
				if err != nil {
					add("unverified_file", book, 0)
				} else {
					validFile = true
				}
			}
		}
		actual++
		if idx != previousIdx+1 {
			add("chapter_sequence", book, idx)
		}
		if start < 0 || end <= start || (validFile && end > size) {
			add("invalid_offset", book, idx)
		}
		if actual > 1 && start < previousEnd {
			add("overlapping_chapters", book, idx)
		}
		previousIdx, previousEnd = idx, end
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	rows.Close()
	finish()
	missing, err := s.db.QueryContext(ctx, `SELECT b.id FROM books b WHERE b.hasContent=1 AND NOT EXISTS (SELECT 1 FROM chapters c WHERE c.bookId=b.id)`)
	if err != nil {
		return report, err
	}
	defer missing.Close()
	for missing.Next() {
		var id int64
		if err := missing.Scan(&id); err != nil {
			return report, err
		}
		add("missing_chapters", id, 0)
	}
	if err := missing.Err(); err != nil {
		return report, err
	}
	report.Complete = true
	return report, nil
}

func (r Verification) Error() error {
	if !r.Complete {
		return fmt.Errorf("verification did not finish")
	}
	if r.Problems != 0 {
		return fmt.Errorf("verification found %d problems", r.Problems)
	}
	return nil
}
