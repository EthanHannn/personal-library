package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type SearchCandidate struct {
	Metadata
	SourceID      string `json:"sourceId"`
	SourceName    string `json:"sourceName"`
	SourceVersion int    `json:"sourceVersion"`
	URL           string `json:"url"`
	Latest        string `json:"latest"`
	NeedsReview   bool   `json:"needsReview"`
}
type SearchSourceResult struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
}
type SearchResult struct {
	Candidates []SearchCandidate    `json:"searchCandidates"`
	Sources    []SearchSourceResult `json:"searchSources"`
}

func (m *Manager) sourceProgress(ctx context.Context, j Job, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = m.DB.Write.ExecContext(ctx, `UPDATE library_jobs SET result=? WHERE id=? AND status='running'`, string(raw), j.ID)
	return err
}
func (m *Manager) searchOne(ctx context.Context, s SourceSnapshot, query, author string) ([]SearchCandidate, error) {
	rule := s.Config.Search
	if rule == nil {
		return nil, fmt.Errorf("该书源不支持搜索")
	}
	encoded := query
	if s.Config.Charset == "gb18030" {
		raw, err := simplifiedchinese.GB18030.NewEncoder().String(query)
		if err != nil {
			return nil, fmt.Errorf("搜索词无法按 GB18030 编码")
		}
		encoded = raw
	}
	raw, err := sourceURL(s.Config, s.Config.BaseURL, strings.ReplaceAll(rule.URL, "{query}", url.QueryEscape(encoded)))
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	for k, v := range rule.Form {
		form.Set(k, strings.ReplaceAll(v, "{query}", encoded))
	}
	doc, _, err := m.sourcePage(ctx, s, raw, form)
	if err != nil {
		return nil, err
	}
	items, err := doc.items(rule.Items)
	if err != nil {
		return nil, err
	}
	out := []SearchCandidate{}
	seen := map[string]bool{}
	for _, item := range items {
		c := SearchCandidate{Metadata: Metadata{Title: item.text(rule.Title, false), Author: item.text(rule.Author, false), Tag1: item.text(rule.Category, false)}, SourceID: s.ID, SourceName: s.Config.Name, SourceVersion: s.Version, Latest: item.text(rule.Latest, false)}
		c.URL, err = sourceURL(s.Config, doc.base, item.text(rule.Link, false))
		if err != nil {
			return nil, fmt.Errorf("搜索结果链接：%w", err)
		}
		if !ValidMetadata(c.Metadata) {
			return nil, fmt.Errorf("搜索结果缺少有效书名或元数据超限")
		}
		if seen[c.URL] {
			continue
		}
		seen[c.URL] = true
		c.NeedsReview = c.Author == "" || (author != "" && sourceIdentity(author) != sourceIdentity(c.Author))
		out = append(out, c)
		if len(out) >= 20 {
			break
		}
	}
	return out, nil
}
func (m *Manager) searchSources(ctx context.Context, j Job) (SearchResult, error) {
	out := SearchResult{Candidates: []SearchCandidate{}, Sources: []SearchSourceResult{}}
	for _, s := range j.Input.SourceSnapshots {
		current, err := m.Source(ctx, s.ID)
		if err == nil && !current.Enabled {
			err = fmt.Errorf("书源已停用")
		}
		var candidates []SearchCandidate
		if err == nil {
			one, cancel := context.WithTimeout(ctx, 45*time.Second)
			candidates, err = m.searchOne(one, s, j.Input.Query, j.Input.Author)
			cancel()
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		report := SearchSourceResult{ID: s.ID, Name: s.Config.Name, Count: len(candidates)}
		if err != nil {
			report.Error = err.Error()
		} else {
			out.Candidates = append(out.Candidates, candidates...)
		}
		out.Sources = append(out.Sources, report)
		if err := m.sourceProgress(ctx, j, out); err != nil {
			return out, err
		}
	}
	return out, nil
}

type SourceCheck struct {
	SourceID    string `json:"sourceId"`
	Version     int    `json:"version"`
	Passed      bool   `json:"passed"`
	Stage       string `json:"stage"`
	Error       string `json:"error,omitempty"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Chapters    int    `json:"chapters"`
	SampleTitle string `json:"sampleTitle"`
	SampleWords int    `json:"sampleWords"`
	SearchCount *int   `json:"searchCount,omitempty"`
	CheckedAt   int64  `json:"checkedAt"`
}

func (m *Manager) checkSource(ctx context.Context, j Job) (any, error) {
	s := j.Input.SourceSnapshots[0]
	report := SourceCheck{SourceID: s.ID, Version: s.Version, Stage: "详情", CheckedAt: time.Now().UnixMilli()}
	check := func() error {
		if j.Input.Query != "" && s.Config.Search != nil {
			report.Stage = "搜索"
			list, err := m.searchOne(ctx, s, j.Input.Query, "")
			if err != nil {
				return err
			}
			n := len(list)
			report.SearchCount = &n
			if n == 0 {
				return fmt.Errorf("样本搜索没有结果，请核对搜索规则或关键词")
			}
		}
		report.Stage = "详情"
		book, err := m.fetchSourceBook(ctx, s, j.Input.URL)
		if err != nil {
			return err
		}
		report.Title = book.Title
		report.Author = book.Author
		report.Stage = "目录"
		chapters, _, err := m.fetchSourceTOC(ctx, s, book.TOC)
		if err != nil {
			return err
		}
		report.Chapters = len(chapters)
		if len(chapters) == 0 {
			return fmt.Errorf("目录没有章节")
		}
		catalog := map[string]bool{}
		for _, c := range chapters {
			catalog[c.URL] = true
		}
		report.Stage = "正文"
		body, _, err := m.fetchSourceChapter(ctx, s, chapters[0], catalog)
		if err != nil {
			return err
		}
		report.SampleTitle = chapters[0].Title
		report.SampleWords = len([]rune(body))
		report.Passed = true
		report.Stage = "完成"
		return nil
	}
	if err := check(); err != nil {
		report.Error = err.Error()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	raw, _ := json.Marshal(report)
	// An older in-flight check must not certify newly edited rules.
	_, err := m.DB.Write.ExecContext(ctx, `UPDATE source_configs SET checked_at=?,check_version=?,check_result=? WHERE id=? AND version=?`, report.CheckedAt, s.Version, string(raw), s.ID, s.Version)
	return map[string]any{"sourceCheck": report}, err
}

type sourceCapture struct {
	Metadata
	RulesHash   string          `json:"rulesHash"`
	CatalogHash string          `json:"catalogHash"`
	Source      Source          `json:"source"`
	Chapters    []sourceChapter `json:"chapters"`
	Warnings    []string        `json:"warnings"`
}

func (m *Manager) collect(ctx context.Context, j Job) (Preview, error) {
	// Legacy v3 jobs have no snapshots and remain resumable after migration.
	if j.Input.SourceID == "" || j.Input.SourceID == "qidian" {
		return m.collectQidian(ctx, j)
	}
	return m.collectSource(ctx, j)
}
func (m *Manager) collectSource(ctx context.Context, j Job) (Preview, error) {
	var p Preview
	if len(j.Input.SourceSnapshots) != 1 {
		return p, fmt.Errorf("任务缺少书源规则，请重新创建")
	}
	s := j.Input.SourceSnapshots[0]
	current, err := m.Source(ctx, s.ID)
	if err != nil {
		return p, err
	}
	if !current.Enabled {
		return p, fmt.Errorf("书源已停用，启用后可重试")
	}
	book, err := m.fetchSourceBook(ctx, s, j.Input.URL)
	if err != nil {
		return p, err
	}
	if j.Input.Title != "" && sourceIdentity(j.Input.Title) != sourceIdentity(book.Title) || j.Input.Author != "" && sourceIdentity(j.Input.Author) != sourceIdentity(book.Author) {
		return p, fmt.Errorf("作品页书名或作者与所选搜索结果不符，请检查书源规则")
	}
	catalog, duplicates, err := m.fetchSourceTOC(ctx, s, book.TOC)
	if err != nil {
		return p, err
	}
	if len(catalog) == 0 {
		return p, fmt.Errorf("目录没有章节")
	}
	catalogBytes, _ := json.Marshal(catalog)
	configBytes, _ := json.Marshal(s)
	state := sourceCapture{Metadata: book.Metadata, RulesHash: hashBytes(configBytes), CatalogHash: hashBytes(catalogBytes), Source: Source{Name: s.ID, Label: s.Config.Name, ID: book.URL, URL: book.URL, Total: len(catalog), RuleVersion: s.Version, WorkStatus: book.Status}, Chapters: catalog[:min(len(catalog), j.Input.MaxChapters)], Warnings: []string{}}
	if duplicates > 0 {
		state.Warnings = append(state.Warnings, fmt.Sprintf("目录中 %d 个重复链接已去重，保留首次顺序。", duplicates))
	}
	stateName := j.ID + ".source.json"
	if raw, e := m.Collected.ReadFile(stateName); e == nil {
		var saved sourceCapture
		if json.Unmarshal(raw, &saved) != nil || saved.RulesHash != state.RulesHash || saved.CatalogHash != state.CatalogHash || saved.Metadata != state.Metadata || len(saved.Chapters) != len(state.Chapters) {
			return p, fmt.Errorf("来源目录、作品信息或检查点已变化，请新建采集任务；已有正文未修改")
		}
		state = saved
	} else if !os.IsNotExist(e) {
		return p, e
	}
	save := func() error { raw, _ := json.Marshal(state); return writeAtomic(m.Collected, stateName, raw) }
	if err := save(); err != nil {
		return p, err
	}
	known := map[string]bool{}
	for _, c := range catalog {
		known[c.URL] = true
	}
	hashes := map[string]int{}
	warnings := append([]string{}, state.Warnings...)
	totalBytes := int64(0)
	for i := range state.Chapters {
		if ctx.Err() != nil {
			return p, ctx.Err()
		}
		current, err = m.Source(ctx, s.ID)
		if err != nil {
			return p, err
		}
		if !current.Enabled {
			return p, fmt.Errorf("书源已停用，保留已采集章节")
		}
		c := &state.Chapters[i]
		name := fmt.Sprintf("%s.source.%d.part", j.ID, i)
		raw, readErr := m.Collected.ReadFile(name)
		if readErr != nil || c.FileHash == "" || hashBytes(raw) != c.FileHash {
			text, rawHash, e := m.fetchSourceChapter(ctx, s, *c, known)
			if e != nil {
				return p, fmt.Errorf("第 %d 章「%s」：%w", i+1, c.Title, e)
			}
			raw = []byte(c.Title + "\n" + text + "\n\n")
			if err := writeAtomic(m.Collected, name, raw); err != nil {
				return p, err
			}
			c.RawHash = rawHash
			c.TextHash = hashBytes([]byte(text))
			c.FileHash = hashBytes(raw)
			c.CollectedAt = time.Now().UnixMilli()
			c.Warnings = nil
			if len([]rune(text)) < 80 {
				c.Warnings = append(c.Warnings, fmt.Sprintf("第 %d 章正文较短（%d 字），请核对。", i+1, len([]rune(text))))
			}
			if err := save(); err != nil {
				return p, err
			}
		}
		totalBytes += int64(len(raw))
		if totalBytes > 512<<20 {
			return p, fmt.Errorf("采集正文超过 512 MiB，请减少本次章节数")
		}
		if previous, ok := hashes[c.TextHash]; ok {
			warnings = append(warnings, fmt.Sprintf("第 %d 章与第 %d 章正文完全重复，请核对来源。", i+1, previous))
		} else {
			hashes[c.TextHash] = i + 1
		}
		warnings = append(warnings, c.Warnings...)
		if len(warnings) > 100 {
			warnings = warnings[:100]
		}
		if err := m.sourceProgress(ctx, j, map[string]any{"downloaded": i + 1, "planned": len(state.Chapters), "source": state.Source}); err != nil {
			return p, err
		}
	}
	name := j.ID + ".txt"
	out, err := m.Collected.OpenFile(name+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return p, err
	}
	defer out.Close()
	chapters := []Chapter{}
	offset := int64(0)
	for i, c := range state.Chapters {
		f, e := m.Collected.Open(fmt.Sprintf("%s.source.%d.part", j.ID, i))
		if e != nil {
			return p, e
		}
		n, e := io.Copy(out, f)
		f.Close()
		if e != nil {
			return p, e
		}
		chapters = append(chapters, Chapter{Index: i + 1, Title: c.Title, Start: offset, End: offset + n})
		offset += n
	}
	if err = out.Sync(); err != nil {
		return p, err
	}
	if err = out.Close(); err != nil {
		return p, err
	}
	if err = m.Collected.Rename(name+".tmp", name); err != nil {
		return p, err
	}
	p, err = m.preview(ctx, j, Input{Metadata: state.Metadata, Path: "_collected/" + name, Encoding: "utf-8", BookID: j.Input.BookID}, chapters)
	if err != nil {
		return p, err
	}
	state.Source.Downloaded = len(chapters)
	state.Source.Complete = len(chapters) == state.Source.Total
	state.Source.CollectedAt = time.Now().UnixMilli()
	p.Source = &state.Source
	p.Warnings = append(p.Warnings, warnings...)
	if state.Metadata.Author == "" {
		p.Warnings = append(p.Warnings, "来源未提供作者，请先核对作品身份。")
	}
	if !state.Source.Complete {
		p.Warnings = append(p.Warnings, fmt.Sprintf("本次仅采集目录前 %d / %d 章，属于部分正文；已入库的正文暂不支持追加或替换。", len(chapters), state.Source.Total))
	}
	p.Warnings = append(p.Warnings, "采集范围以当前来源目录为准，不代表作品已完结或与首发目录完全一致。")
	return p, nil
}
