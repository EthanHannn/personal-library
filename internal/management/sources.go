package management

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/andybalholm/cascadia"
)

// Rules are declarative: CSS selectors for HTML, RFC 6901 pointers for JSON.
// There is deliberately no executable code, cookies, custom headers or file I/O.
type FieldRule struct {
	Selector   string `json:"selector"`
	Attr       string `json:"attr,omitempty"`
	TrimPrefix string `json:"trimPrefix,omitempty"`
}
type SearchRule struct {
	URL      string            `json:"url"`
	Form     map[string]string `json:"form,omitempty"`
	Items    string            `json:"items"`
	Title    FieldRule         `json:"title"`
	Author   FieldRule         `json:"author"`
	Link     FieldRule         `json:"link"`
	Category FieldRule         `json:"category,omitempty"`
	Latest   FieldRule         `json:"latest,omitempty"`
}
type BookRule struct {
	Title    FieldRule `json:"title"`
	Author   FieldRule `json:"author"`
	Intro    FieldRule `json:"intro,omitempty"`
	Category FieldRule `json:"category,omitempty"`
	TOC      FieldRule `json:"toc,omitempty"`
	Status   FieldRule `json:"status,omitempty"`
}
type TOCRule struct {
	Items   string    `json:"items"`
	Title   FieldRule `json:"title"`
	Link    FieldRule `json:"link"`
	Next    FieldRule `json:"next,omitempty"`
	Reverse bool      `json:"reverse,omitempty"`
}
type ChapterRule struct {
	Title     FieldRule `json:"title"`
	Content   FieldRule `json:"content"`
	Next      FieldRule `json:"next,omitempty"`
	Remove    []string  `json:"remove,omitempty"`
	DropLines []string  `json:"dropLines,omitempty"`
}
type SourceConfig struct {
	Name         string      `json:"name"`
	Group        string      `json:"group"`
	Kind         string      `json:"kind"`
	BaseURL      string      `json:"baseUrl"`
	AllowedHosts []string    `json:"allowedHosts"`
	Charset      string      `json:"charset"`
	IntervalMS   int         `json:"intervalMs"`
	Search       *SearchRule `json:"search,omitempty"`
	Book         BookRule    `json:"book"`
	TOC          TOCRule     `json:"toc"`
	Chapter      ChapterRule `json:"chapter"`
}
type BookSource struct {
	ID            string          `json:"id"`
	Version       int             `json:"version"`
	Enabled       bool            `json:"enabled"`
	Config        SourceConfig    `json:"config"`
	CheckedAt     int64           `json:"checkedAt"`
	CheckVersion  int             `json:"checkVersion"`
	Check         json.RawMessage `json:"check"`
	CooldownUntil int64           `json:"cooldownUntil"`
}

// Snapshots contain only rules, never mutable health data. Every queued job owns
// its version, including retries after rules have been edited.
type SourceSnapshot struct {
	ID      string       `json:"id"`
	Version int          `json:"version"`
	Config  SourceConfig `json:"config"`
}

func (s BookSource) snapshot() SourceSnapshot { return SourceSnapshot{s.ID, s.Version, s.Config} }

const sourceColumns = `s.id,s.version,s.enabled,s.config,s.checked_at,s.check_version,s.check_result,coalesce(c.next_at,0)`

