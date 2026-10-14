package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var qidianPath = regexp.MustCompile(`^/(?:book|info)/(\d{1,12})/?$`)

func qidianID(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", fmt.Errorf("请输入起点作品的 HTTPS 链接")
	}
	if u.Host != "m.qidian.com" && u.Host != "www.qidian.com" && u.Host != "book.qidian.com" {
		return "", fmt.Errorf("目前支持中文起点作品链接")
	}
	m := qidianPath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", fmt.Errorf("起点作品链接格式无效")
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || id < 1 {
		return "", fmt.Errorf("起点作品编号无效")
	}
	return strconv.FormatInt(id, 10), nil
}

type collectedChapter struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Hash  string `json:"hash"`
}
type capture struct {
	Metadata
	Source   Source             `json:"source"`
	Chapters []collectedChapter `json:"chapters"`
}

func writeAtomic(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(name+".tmp", name)
}
func (m *Manager) collect(ctx context.Context, j Job) (Preview, error) {
	var p Preview
	id, err := qidianID(j.Input.URL)
	if err != nil {
		return p, err
	}
	stateName := j.ID + ".json"
	var state capture
	if saved, err := m.Collected.ReadFile(stateName); err == nil {
		if json.Unmarshal(saved, &state) != nil || state.Source.ID != id || len(state.Chapters) == 0 {
			return p, fmt.Errorf("采集检查点无效，请新建任务")
		}
	} else {
		body, err := m.Remote.Get(ctx, "qidian", "https://m.qidian.com/book/"+id+"/", 2<<20)
		if err != nil {
			return p, err
		}
		var book struct {
			BookInfo struct {
				ID     int64  `json:"bookId"`
				Title  string `json:"bookName"`
				Author string `json:"authorName"`
				Tag1   string `json:"chanName"`
				Tag2   string `json:"subCateName"`
				Intro  string `json:"desc"`
			} `json:"bookInfo"`
		}
		if err := pageData(body, &book); err != nil {
			return p, err
		}
		if fmt.Sprint(book.BookInfo.ID) != id {
			return p, fmt.Errorf("来源作品编号不匹配")
		}
		state.Metadata = Metadata{book.BookInfo.Title, book.BookInfo.Author, book.BookInfo.Tag1, book.BookInfo.Tag2, plainHTML(book.BookInfo.Intro)}
		if !ValidMetadata(state.Metadata) {
			return p, fmt.Errorf("来源书目信息不完整")
		}
		body, err = m.Remote.Get(ctx, "qidian", "https://m.qidian.com/book/"+id+"/catalog/", 4<<20)
		if err != nil {
			return p, err
		}
		var catalog struct {
			ID      int64 `json:"bookId"`
			Total   int   `json:"chapterTotalCnt"`
			Volumes []struct {
				VIP      *int `json:"vS"`
				Chapters []struct {
					ID    int64  `json:"id"`
					Title string `json:"cN"`
				} `json:"cs"`
			} `json:"vs"`
		}
		if err := pageData(body, &catalog); err != nil {
			return p, err
		}
		if fmt.Sprint(catalog.ID) != id || len(catalog.Volumes) == 0 {
			return p, fmt.Errorf("来源目录不完整")
		}
		state.Source = Source{Name: "qidian", ID: id, URL: "https://m.qidian.com/book/" + id + "/", Total: catalog.Total}
		seen := map[int64]bool{}
		total := 0
		for _, v := range catalog.Volumes {
			if v.VIP == nil {
				return p, fmt.Errorf("来源目录缺少章节访问状态")
			}
			for _, c := range v.Chapters {
				if c.ID < 1 || strings.TrimSpace(c.Title) == "" || seen[c.ID] {
					return p, fmt.Errorf("来源目录存在重复或无效章节")
				}
				seen[c.ID] = true
				total++
				if *v.VIP != 0 {
					state.Source.SkippedPaid++
					continue
				}
				if len(state.Chapters) < j.Input.MaxChapters {
					state.Chapters = append(state.Chapters, collectedChapter{ID: c.ID, Title: c.Title})
				}
			}
		}
		if total != catalog.Total {
			return p, fmt.Errorf("来源目录数量不一致，暂不采集，避免漏章")
		}
		if len(state.Chapters) == 0 {
			return p, fmt.Errorf("目录中没有可采集的公开免费章节")
		}
		state.Source.Downloaded = len(state.Chapters)
		state.Source.Complete = len(state.Chapters) == total
		data, _ := json.Marshal(state)
		if err := writeAtomic(m.Collected, stateName, data); err != nil {
			return p, err
		}
	}
	for i, c := range state.Chapters {
		if ctx.Err() != nil {
			return p, ctx.Err()
		}
		name := fmt.Sprintf("%s.%d.part", j.ID, c.ID)
		if c.Hash != "" {
			if data, err := m.Collected.ReadFile(name); err == nil {
				sum := sha256.Sum256(data)
				if hex.EncodeToString(sum[:]) == c.Hash {
					continue
				}
			}
		}
		body, err := m.Remote.Get(ctx, "qidian", fmt.Sprintf("https://m.qidian.com/chapter/%s/%d/", id, c.ID), 2<<20)
		if err != nil {
			return p, err
		}
		var chapter struct {
			BookInfo struct {
				ID int64 `json:"bookId"`
			} `json:"bookInfo"`
			Info struct {
				ID        int64  `json:"chapterId"`
				Name      string `json:"chapterName"`
				Content   string `json:"content"`
				VIP       *int   `json:"vipStatus"`
				Price     int    `json:"price"`
				Encrypted int    `json:"fEnS"`
				Font      string `json:"randomFont"`
				FixedFont string `json:"fixedFontTtf"`
				Woff      string `json:"fixedFontWoff2"`
				E         int    `json:"eFW"`
				CES       int    `json:"cES"`
			} `json:"chapterInfo"`
		}
		if err := pageData(body, &chapter); err != nil {
			return p, err
		}
		info := chapter.Info
		if fmt.Sprint(chapter.BookInfo.ID) != id || info.ID != c.ID || info.VIP == nil || *info.VIP != 0 || info.Price != 0 {
			return p, fmt.Errorf("章节访问状态已变化，已暂停采集")
		}
		if info.Encrypted != 0 || info.Font != "" || info.FixedFont != "" || info.Woff != "" || info.E != 0 || info.CES != 0 {
			return p, fmt.Errorf("章节使用特殊字体或加密，请通过浏览器采集后导入")
		}
		content := plainHTML(info.Content)
		if strings.TrimSpace(content) == "" {
			return p, fmt.Errorf("章节没有可读取的正文，已暂停采集")
		}
		// A deterministic heading keeps chapter order even for nonstandard titles.
		title := c.Title
		if !heading.MatchString(title) {
			title = fmt.Sprintf("第%d章 %s", i+1, title)
		}
		data := []byte(title + "\n" + content + "\n\n")
		if err := writeAtomic(m.Collected, name, data); err != nil {
			return p, err
		}
		sum := sha256.Sum256(data)
		state.Chapters[i].Hash = hex.EncodeToString(sum[:])
		saved, _ := json.Marshal(state)
		if err := writeAtomic(m.Collected, stateName, saved); err != nil {
			return p, err
		}
		progress, _ := json.Marshal(map[string]any{"downloaded": i + 1, "planned": len(state.Chapters), "source": state.Source})
		if _, err := m.DB.Write.ExecContext(ctx, `UPDATE library_jobs SET result=? WHERE id=?`, string(progress), j.ID); err != nil {
			return p, err
		}
	}
	name := j.ID + ".txt"
	out, err := m.Collected.OpenFile(name+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return p, err
	}
	chapters := make([]Chapter, 0, len(state.Chapters))
	offset := int64(0)
	for i, c := range state.Chapters {
		f, e := m.Collected.Open(fmt.Sprintf("%s.%d.part", j.ID, c.ID))
		if e != nil {
			out.Close()
			return p, e
		}
		n, copyErr := io.Copy(out, f)
		e = copyErr
		f.Close()
		if e != nil {
			out.Close()
			return p, e
		}
		chapters = append(chapters, Chapter{Index: i + 1, Title: c.Title, Start: offset, End: offset + n})
		offset += n
	}
	err = out.Sync()
	closeErr := out.Close()
	if err != nil {
		return p, err
	}
	if closeErr != nil {
		return p, closeErr
	}
	if err = m.Collected.Rename(name+".tmp", name); err != nil {
		return p, err
	}
	in := Input{Metadata: state.Metadata, Path: "_collected/" + name, Encoding: "utf-8", BookID: j.Input.BookID}
	p, err = m.preview(ctx, j, in, chapters)
	if err != nil {
		return p, err
	}
	p.Source = &state.Source
	if !state.Source.Complete {
		p.Warnings = append(p.Warnings, fmt.Sprintf("本次收录 %d / %d 章；%d 章付费内容未采集。这是部分正文。", state.Source.Downloaded, state.Source.Total, state.Source.SkippedPaid))
	}
	return p, nil
}
