package management

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/EthanHannn/personal-library/internal/reader"
)

type Chapter struct {
	Index int    `json:"idx"`
	Title string `json:"title"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}
type Source struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	URL         string `json:"url"`
	Complete    bool   `json:"complete"`
	Total       int    `json:"total"`
	Downloaded  int    `json:"downloaded"`
	SkippedPaid int    `json:"skippedPaid"`
}
type Preview struct {
	Metadata
	File         reader.File `json:"file"`
	Words        int64       `json:"words"`
	ChapterCount int         `json:"chapterCount"`
	Chapters     []Chapter   `json:"chapters"`
	Warnings     []string    `json:"warnings"`
	BookID       int64       `json:"bookId"`
	BookVersion  int64       `json:"bookVersion"`
	Source       *Source     `json:"source,omitempty"`
}

var heading = regexp.MustCompile(`^(第[零〇一二三四五六七八九十百千万两0-9]+[章回节卷部集]|[Cc][Hh][Aa][Pp][Tt][Ee][Rr]\s+[0-9]+|序章|楔子|序言|引子|尾声|后记)(\s|[：:、.．]|$|\p{Han})`)

func (m *Manager) preview(ctx context.Context, j Job, in Input, known ...[]Chapter) (Preview, error) {
	p := Preview{Metadata: in.Metadata, BookID: in.BookID, Warnings: []string{}, Chapters: []Chapter{}}
	ref, err := m.Reader.Inspect(in.Path, in.Encoding)
	if err != nil {
		return p, fmt.Errorf("无法读取 TXT 文件")
	}
	if ref.Size == 0 || ref.Size > 512<<20 {
		return p, fmt.Errorf("TXT 必须为 1 字节至 512 MiB")
	}
	f, err := m.Reader.OpenSource(ref.Path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	hash := sha256.New()
	lines := bufio.NewReaderSize(io.TeeReader(f, hash), 1<<20)
	chapters := make([]Chapter, 0)
	offset := int64(0)
	for {
		if ctx.Err() != nil {
			return p, ctx.Err()
		}
		raw, readErr := lines.ReadSlice('\n')
		if readErr != nil && readErr != io.EOF {
			return p, fmt.Errorf("单行超过 1 MiB 或文件读取失败，请先整理 TXT")
		}
		if len(raw) > 0 {
			text, err := reader.DecodeLine(raw, in.Encoding)
			if err != nil {
				return p, fmt.Errorf("文件编码不符，请选择正确编码后重新预览")
			}
			if strings.ContainsRune(text, 0) {
				return p, fmt.Errorf("TXT 中含有二进制空字节，请先检查文件编码")
			}
			for _, ch := range text {
				if !unicode.IsSpace(ch) {
					p.Words++
				}
			}
			title := strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
			if len(known) == 0 && len([]rune(title)) <= 120 && heading.MatchString(title) {
				if len(chapters) == 0 && offset > 0 {
					chapters = append(chapters, Chapter{Index: 1, Title: "卷首", Start: 0, End: offset})
				}
				if len(chapters) > 0 {
					chapters[len(chapters)-1].End = offset
				}
				chapters = append(chapters, Chapter{Index: len(chapters) + 1, Title: title, Start: offset})
				if len(chapters) > 20000 {
					return p, fmt.Errorf("章节超过 20000，请分卷后导入")
				}
			}
			offset += int64(len(raw))
		}
		if readErr == io.EOF {
			break
		}
	}
	observed, err := m.Reader.Inspect(ref.Path, ref.Encoding)
	if err != nil || observed != ref || offset != ref.Size {
		return p, ErrConflict
	}
	ref.SHA256 = hex.EncodeToString(hash.Sum(nil))
	p.File = ref
	if len(known) > 0 {
		chapters = known[0]
		end := int64(0)
		for i, c := range chapters {
			if c.Index != i+1 || c.Start != end || c.End <= c.Start {
				return p, ErrConflict
			}
			end = c.End
		}
		if end != ref.Size {
			return p, ErrConflict
		}
	}
	if len(chapters) == 0 {
		chapters = append(chapters, Chapter{Index: 1, Title: "正文", Start: 0})
		p.Warnings = append(p.Warnings, "未识别到章节标题，按一章收录；确认前请核对文件。")
	}
	chapters[len(chapters)-1].End = offset
	p.ChapterCount = len(chapters)
	p.Chapters = chapters[:min(12, len(chapters))]
	if p.BookID > 0 {
		var has int
		if err := m.DB.Read.QueryRowContext(ctx, `SELECT has_content,updated_at FROM books WHERE id=?`, p.BookID).Scan(&has, &p.BookVersion); err != nil {
			return p, err
		}
		if has != 0 {
			return p, fmt.Errorf("这本书已有正文，当前导入仅补充缺失正文，避免覆盖阅读进度")
		}
	}
	var duplicates int
	if err := m.DB.Read.QueryRowContext(ctx, `SELECT count(*) FROM books WHERE title=? AND author=? AND id<>?`, p.Title, p.Author, p.BookID).Scan(&duplicates); err != nil {
		return p, err
	}
	if duplicates > 0 {
		p.Warnings = append(p.Warnings, "书库已有同名同作者作品；请从管理列表选择该作品补充正文，避免重复入库。")
	}
	if err := m.DB.Read.QueryRowContext(ctx, `SELECT count(*) FROM files WHERE sha256=? OR path=?`, ref.SHA256, ref.Path).Scan(&duplicates); err != nil {
		return p, err
	}
	if duplicates > 0 {
		p.Warnings = append(p.Warnings, "此文件或相同内容已登记，确认时会拒绝重复导入。")
	}
	tx, err := m.DB.Write.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM job_chapters WHERE job_id=?`, j.ID); err != nil {
		return p, err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO job_chapters VALUES(?,?,?,?,?)`)
	if err != nil {
		return p, err
	}
	defer stmt.Close()
	for _, c := range chapters {
		if _, err = stmt.ExecContext(ctx, j.ID, c.Index, c.Title, c.Start, c.End); err != nil {
			return p, err
		}
	}
	return p, tx.Commit()
}

func (m *Manager) confirm(ctx context.Context, j Job) (Preview, error) {
	var p Preview
	if err := json.Unmarshal(j.Result, &p); err != nil {
		return p, err
	}
	if p.ChapterCount < 1 || !ValidMetadata(p.Metadata) {
		return p, ErrConflict
	}
	ref := p.File
	ref.SHA256 = ""
	sum, err := m.Reader.Fingerprint(ctx, ref)
	if err != nil || sum != p.File.SHA256 {
		return p, ErrConflict
	}
	tx, err := m.DB.Write.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var duplicate int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM files WHERE sha256=? OR path=?)+(SELECT count(*) FROM books WHERE title=? AND author=? AND id<>?)`, sum, p.File.Path, p.Title, p.Author, p.BookID).Scan(&duplicate); err != nil {
		return p, err
	}
	if duplicate > 0 {
		return p, fmt.Errorf("相同作品或文件已经存在，请从管理列表选择缺正文的作品补充")
	}
	now := time.Now().UnixMilli()
	if p.BookID == 0 {
		r, err := tx.ExecContext(ctx, `INSERT INTO books(title,author,tag1,tag2,size,intro,status,chapter_count,word_count,has_content,created_at,updated_at) VALUES(?,?,?,?,'',?,'APPROVED',?,?,1,?,?)`, p.Title, p.Author, p.Tag1, p.Tag2, p.Intro, p.ChapterCount, p.Words, now, now)
		if err != nil {
			return p, err
		}
		p.BookID, _ = r.LastInsertId()
		if _, err = tx.ExecContext(ctx, `INSERT INTO rating_snapshots VALUES(?,0,0,0,0,0,0,0,0)`, p.BookID); err != nil {
			return p, err
		}
	} else {
		r, err := tx.ExecContext(ctx, `UPDATE books SET title=?,author=?,tag1=?,tag2=?,intro=?,status='APPROVED',chapter_count=?,word_count=?,has_content=1,updated_at=? WHERE id=? AND has_content=0 AND updated_at=?`, p.Title, p.Author, p.Tag1, p.Tag2, p.Intro, p.ChapterCount, p.Words, max(now, p.BookVersion+1), p.BookID, p.BookVersion)
		if err != nil {
			return p, err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return p, ErrConflict
		}
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO files(path,encoding,size,modified_ns,sha256) VALUES(?,?,?,?,?)`, p.File.Path, p.File.Encoding, p.File.Size, p.File.ModifiedNS, sum)
	if err != nil {
		return p, err
	}
	fileID, _ := r.LastInsertId()
	if _, err = tx.ExecContext(ctx, `INSERT INTO book_contents VALUES(?,1,?,1)`, p.BookID, fileID); err != nil {
		return p, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chapters(book_id,content_version,idx,title,start_offset,end_offset) SELECT ?,1,idx,title,start_offset,end_offset FROM job_chapters WHERE job_id=? ORDER BY idx`, p.BookID, j.ID); err != nil {
		return p, err
	}
	if p.Source != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO book_sources VALUES(?,?,?,?,?)`, p.BookID, p.Source.Name, p.Source.ID, p.Source.URL, p.Source.Complete); err != nil {
			return p, ErrConflict
		}
	}
	// Publication and its receipt are atomic: a crash cannot publish twice.
	data, _ := json.Marshal(p)
	receipt, err := tx.ExecContext(ctx, `UPDATE library_jobs SET status='completed',result=?,message='',updated_at=? WHERE id=? AND status='running'`, string(data), now, j.ID)
	if err != nil {
		return p, err
	}
	if n, _ := receipt.RowsAffected(); n != 1 {
		return p, ErrConflict
	}
	return p, tx.Commit()
}

func (m *Manager) scan(ctx context.Context) (any, error) {
	type issue struct {
		BookID  int64  `json:"bookId"`
		Path    string `json:"path"`
		Message string `json:"message"`
	}
	report := struct {
		Checked    int      `json:"checked"`
		Missing    int      `json:"missing"`
		Changed    int      `json:"changed"`
		Candidates []string `json:"candidates"`
		Issues     []issue  `json:"issues"`
		Truncated  bool     `json:"truncated"`
	}{Candidates: []string{}, Issues: []issue{}}
	rows, err := m.DB.Read.QueryContext(ctx, `SELECT bc.book_id,f.path,f.encoding,f.size,f.modified_ns FROM files f JOIN book_contents bc ON bc.file_id=f.id AND bc.is_current=1`)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for rows.Next() {
		var id int64
		var f reader.File
		if err := rows.Scan(&id, &f.Path, &f.Encoding, &f.Size, &f.ModifiedNS); err != nil {
			rows.Close()
			return nil, err
		}
		known[f.Path] = true
		report.Checked++
		actual, e := m.Reader.Inspect(f.Path, f.Encoding)
		message := ""
		if e != nil {
			report.Missing++
			message = "文件缺失或不可读"
		} else if f != actual {
			report.Changed++
			message = "文件已变化，现有索引需要重建"
		}
		if message != "" && len(report.Issues) < 200 {
			report.Issues = append(report.Issues, issue{id, f.Path, message})
		}
		if ctx.Err() != nil {
			rows.Close()
			return nil, ctx.Err()
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(m.Novels)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	visited := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > 50000 {
			report.Truncated = true
			return fs.SkipAll
		}
		if !d.Type().IsRegular() || known[path] || !strings.EqualFold(filepath.Ext(path), ".txt") {
			return nil
		}
		if len(report.Candidates) < 200 {
			report.Candidates = append(report.Candidates, path)
		} else {
			report.Truncated = true
		}
		return nil
	})
	if errors.Is(err, fs.ErrPermission) {
		return report, fmt.Errorf("部分目录不可读，请检查小说目录权限")
	}
	return report, err
}
