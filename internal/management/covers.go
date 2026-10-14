package management

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
)

func identity(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("《》〈〉（）()[]【】·:：,，.。_-", r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
}
func matched(b Metadata, title, author string) bool {
	return identity(b.Title) != "" && identity(b.Author) != "" && identity(b.Title) == identity(title) && identity(b.Author) == identity(author)
}
func qidianCover(body []byte, b Metadata) (string, error) {
	var data struct {
		BookInfo struct {
			Records []struct {
				ID     json.Number `json:"bid"`
				Title  string      `json:"bName"`
				Author string      `json:"bAuth"`
			} `json:"records"`
		} `json:"bookInfo"`
	}
	if err := pageData(body, &data); err != nil {
		return "", err
	}
	for _, c := range data.BookInfo.Records {
		if matched(b, c.Title, c.Author) {
			id, err := c.ID.Int64()
			if err != nil || id < 1 {
				return "", fmt.Errorf("来源封面编号无效")
			}
			return fmt.Sprintf("https://bookcover.yuewen.com/qdbimg/349573/%d/300", id), nil
		}
	}
	return "", nil
}

var qqBook = regexp.MustCompile(`^//book\.qq\.com/book-detail/(\d+)$`)

func qqCover(body []byte, b Metadata) (string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	var result string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if result != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "li" {
			var title, author, id string
			var inside func(*html.Node)
			inside = func(x *html.Node) {
				if x.Type == html.ElementNode && x.Data == "a" {
					var href, t string
					for _, a := range x.Attr {
						if a.Key == "href" {
							href = a.Val
						}
						if a.Key == "title" {
							t = a.Val
						}
					}
					if match := qqBook.FindStringSubmatch(href); match != nil {
						title = t
						id = match[1]
					}
					if strings.HasPrefix(href, "//book.qq.com/book-writer/") {
						var text strings.Builder
						var read func(*html.Node)
						read = func(c *html.Node) {
							if c.Type == html.TextNode {
								text.WriteString(c.Data)
							}
							for q := c.FirstChild; q != nil; q = q.NextSibling {
								read(q)
							}
						}
						read(x)
						author = text.String()
					}
				}
				for c := x.FirstChild; c != nil; c = c.NextSibling {
					inside(c)
				}
			}
			inside(n)
			if id != "" && matched(b, title, author) {
				var num int64
				fmt.Sscan(id, &num)
				result = fmt.Sprintf("https://wfqqreader-1252317822.image.myqcloud.com/cover/%d/%s/t7_%s.webp", num%1000, id, id)
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return result, nil
}
func doubanCover(body []byte, b Metadata) (string, error) {
	var records []struct {
		Title  string `json:"title"`
		Author string `json:"author_name"`
		Image  string `json:"pic"`
	}
	if err := json.Unmarshal(body, &records); err != nil {
		return "", fmt.Errorf("来源搜索格式变化")
	}
	for _, c := range records {
		if matched(b, c.Title, c.Author) {
			return c.Image, nil
		}
	}
	return "", nil
}
func (m *Manager) cover(ctx context.Context, j Job) (any, error) {
	if f, _, err := m.Cover(ctx, j.Input.BookID); err == nil {
		f.Close()
		return map[string]any{"bookId": j.Input.BookID, "preserved": true}, nil
	}
	var b Metadata
	if err := m.DB.Read.QueryRowContext(ctx, `SELECT title,author FROM books WHERE id=?`, j.Input.BookID).Scan(&b.Title, &b.Author); err != nil {
		return nil, err
	}
	if identity(b.Author) == "" {
		return nil, fmt.Errorf("请先补充作者，以免匹配到同名作品")
	}
	lookups := []struct {
		name, url string
		parse     func([]byte, Metadata) (string, error)
	}{
		{"qidian", "https://m.qidian.com/search?kw=" + url.QueryEscape(b.Title), qidianCover},
		{"qq", "https://book.qq.com/so/" + url.PathEscape(b.Title), qqCover},
		{"douban", "https://book.douban.com/j/subject_suggest?q=" + url.QueryEscape(b.Title), doubanCover},
	}
	var last error
	for _, source := range lookups {
		body, err := m.Remote.Get(ctx, source.name, source.url, 2<<20)
		if err != nil {
			last = err
			continue
		}
		candidate, err := source.parse(body, b)
		if err != nil {
			last = err
			continue
		}
		if candidate == "" {
			continue
		}
		body, err = m.Remote.Get(ctx, source.name, candidate, 6<<20)
		if err != nil {
			last = err
			continue
		}
		data, err := thumbnail(body)
		if err != nil {
			last = err
			continue
		}
		sum := sha256.Sum256(data)
		name := fmt.Sprintf("%d-%s.jpg", j.Input.BookID, hex.EncodeToString(sum[:8]))
		if err := writeAtomic(m.Covers, name, data); err != nil {
			return nil, err
		}
		// A metadata edit while downloading invalidates the identity match.
		tx, err := m.DB.Write.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM library_jobs WHERE id=?`, j.ID).Scan(&status); err != nil {
			return nil, err
		}
		if status != "running" {
			return nil, ErrConflict
		}
		var title, author string
		if err := tx.QueryRowContext(ctx, `SELECT title,author FROM books WHERE id=?`, j.Input.BookID).Scan(&title, &author); err != nil {
			return nil, err
		}
		if title != b.Title || author != b.Author {
			return nil, ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO cover_assets VALUES(?,?,?,?) ON CONFLICT(book_id) DO UPDATE SET path=excluded.path,source_url=excluded.source_url,updated_at=excluded.updated_at`, j.Input.BookID, name, candidate, time.Now().UnixMilli()); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return map[string]any{"bookId": j.Input.BookID, "source": source.name}, nil
	}
	if last != nil {
		return nil, last
	}
	return nil, &BlockedError{Message: "没有找到书名和作者均匹配的真实封面", Until: time.Now().Add(30 * 24 * time.Hour).UnixMilli()}
}
func thumbnail(data []byte) ([]byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 10 || config.Height < 10 || config.Width > 4000 || config.Height > 4000 || int64(config.Width)*int64(config.Height) > 8_000_000 {
		return nil, fmt.Errorf("来源图片无效或过大")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("来源图片无法解码")
	}
	w, h := config.Width, config.Height
	if w > 300 {
		h = max(1, h*300/w)
		w = 300
	}
	if h > 480 {
		w = max(1, w*480/h)
		h = 480
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), im, im.Bounds(), draw.Src, nil)
	var b bytes.Buffer
	if err := jpeg.Encode(&b, out, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Cover resolves only database-selected paths inside the dedicated image root.
func (m *Manager) Cover(ctx context.Context, id int64) (*os.File, string, error) {
	var current, legacy sql.NullString
	err := m.DB.Read.QueryRowContext(ctx, `SELECT a.path,c.legacy_path FROM books b LEFT JOIN cover_assets a ON a.book_id=b.id LEFT JOIN covers c ON c.book_id=b.id WHERE b.id=? AND b.status='APPROVED'`, id).Scan(&current, &legacy)
	if err != nil {
		return nil, "", err
	}
	for _, name := range []string{current.String, strings.TrimPrefix(legacy.String, "/covers/")} {
		if name == "" || strings.ContainsAny(name, ":\\") || strings.HasPrefix(name, "/") {
			continue
		}
		ext := strings.ToLower(path.Ext(name))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" && ext != ".gif" {
			continue
		}
		f, err := m.Covers.Open(name)
		if err != nil {
			continue
		}
		stat, err := f.Stat()
		if err != nil || !stat.Mode().IsRegular() || stat.Size() > 6<<20 {
			f.Close()
			continue
		}
		_, format, err := image.DecodeConfig(io.LimitReader(f, 6<<20))
		if err != nil {
			f.Close()
			continue
		}
		if _, err = f.Seek(0, 0); err != nil {
			f.Close()
			continue
		}
		return f, "image/" + format, nil
	}
	return nil, "", sql.ErrNoRows
}
