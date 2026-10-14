package management

// Templates are editable starting points, never silently installed sources.
// Site layouts and reachability can change: users check a sample before use.
type SourceTemplate struct {
	ID     string       `json:"id"`
	Note   string       `json:"note"`
	Config SourceConfig `json:"config"`
}

func css(s string) FieldRule     { return FieldRule{Selector: s} }
func attr(s, a string) FieldRule { return FieldRule{Selector: s, Attr: a} }
func SourceTemplates() []SourceTemplate {
	html := SourceConfig{Name: "自定义 HTML 书源", Group: "男频", Kind: "html", BaseURL: "https://example.com/", AllowedHosts: []string{"example.com"}, Charset: "utf-8", IntervalMS: 5000,
		Search:  &SearchRule{URL: "https://example.com/search?q={query}", Items: ".book", Title: css("a.title"), Author: css(".author"), Link: attr("a.title", "href")},
		Book:    BookRule{Title: css("h1"), Author: css(".author"), Intro: css(".intro")},
		TOC:     TOCRule{Items: "#list a", Title: css("."), Link: attr(".", "href")},
		Chapter: ChapterRule{Title: css("h1"), Content: css("#content"), Remove: []string{".ad", ".navigation"}},
	}
	js := SourceConfig{Name: "自定义 JSON 书源", Group: "男频", Kind: "json", BaseURL: "https://example.com/", AllowedHosts: []string{"example.com"}, Charset: "utf-8", IntervalMS: 5000,
		Search:  &SearchRule{URL: "https://example.com/search?q={query}", Items: "/books", Title: css("/title"), Author: css("/author"), Link: css("/url")},
		Book:    BookRule{Title: css("/title"), Author: css("/author"), TOC: css("/tocUrl")},
		TOC:     TOCRule{Items: "/chapters", Title: css("/title"), Link: css("/url"), Next: css("/next")},
		Chapter: ChapterRule{Title: css("/title"), Content: css("/content"), Next: css("/next")},
	}
	shu := SourceConfig{Name: "书海阁", Group: "男频", Kind: "html", BaseURL: "https://www.shuhaige.net/", AllowedHosts: []string{"www.shuhaige.net"}, Charset: "utf-8", IntervalMS: 5000,
		Search:  &SearchRule{URL: "https://www.shuhaige.net/search.html", Form: map[string]string{"searchkey": "{query}", "searchtype": "all"}, Items: "#sitembox > dl", Title: css("dd h3 a"), Author: FieldRule{Selector: "dd:nth-child(3) span:nth-child(1)", TrimPrefix: "作者："}, Link: attr("dd h3 a", "href"), Category: css("dd:nth-child(3) span:nth-child(3)"), Latest: css("dd:nth-child(5) a")},
		Book:    BookRule{Title: attr(`meta[property="og:novel:book_name"]`, "content"), Author: attr(`meta[property="og:novel:author"]`, "content"), Category: attr(`meta[property="og:novel:category"]`, "content"), Status: attr(`meta[property="og:novel:status"]`, "content"), Intro: css("#intro p:first-child")},
		TOC:     TOCRule{Items: "dl > dt:nth-of-type(2) ~ dd > a", Title: css("."), Link: attr(".", "href")},
		Chapter: ChapterRule{Title: css(".bookname h1"), Content: css("#content"), Next: attr("#pager_next", "href"), Remove: []string{".bottem2", "hr", "table"}, DropLines: []string{"本小章还未完，请点击下一页继续阅读后面精彩内容！", "小主，这个章节后面还有哦，请点击下一页继续阅读，后面更精彩！", "这章没有结束，请点击下一页继续阅读！"}},
	}
	// Direct HTTP search currently returns a site error despite browser results.
	// Keep URL collection available without advertising a verified search adapter.
	shu.Search = nil
	xiang := SourceConfig{Name: "香书小说", Group: "男频", Kind: "html", BaseURL: "http://www.ibiqugu.net/", AllowedHosts: []string{"www.ibiqugu.net", "www.xbiqugu.la"}, Charset: "utf-8", IntervalMS: 5000,
		Search:  &SearchRule{URL: "http://www.ibiqugu.net/modules/article/waps.php", Form: map[string]string{"searchkey": "{query}"}, Items: "#checkform tr:has(td)", Title: css("td:nth-child(1) a"), Author: css("td:nth-child(3)"), Link: attr("td:nth-child(1) a", "href"), Latest: css("td:nth-child(2) a")},
		Book:    BookRule{Title: attr(`meta[property="og:novel:book_name"]`, "content"), Author: attr(`meta[property="og:novel:author"]`, "content"), Category: attr(`meta[property="og:novel:category"]`, "content"), Intro: css("#intro")},
		TOC:     TOCRule{Items: "#list dl dd a", Title: css("."), Link: attr(".", "href")},
		Chapter: ChapterRule{Title: css(".bookname h1"), Content: css("#content"), Remove: []string{".bottem2", ".ad"}},
	}
	return []SourceTemplate{
		{ID: "html", Note: "示例域名需替换；使用 CSS 选择器和 attr 提取属性。", Config: html},
		{ID: "json", Note: "示例接口需替换；使用 /data/title 形式的 JSON Pointer。", Config: js},
		{ID: "shuhaige", Note: "支持作品链接采集；当前后端搜索返回站点错误，暂不启用搜索。添加后先检查样本。", Config: shu},
		{ID: "xiangshu", Note: "香书当前域名 ibiqugu.net；旧域名已跳转。添加后先检查样本。", Config: xiang},
	}
}
