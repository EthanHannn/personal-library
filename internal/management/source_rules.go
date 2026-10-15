package management

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	htmlcharset "golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type sourceDocument struct {
	base  string
	node  *html.Node
	value any
	kind  string
}

func validPointer(p string) bool {
	if p == "." {
		return true
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' {
			if i+1 == len(p) || (p[i+1] != '0' && p[i+1] != '1') {
				return false
			}
			i++
		}
	}
	return true
}
func pointer(v any, p string) any {
	if p == "." {
		return v
	}
	if !validPointer(p) {
		return nil
	}
	for _, key := range strings.Split(p[1:], "/") {
		key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
		switch obj := v.(type) {
		case map[string]any:
			v = obj[key]
		case []any:
			n, e := strconv.Atoi(key)
			if e != nil || n < 0 || n >= len(obj) {
				return nil
			}
			v = obj[n]
		default:
			return nil
		}
	}
	return v
}
func parseDocument(raw []byte, c SourceConfig) (sourceDocument, error) {
	var err error
	switch c.Charset {
	case "gb18030":
		raw, err = simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
	case "auto":
		if !utf8.Valid(raw) {
			enc, _, _ := htmlcharset.DetermineEncoding(raw, "")
			if enc != nil {
				raw, err = enc.NewDecoder().Bytes(raw)
			}
		}
	}
	if err != nil || !utf8.Valid(raw) || bytes.ContainsRune(raw, '\uFFFD') || bytes.ContainsRune(raw, 0) {
		return sourceDocument{}, fmt.Errorf("页面编码不符或含乱码，请检查书源编码")
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if c.Kind == "json" {
		var v any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&v) != nil {
			return sourceDocument{}, fmt.Errorf("来源未返回有效 JSON，可能是验证页面")
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return sourceDocument{}, fmt.Errorf("JSON 响应包含多余数据")
		}
		return sourceDocument{value: v, kind: "json"}, nil
	}
	node, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return sourceDocument{}, fmt.Errorf("网页解析失败")
	}
	return sourceDocument{node: node, kind: "html"}, nil
}
func (d sourceDocument) items(selector string) ([]sourceDocument, error) {
	out := []sourceDocument{}
	if d.kind == "json" {
		v := pointer(d.value, selector)
		if v == nil {
			return out, nil
		}
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("JSON items 指针没有指向数组")
		}
		if len(list) > 20000 {
			return nil, fmt.Errorf("列表超过 20000 项")
		}
		for _, v := range list {
			out = append(out, sourceDocument{value: v, kind: "json"})
		}
		return out, nil
	}
	if selector == "." {
		return []sourceDocument{d}, nil
	}
	sel, err := cascadia.Compile(selector)
	if err != nil {
		return nil, err
	}
	list := cascadia.QueryAll(d.node, sel)
	if len(list) > 20000 {
		return nil, fmt.Errorf("列表超过 20000 项")
	}
	for _, n := range list {
		out = append(out, sourceDocument{node: n, kind: "html"})
	}
	return out, nil
}
func (d sourceDocument) text(f FieldRule, content bool) string {
	if f.Selector == "" {
		return ""
	}
	value := ""
	if d.kind == "json" {
		switch v := pointer(d.value, f.Selector).(type) {
		case string:
			value = v
		case json.Number:
			value = v.String()
		}
	} else {
		nodes, _ := d.items(f.Selector)
		for _, n := range nodes {
			if f.Attr != "" {
				for _, a := range n.node.Attr {
					if a.Key == f.Attr {
						value = a.Val
						break
					}
				}
				break
			}
			var buf bytes.Buffer
			_ = html.Render(&buf, n.node)
			if value != "" {
				value += "\n"
			}
			value += plainHTML(buf.String())
			if !content {
				break
			}
		}
	}
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), f.TrimPrefix))
	if !content {
		value = strings.Join(strings.Fields(value), " ")
	}
	return value
}
func (d sourceDocument) remove(selectors []string) {
	if d.kind != "html" {
		return
	}
	for _, sel := range append([]string{"script", "style", "noscript", "iframe"}, selectors...) {
		nodes, _ := d.items(sel)
		for _, n := range nodes {
			if n.node.Parent != nil {
				n.node.Parent.RemoveChild(n.node)
			}
		}
	}
}
func (m *Manager) sourcePage(ctx context.Context, s SourceSnapshot, raw string, form url.Values) (sourceDocument, []byte, error) {
	body, final, err := m.Remote.SourcePage(ctx, s, raw, form)
	if err != nil {
		return sourceDocument{}, nil, err
	}
	doc, err := parseDocument(body, s.Config)
	doc.base = final
	return doc, body, err
}