func scanSource(row interface{ Scan(...any) error }) (BookSource, error) {
	var s BookSource
	var config, check string
	err := row.Scan(&s.ID, &s.Version, &s.Enabled, &config, &s.CheckedAt, &s.CheckVersion, &check, &s.CooldownUntil)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal([]byte(config), &s.Config)
	s.Check = json.RawMessage(check)
	return s, err
}
func (m *Manager) Sources(ctx context.Context) ([]BookSource, error) {
	rows, err := m.DB.Read.QueryContext(ctx, `SELECT `+sourceColumns+` FROM source_configs s LEFT JOIN source_cooldowns c ON c.source=s.id ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BookSource{}
	for rows.Next() {
		s, e := scanSource(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (m *Manager) Source(ctx context.Context, id string) (BookSource, error) {
	return scanSource(m.DB.Read.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM source_configs s LEFT JOIN source_cooldowns c ON c.source=s.id WHERE s.id=?`, id))
}
func (m *Manager) SaveSource(ctx context.Context, id string, version int, enabled bool, c SourceConfig) (BookSource, error) {
	if id == "qidian" {
		current, err := m.Source(ctx, id)
		if err != nil {
			return BookSource{}, err
		}
		if !reflect.DeepEqual(c, current.Config) {
			return BookSource{}, fmt.Errorf("起点内置规则仅支持启用或停用")
		}
	} else if err := validateSource(c); err != nil {
		return BookSource{}, err
	}
	raw, _ := json.Marshal(c)
	if len(raw) > 24000 {
		return BookSource{}, fmt.Errorf("书源规则最多 24 KiB")
	}
	if id == "" {
		id = "src_" + rand.Text()
		result, err := m.DB.Write.ExecContext(ctx, `INSERT INTO source_configs(id,version,enabled,config) SELECT ?,1,?,? WHERE (SELECT count(*) FROM source_configs)<50`, id, enabled, string(raw))
		if err != nil {
			return BookSource{}, err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return BookSource{}, fmt.Errorf("最多保存 50 个书源")
		}
	} else {
		result, err := m.DB.Write.ExecContext(ctx, `UPDATE source_configs SET version=version+1,enabled=?,config=? WHERE id=? AND version=?`, enabled, string(raw), id, version)
		if err != nil {
			return BookSource{}, err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return BookSource{}, ErrConflict
		}
	}
	return m.Source(ctx, id)
}

var hostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

func validateSource(c SourceConfig) error {
	if strings.TrimSpace(c.Name) == "" || len([]rune(c.Name)) > 100 || len([]rune(c.Group)) > 100 {
		return fmt.Errorf("请填写书源名称（最多 100 字）")
	}
	if c.Kind != "html" && c.Kind != "json" {
		return fmt.Errorf("仅支持 html/CSS 或 json/JSON Pointer 规则，不支持 JS、XPath 或直接导入阅读书源")
	}
	if c.Charset != "utf-8" && c.Charset != "gb18030" && c.Charset != "auto" {
		return fmt.Errorf("编码须为 utf-8、gb18030 或 auto")
	}
	if c.IntervalMS < 1000 || c.IntervalMS > 60000 {
		return fmt.Errorf("请求间隔须为 1000–60000 毫秒")
	}
	if len(c.AllowedHosts) < 1 || len(c.AllowedHosts) > 8 {
		return fmt.Errorf("请填写 1–8 个确切的允许域名")
	}
	for _, h := range c.AllowedHosts {
		if !hostPattern.MatchString(h) || !strings.Contains(h, ".") || len(h) > 253 || strings.Contains(h, "..") {
			return fmt.Errorf("允许域名无效：不支持通配符、端口或路径")
		}
	}
	if _, err := sourceURL(c, c.BaseURL, c.BaseURL); err != nil {
		return err
	}
	validateSelector := func(sel string) error {
		if sel == "" || sel == "." {
			return nil
		}
		if len(sel) > 1000 {
			return fmt.Errorf("提取规则过长")
		}
		if c.Kind == "json" {
			if !validPointer(sel) {
				return fmt.Errorf("JSON 规则须使用 /data/title 形式的 JSON Pointer；当前项用 .")
			}
			return nil
		}
		if _, err := cascadia.Compile(sel); err != nil {
			return fmt.Errorf("CSS 选择器无效：%s", sel)
		}
		return nil
	}
	fields := []FieldRule{c.Book.Title, c.Book.Author, c.Book.Intro, c.Book.Category, c.Book.TOC, c.Book.Status, c.TOC.Title, c.TOC.Link, c.TOC.Next, c.Chapter.Title, c.Chapter.Content, c.Chapter.Next}
	required := []string{c.Book.Title.Selector, c.TOC.Items, c.TOC.Title.Selector, c.TOC.Link.Selector, c.Chapter.Title.Selector, c.Chapter.Content.Selector}
	selectors := []string{c.TOC.Items}
	if s := c.Search; s != nil {
		if !strings.Contains(s.URL, "{query}") && len(s.Form) == 0 {
			return fmt.Errorf("搜索链接或表单须包含 {query}")
		}
		if _, err := sourceURL(c, c.BaseURL, strings.ReplaceAll(s.URL, "{query}", "test")); err != nil {
			return err
		}
		if len(s.Form) > 10 {
			return fmt.Errorf("搜索表单最多 10 个字段")
		}
		hasQuery := strings.Contains(s.URL, "{query}")
		for k, v := range s.Form {
			if len(k) > 100 || len(v) > 500 {
				return fmt.Errorf("搜索表单字段过长")
			}
			hasQuery = hasQuery || strings.Contains(v, "{query}")
		}
		if !hasQuery {
			return fmt.Errorf("搜索链接或表单须包含 {query}")
		}
		fields = append(fields, s.Title, s.Author, s.Link, s.Category, s.Latest)
		required = append(required, s.Items, s.Title.Selector, s.Link.Selector)
		selectors = append(selectors, s.Items)
	}
	for _, s := range required {
		if s == "" {
			return fmt.Errorf("缺少书名、目录项/标题/链接或正文标题/内容规则")
		}
	}
	for _, f := range fields {
		if err := validateSelector(f.Selector); err != nil {
			return err
		}
		if len(f.TrimPrefix) > 200 || len(f.Attr) > 100 || (c.Kind == "json" && f.Attr != "") {
			return fmt.Errorf("属性或前缀规则无效；JSON 不使用 attr")
		}
	}
	selectors = append(selectors, c.Chapter.Remove...)
	if len(c.Chapter.Remove) > 20 || len(c.Chapter.DropLines) > 30 {
		return fmt.Errorf("正文清理规则过多")
	}
	if c.Kind == "json" && len(c.Chapter.Remove) > 0 {
		return fmt.Errorf("JSON 正文不支持 DOM 移除规则")
	}
	for _, sel := range selectors {
		if err := validateSelector(sel); err != nil {
			return err
		}
	}
	for _, line := range c.Chapter.DropLines {
		if line == "" || len(line) > 1000 {
			return fmt.Errorf("清理行须为 1–1000 字节的完整文本")
		}
	}
	return nil
}

