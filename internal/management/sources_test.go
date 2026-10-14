package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func ruleFixture(t *testing.T, m *Manager, kind string) BookSource {
	t.Helper()
	c := SourceTemplates()[0].Config
	if kind == "json" {
		c = SourceTemplates()[1].Config
	}
	s, err := m.SaveSource(context.Background(), "", 0, true, c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func htmlPages() map[string]string {
	return map[string]string{
		"/book":   `<h1>测试长篇</h1><span class="author">测试作者</span><div id="list"><a href="/c1">第一章 开始</a><a href="/c1">第一章 开始</a></div><a class="next" href="/toc2">下一页</a>`,
		"/toc2":   `<div id="list"><a href="/c2">第二章 继续</a></div>`,
		"/c1":     `<h1>第一章 开始</h1><div id="content"><p>正文第一段。</p><script>恶意脚本</script><p class="ad">广告</p><p>正文第二段。</p></div><a class="next" href="/c1-2">下一页</a>`,
		"/c1-2":   `<h1>第一章 开始（2/2）</h1><div id="content">分页正文<br>段落末尾</div><a class="next" href="/c2">下一章</a>`,
		"/c2":     `<h1>第二章 继续</h1><div id="content">另一章的故事。</div>`,
		"/search": `<div class="book"><a class="title" href="/book">测试长篇</a><span class="author">测试作者</span></div>`,
	}
}
func pagedSource(t *testing.T, m *Manager) BookSource {
	s := ruleFixture(t, m, "html")
	c := s.Config
	c.TOC.Next = attr("a.next", "href")
	c.Chapter.Next = attr("a.next", "href")
	s, e := m.SaveSource(context.Background(), s.ID, s.Version, true, c)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func servePages(m *Manager, pages map[string]string, calls map[string]int) {
	m.Remote.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		body, ok := pages[r.URL.Path]
		if !ok {
			return response(500, nil), nil
		}
		return response(200, []byte(body)), nil
	})
}
func TestSourceRulesAndPublicNetworkValidation(t *testing.T) {
	for _, tpl := range SourceTemplates() {
		if err := validateSource(tpl.Config); err != nil {
			t.Fatal(tpl.ID, err)
		}
	}
	c := SourceTemplates()[0].Config
	for _, raw := range []string{"http://127.0.0.1/a", "https://example.com:8080/a", "https://user:pass@example.com/", "file:///etc/passwd", "https://evil.example/a", "https://example.com.localhost/", "https://[::1]/"} {
		if _, err := sourceURL(c, c.BaseURL, raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	for _, addr := range []string{"127.0.0.1", "10.1.1.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "224.0.0.1", "198.18.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2002:7f00:1::", "64:ff9b::7f00:1"} {
		if publicIP(netip.MustParseAddr(addr)) {
			t.Fatal("accepted IP", addr)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) || !publicIP(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public IP rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, e := publicTransport().DialContext(ctx, "tcp", "127.0.0.1:80"); e == nil {
		conn.Close()
		t.Fatal("dialed loopback")
	}
	cases := []func(*SourceConfig){
		func(c *SourceConfig) { c.Kind = "legado" }, func(c *SourceConfig) { c.IntervalMS = 0 }, func(c *SourceConfig) { c.AllowedHosts = []string{"*.example.com"} },
		func(c *SourceConfig) { c.TOC.Items = "[" }, func(c *SourceConfig) { c.Chapter.Content = FieldRule{} },
	}
	for _, change := range cases {
		bad := c
		change(&bad)
		if validateSource(bad) == nil {
			t.Fatal("invalid rule accepted", bad)
		}
	}
	m, _ := fixture(t)
	s := ruleFixture(t, m, "html")
	calls := 0
	m.Remote.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		res := response(302, nil)
		res.Header.Set("Location", "https://127.0.0.1/private")
		return res, nil
	})
	if _, _, err := m.Remote.SourcePage(ctx, s.snapshot(), "https://example.com/start", nil); err == nil || calls != 1 {
		t.Fatal("redirect escaped", err, calls)
	}
}
func TestSourceCRUDAndSnapshotSelection(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	first := ruleFixture(t, m, "html")
	second := ruleFixture(t, m, "json")
	j := enqueue(t, m, "search", Input{Query: "测试", SourceIDs: []string{first.ID}, SourceSnapshots: []SourceSnapshot{{ID: "untrusted"}}})
	if len(j.Input.SourceSnapshots) != 1 || j.Input.SourceSnapshots[0].ID != first.ID {
		t.Fatal("selection/snapshot", j.Input)
	}
	if _, e := m.SaveSource(ctx, first.ID, first.Version+1, false, first.Config); e != ErrConflict {
		t.Fatal("stale save", e)
	}
	disabled, e := m.SaveSource(ctx, second.ID, second.Version, false, second.Config)
	if e != nil || disabled.Enabled {
		t.Fatal(e)
	}
	if _, e = m.Enqueue(ctx, "search", Input{Query: "测试", SourceIDs: []string{disabled.ID}}); e == nil {
		t.Fatal("disabled searched")
	}
	if _, e = m.Enqueue(ctx, "collect", Input{URL: "https://example.com/book", SourceID: first.ID, SourceVersion: 99, MaxChapters: 2}); e != ErrConflict {
		t.Fatal(e)
	}
	c := first.Config
	c.Chapter.Content = css("#edited")
	if _, e = m.SaveSource(ctx, first.ID, first.Version, true, c); e != nil {
		t.Fatal(e)
	}
	old, _ := m.Job(ctx, j.ID)
	if old.Input.SourceSnapshots[0].Config.Chapter.Content.Selector != "#content" {
		t.Fatal("queued snapshot changed")
	}
}
func TestHTMLCollectionPagingCheckpointAndPublication(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	s := pagedSource(t, m)
	pages := htmlPages()
	calls := map[string]int{}
	delete(pages, "/c2")
	servePages(m, pages, calls)
	j := enqueue(t, m, "collect", Input{SourceID: s.ID, URL: "https://example.com/book", MaxChapters: 2, Metadata: Metadata{Title: "测试长篇", Author: "测试作者"}})
	m.RunOne(ctx)
	failed := state(t, m, j.ID, "failed")
	if !strings.Contains(failed.Message, "第 2 章") {
		t.Fatal(failed.Message)
	}
	pages["/c2"] = htmlPages()["/c2"]
	changed := s.Config
	changed.Chapter.Content = css("#wrong")
	if _, err := m.SaveSource(ctx, s.ID, s.Version, true, changed); err != nil {
		t.Fatal(err)
	}
	if err := m.Action(ctx, j.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	m.DB.Write.Exec(`UPDATE library_jobs SET next_at=0 WHERE id=?`, j.ID)
	m.RunOne(ctx)
	review := state(t, m, j.ID, "review")
	if calls["/c1"] != 1 || calls["/c1-2"] != 1 {
		t.Fatal("checkpoint redownloaded", calls)
	}
	var p Preview
	if err := json.Unmarshal(review.Result, &p); err != nil {
		t.Fatal(err)
	}
	if p.ChapterCount != 2 || p.Source.RuleVersion != s.Version || !p.Source.Complete || !strings.Contains(strings.Join(p.Warnings, ""), "重复链接") {
		t.Fatal(p)
	}
	raw, err := m.Collected.ReadFile(j.ID + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "广告") || strings.Contains(string(raw), "恶意脚本") || strings.Count(string(raw), "另一章的故事") != 1 {
		t.Fatal(string(raw))
	}
	var n int
	m.DB.Read.QueryRow(`SELECT count(*) FROM books`).Scan(&n)
	if n != 0 {
		t.Fatal("preview published")
	}
	if err := m.Action(ctx, j.ID, "confirm"); err != nil {
		t.Fatal(err)
	}
	m.RunOne(ctx)
	state(t, m, j.ID, "completed")
	var version int
	var sourceURL string
	if err := m.DB.Read.QueryRow(`SELECT rule_version,source_url FROM book_sources`).Scan(&version, &sourceURL); err != nil || version != s.Version || sourceURL != "https://example.com/book" {
		t.Fatal(version, sourceURL, err)
	}
}
func TestJSONRulesEncodingAndCollection(t *testing.T) {
	m, _ := fixture(t)
	s := ruleFixture(t, m, "json")
	ctx := context.Background()
	pages := map[string]string{
		"/book": `{"title":"接口小说","author":"作者","tocUrl":"/toc"}`,
		"/toc":  `{"chapters":[{"title":"第一章","url":"/c1"}]}`,
		"/c1":   `{"title":"第一章","content":"第一段。\n第二段。"}`,
	}
	servePages(m, pages, map[string]int{})
	j := enqueue(t, m, "collect", Input{SourceID: s.ID, URL: "https://example.com/book", MaxChapters: 1})
	m.RunOne(ctx)
	state(t, m, j.ID, "review")
	c := SourceTemplates()[0].Config
	c.Charset = "gb18030"
	raw, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("<h1>中文网页</h1>"))
	d, err := parseDocument(raw, c)
	if err != nil || d.text(css("h1"), false) != "中文网页" {
		t.Fatal(err)
	}
	c.Charset = "utf-8"
	if _, err := parseDocument(raw, c); err == nil {
		t.Fatal("garbled UTF8 accepted")
	}
	if v := pointer(map[string]any{"a/b": []any{"value"}}, "/a~1b/0"); v != "value" {
		t.Fatal(v)
	}
}
func TestSourcePaginationAndIdentityFailures(t *testing.T) {
	for _, scenario := range []string{"toc-loop", "chapter-loop", "wrong-title", "empty", "repeated-page", "wrong-book", "changed-toc"} {
		t.Run(scenario, func(t *testing.T) {
			m, _ := fixture(t)
			s := pagedSource(t, m)
			pages := htmlPages()
			in := Input{SourceID: s.ID, URL: "https://example.com/book", MaxChapters: 2}
			switch scenario {
			case "toc-loop":
				pages["/toc2"] += `<a class="next" href="/book">next</a>`
			case "chapter-loop":
				pages["/c1-2"] = strings.ReplaceAll(pages["/c1-2"], `href="/c2"`, `href="/c1"`)
			case "wrong-title":
				pages["/c1-2"] = strings.ReplaceAll(pages["/c1-2"], "第一章 开始", "第二章 继续")
			case "empty":
				pages["/c1"] = `<h1>第一章 开始</h1><div id="content"></div>`
			case "repeated-page":
				pages["/c1-2"] = pages["/c1"]
			case "wrong-book":
				in.Title = "另一本书"
			case "changed-toc":
				delete(pages, "/c2")
			}
			servePages(m, pages, map[string]int{})
			j := enqueue(t, m, "collect", in)
			m.RunOne(context.Background())
			state(t, m, j.ID, "failed")
			if scenario == "changed-toc" {
				pages["/c2"] = htmlPages()["/c2"]
				pages["/toc2"] = strings.ReplaceAll(pages["/toc2"], "继续", "新标题")
				m.Action(context.Background(), j.ID, "retry")
				m.DB.Write.Exec(`UPDATE library_jobs SET next_at=0`)
				m.RunOne(context.Background())
				got := state(t, m, j.ID, "failed")
				if !strings.Contains(got.Message, "检查点已变化") {
					t.Fatal(got.Message)
				}
			}
		})
	}
}
func TestSearchIsolationAndSourceChecks(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	a := pagedSource(t, m)
	b := ruleFixture(t, m, "json")
	pages := htmlPages()
	servePages(m, pages, map[string]int{})
	j := enqueue(t, m, "search", Input{Query: "测试", Author: "不同作者", SourceIDs: []string{a.ID, b.ID}})
	m.RunOne(ctx)
	completed := state(t, m, j.ID, "completed")
	var result SearchResult
	json.Unmarshal(completed.Result, &result)
	if len(result.Sources) != 2 || len(result.Candidates) != 1 || !result.Candidates[0].NeedsReview {
		t.Fatal(string(completed.Result))
	}
	check := enqueue(t, m, "source-check", Input{SourceID: a.ID, URL: "https://example.com/book", Query: "测试"})
	m.RunOne(ctx)
	state(t, m, check.ID, "completed")
	updated, _ := m.Source(ctx, a.ID)
	var report SourceCheck
	json.Unmarshal(updated.Check, &report)
	if !report.Passed || report.Chapters != 2 || report.SampleWords == 0 || report.SearchCount == nil || *report.SearchCount != 1 || updated.CheckVersion != a.Version {
		t.Fatal(string(updated.Check))
	}
}

// Opt-in, low-volume integration check: metadata, directory, first chapter only.
// No publication or whole-book download. Fixed fixtures remain the CI baseline.
func TestLiveSourceSamples(t *testing.T) {
	if os.Getenv("LIBRARY_SOURCE_LIVE") != "1" {
		t.Skip("set LIBRARY_SOURCE_LIVE=1 for current online source observations")
	}
	samples := map[string]struct{ URL, Title, Author string }{
		"shuhaige": {"https://www.shuhaige.net/57169/", "万道龙皇", "牧童听竹"},
		"xiangshu": {"http://www.ibiqugu.net/53/53211/", "仙工开物", "蛊真人"},
	}
	for _, tpl := range SourceTemplates() {
		sample, ok := samples[tpl.ID]
		if !ok {
			continue
		}
		t.Run(tpl.ID, func(t *testing.T) {
			m, _ := fixture(t)
			m.Remote.Interval = 5 * time.Second
			s, e := m.SaveSource(context.Background(), "", 0, true, tpl.Config)
			if e != nil {
				t.Fatal(e)
			}
			j := enqueue(t, m, "source-check", Input{SourceID: s.ID, URL: sample.URL, Query: sample.Title})
			m.RunOne(context.Background())
			got := state(t, m, j.ID, "completed")
			var result struct {
				Check SourceCheck `json:"sourceCheck"`
			}
			json.Unmarshal(got.Result, &result)
			t.Log(string(got.Result))
			if !result.Check.Passed || result.Check.Title != sample.Title || result.Check.Author != sample.Author {
				t.Fatal(fmt.Sprintf("sample check did not pass: %+v", result.Check))
			}
		})
	}
}

func TestSourceSearchFormEncoding(t *testing.T) {
	m, _ := fixture(t)
	s := ruleFixture(t, m, "html")
	s.Config.Search.Form = map[string]string{"searchkey": "{query}"}
	m.Remote.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" {
			t.Fatal(r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("searchkey") != "中文 & +" {
			t.Fatal(r.Form)
		}
		return response(200, []byte(htmlPages()["/search"])), nil
	})
	if _, err := m.searchOne(context.Background(), s.snapshot(), "中文 & +", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := url.Parse(s.Config.BaseURL); err != nil {
		t.Fatal(err)
	}
}