type sourceBook struct {
	Metadata
	URL    string
	TOC    string
	Status string
}
type sourceChapter struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	RawHash     string   `json:"rawHash,omitempty"`
	TextHash    string   `json:"textHash,omitempty"`
	FileHash    string   `json:"fileHash,omitempty"`
	CollectedAt int64    `json:"collectedAt,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

func (m *Manager) fetchSourceBook(ctx context.Context, s SourceSnapshot, raw string) (sourceBook, error) {
	b := sourceBook{URL: raw, TOC: raw}
	doc, _, err := m.sourcePage(ctx, s, raw, nil)
	if err != nil {
		return b, err
	}
	b.URL = doc.base
	b.TOC = doc.base
	r := s.Config.Book
	b.Metadata = Metadata{Title: doc.text(r.Title, false), Author: doc.text(r.Author, false), Tag1: doc.text(r.Category, false), Intro: doc.text(r.Intro, true)}
	b.Status = doc.text(r.Status, false)
	if !ValidMetadata(b.Metadata) {
		return b, fmt.Errorf("作品页没有有效书名或书目信息超限，请检查详情规则")
	}
	if r.TOC.Selector != "" {
		b.TOC, err = sourceURL(s.Config, doc.base, doc.text(r.TOC, false))
		if err != nil {
			return b, fmt.Errorf("目录链接：%w", err)
		}
	}
	return b, nil
}
func (m *Manager) fetchSourceTOC(ctx context.Context, s SourceSnapshot, raw string) ([]sourceChapter, int, error) {
	out := []sourceChapter{}
	visited := map[string]bool{}
	chapters := map[string]bool{}
	duplicates := 0
	for page := 0; raw != ""; page++ {
		if page >= 50 || visited[raw] {
			return nil, duplicates, fmt.Errorf("目录分页循环或超过 50 页，请检查下一页规则")
		}
		visited[raw] = true
		doc, _, err := m.sourcePage(ctx, s, raw, nil)
		if err != nil {
			return nil, duplicates, err
		}
		items, err := doc.items(s.Config.TOC.Items)
		if err != nil {
			return nil, duplicates, err
		}
		if len(items) == 0 {
			return nil, duplicates, fmt.Errorf("目录页未找到章节，请检查目录规则")
		}
		for _, item := range items {
			title := item.text(s.Config.TOC.Title, false)
			link, err := sourceURL(s.Config, doc.base, item.text(s.Config.TOC.Link, false))
			if err != nil {
				return nil, duplicates, fmt.Errorf("目录章节链接：%w", err)
			}
			if title == "" || len([]rune(title)) > 300 {
				return nil, duplicates, fmt.Errorf("目录章节标题为空或过长")
			}
			if chapters[link] {
				duplicates++
				continue
			}
			chapters[link] = true
			out = append(out, sourceChapter{URL: link, Title: title})
			if len(out) > 20000 {
				return nil, duplicates, fmt.Errorf("目录超过 20000 章")
			}
		}
		next := doc.text(s.Config.TOC.Next, false)
		if next == "" {
			break
		}
		raw, err = sourceURL(s.Config, doc.base, next)
		if err != nil {
			return nil, duplicates, err
		}
	}
	if s.Config.TOC.Reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, duplicates, nil
}

var pageSuffix = regexp.MustCompile(`(?:[（(]\s*(?:第\s*)?\d+\s*(?:/\s*\d+\s*|页\s*)?[）)]|_\d+)$`)
var bookTitleSuffix = regexp.MustCompile(`[-—–]\s*《[^》]{1,100}》$`)
var chapterPageCounter = regexp.MustCompile(`^第[（(]\d+/\d+[）)]页$`)

func sourceIdentity(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(s))
}
func chapterIdentity(s string) string {
	s = bookTitleSuffix.ReplaceAllString(strings.TrimSpace(s), "")
	return sourceIdentity(pageSuffix.ReplaceAllString(s, ""))
}
func hashBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (m *Manager) fetchSourceChapter(ctx context.Context, s SourceSnapshot, c sourceChapter, catalog map[string]bool) (string, string, error) {
	raw := c.URL
	visited := map[string]bool{}
	parts := []string{}
	rawHash := sha256.New()
	size := 0
	for page := 0; raw != ""; page++ {
		if page >= 20 || visited[raw] {
			return "", "", fmt.Errorf("正文分页循环或超过 20 页")
		}
		visited[raw] = true
		doc, body, err := m.sourcePage(ctx, s, raw, nil)
		if err != nil {
			return "", "", err
		}
		rawHash.Write(body)
		title := doc.text(s.Config.Chapter.Title, false)
		if title == "" || chapterIdentity(title) != chapterIdentity(c.Title) {
			return "", "", fmt.Errorf("正文标题与目录不符：%s；请检查标题或下一页规则", title)
		}
		next := doc.text(s.Config.Chapter.Next, false)
		doc.remove(s.Config.Chapter.Remove)
		content := doc.text(s.Config.Chapter.Content, true)
		lines := []string{}
		for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			drop := line == "" || chapterPageCounter.MatchString(line)
			for _, noise := range s.Config.Chapter.DropLines {
				if line == noise {
					drop = true
					break
				}
			}
			if !drop {
				lines = append(lines, line)
			}
		}
		content = strings.Join(lines, "\n")
		if content == "" {
			return "", "", fmt.Errorf("正文为空，请检查内容或清理规则")
		}
		if len([]rune(content)) < 500 {
			for _, marker := range []string{"请登录后阅读", "请订阅本章", "请完成安全验证", "访问过于频繁"} {
				if strings.Contains(content, marker) {
					return "", "", fmt.Errorf("正文为登录、订阅或访问限制提示")
				}
			}
		}
		size += len(content)
		if size > 4<<20 {
			return "", "", fmt.Errorf("单章正文超过 4 MiB")
		}
		for _, part := range parts {
			if part == content {
				return "", "", fmt.Errorf("正文分页返回了重复内容")
			}
		}
		parts = append(parts, content)
		if next == "" {
			break
		}
		raw, err = sourceURL(s.Config, doc.base, next)
		if err != nil {
			return "", "", err
		}
		// Some sites use the same anchor for next page and next chapter. A known
		// different catalog entry ends this chapter; never concatenate it here.
		if raw != c.URL && catalog[raw] {
			break
		}
	}
	return strings.Join(parts, "\n"), hex.EncodeToString(rawHash.Sum(nil)), nil
}