func (m *Manager) bindSources(ctx context.Context, kind string, in *Input) error {
	in.SourceSnapshots = nil // Never accept caller-supplied snapshots.
	if kind == "search" {
		in.Query = strings.TrimSpace(in.Query)
		if in.Query == "" || len([]rune(in.Query)) > 100 || len([]rune(in.Author)) > 100 {
			return fmt.Errorf("请输入 1–100 字的书名，作者最多 100 字")
		}
		list, err := m.Sources(ctx)
		if err != nil {
			return err
		}
		wanted := map[string]bool{}
		for _, id := range in.SourceIDs {
			wanted[id] = true
		}
		all := len(wanted) == 0
		for _, s := range list {
			if s.Enabled && s.Config.Search != nil && (all || wanted[s.ID]) {
				in.SourceSnapshots = append(in.SourceSnapshots, s.snapshot())
				delete(wanted, s.ID)
			}
		}
		if len(wanted) > 0 {
			return fmt.Errorf("所选书源已停用、不支持搜索或不存在")
		}
		if len(in.SourceSnapshots) < 1 || len(in.SourceSnapshots) > 8 {
			return fmt.Errorf("每次请选择 1–8 个启用且支持搜索的书源")
		}
		return nil
	}
	if in.SourceID == "" {
		in.SourceID = "qidian"
	}
	s, err := m.Source(ctx, in.SourceID)
	if err != nil {
		return err
	}
	if kind != "source-check" && !s.Enabled {
		return fmt.Errorf("书源已停用")
	}
	if in.SourceVersion != 0 && in.SourceVersion != s.Version {
		return ErrConflict
	}
	in.SourceVersion = s.Version
	if s.Config.Kind == "qidian" {
		if kind == "source-check" {
			return fmt.Errorf("起点请用少量章节的采集任务检查")
		}
		_, err = qidianID(in.URL)
	} else {
		in.URL, err = sourceURL(s.Config, s.Config.BaseURL, in.URL)
	}
	if err != nil {
		return err
	}
	in.SourceSnapshots = []SourceSnapshot{s.snapshot()}
	return nil
}

func sourceURL(c SourceConfig, base, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || len(raw) > 2000 {
		return "", fmt.Errorf("来源链接为空或过长")
	}
	b, e := url.Parse(base)
	if e != nil {
		return "", fmt.Errorf("来源基址无效")
	}
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil {
		return "", fmt.Errorf("来源链接无效")
	}
	u = b.ResolveReference(u)
	if !publicURL(u) {
		return "", fmt.Errorf("来源仅允许公共 HTTP/HTTPS 域名和标准端口")
	}
	found := false
	for _, h := range c.AllowedHosts {
		if strings.EqualFold(h, u.Hostname()) {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("链接域名不在该书源的允许列表：%s", u.Hostname())
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}
