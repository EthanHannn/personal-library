package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EthanHannn/personal-library/internal/storage"
	"golang.org/x/net/html"
)

type BlockedError struct {
	Message string
	Until   int64
}

func (e *BlockedError) Error() string { return e.Message }

type Remote struct {
	DB       *storage.Database
	Client   *http.Client
	Interval time.Duration
}

func NewRemote(db *storage.Database) *Remote {
	return &Remote{DB: db, Interval: 5 * time.Second, Client: &http.Client{Transport: publicTransport(), Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || !allowedURL(r.URL) {
			return fmt.Errorf("来源重定向不可用")
		}
		return nil
	}}}
}
func allowedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "m.qidian.com", "www.qidian.com", "book.qidian.com", "bookcover.yuewen.com", "qidian.qpic.cn", "book.qq.com", "book.douban.com", "img1.doubanio.com", "img2.doubanio.com", "img3.doubanio.com", "img9.doubanio.com", "wfqqreader-1252317822.image.myqcloud.com":
		return true
	}
	return false
}
func (r *Remote) Get(ctx context.Context, source, raw string, limit int64) ([]byte, error) {
	return r.fetch(ctx, source, raw, nil, limit, r.Interval, allowedURL, nil)
}
func (r *Remote) SourcePage(ctx context.Context, s SourceSnapshot, raw string, form url.Values) ([]byte, string, error) {
	interval := time.Duration(s.Config.IntervalMS) * time.Millisecond
	if r.Interval == 0 {
		interval = 0
	} // Fixture transport; production always has a nonzero interval.
	final := raw
	body, err := r.fetch(ctx, s.ID, raw, form, 4<<20, interval, func(u *url.URL) bool { _, err := sourceURL(s.Config, s.Config.BaseURL, u.String()); return err == nil }, &final)
	return body, final, err
}
func (r *Remote) fetch(ctx context.Context, source, raw string, form url.Values, limit int64, interval time.Duration, allowed func(*url.URL) bool, final *string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || !allowed(u) {
		return nil, fmt.Errorf("不支持的来源地址")
	}
	if err := r.reserve(ctx, source, interval); err != nil {
		return nil, err
	}
	method := "GET"
	var requestBody io.Reader
	if len(form) > 0 {
		method = "POST"
		requestBody = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), requestBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "personal-library/0.1 (private-library; sequential requests)")
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	if len(form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := *r.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !allowed(req.URL) {
			return fmt.Errorf("来源重定向不在允许范围内")
		}
		return r.reserve(ctx, source, interval)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("来源连接失败，请稍后重试")
	}
	defer res.Body.Close()
	if final != nil && res.Request != nil {
		*final = res.Request.URL.String()
	}
	if res.StatusCode == 202 || res.StatusCode == 403 || res.StatusCode == 429 {
		until := time.Now().Add(6 * time.Hour).UnixMilli()
		_, _ = r.DB.Write.ExecContext(ctx, `UPDATE source_cooldowns SET next_at=? WHERE source=?`, until, source)
		return nil, &BlockedError{Message: fmt.Sprintf("来源限制访问（HTTP %d），已暂停请求", res.StatusCode), Until: until}
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("来源返回 HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("来源响应中断")
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("来源响应超过大小限制")
	}
	if !bytes.Contains(body, []byte("vite-plugin-ssr_pageContext")) && (bytes.Contains(body, []byte("x-waf-captcha")) || bytes.Contains(body, []byte("<title>Just a moment")) || bytes.Contains(body, []byte("_cf_chl_opt"))) {
		until := time.Now().Add(6 * time.Hour).UnixMilli()
		_, _ = r.DB.Write.ExecContext(ctx, `UPDATE source_cooldowns SET next_at=? WHERE source=?`, until, source)
		return nil, &BlockedError{Message: "来源返回浏览器验证页面，已暂停请求；可在浏览器采集后导入 TXT", Until: until}
	}
	return body, nil
}
func (r *Remote) reserve(ctx context.Context, source string, interval time.Duration) error {
	var next int64
	if err := r.DB.Read.QueryRowContext(ctx, `SELECT coalesce((SELECT next_at FROM source_cooldowns WHERE source=?),0)`, source).Scan(&next); err != nil {
		return err
	}
	wait := time.Until(time.UnixMilli(next))
	if wait > max(10*time.Second, interval+time.Second) {
		return &BlockedError{Message: "来源正在冷却，请稍后重试", Until: next}
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if _, err := r.DB.Write.ExecContext(ctx, `INSERT INTO source_cooldowns VALUES(?,?) ON CONFLICT(source) DO UPDATE SET next_at=excluded.next_at`, source, time.Now().Add(interval).UnixMilli()); err != nil {
		return err
	}
	return nil
}

func pageData(body []byte, target any) error {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			return fmt.Errorf("来源页面缺少作品数据，可能需要浏览器验证")
		}
		if kind != html.StartTagToken {
			continue
		}
		tag := tokenizer.Token()
		if tag.Data != "script" {
			continue
		}
		found := false
		for _, a := range tag.Attr {
			if a.Key == "id" && a.Val == "vite-plugin-ssr_pageContext" {
				found = true
			}
		}
		if !found {
			continue
		}
		if tokenizer.Next() != html.TextToken {
			return fmt.Errorf("来源数据格式变化")
		}
		var envelope struct {
			PageContext struct {
				PageProps struct {
					PageData json.RawMessage `json:"pageData"`
				} `json:"pageProps"`
			} `json:"pageContext"`
		}
		if err := json.Unmarshal(tokenizer.Text(), &envelope); err != nil {
			return fmt.Errorf("来源数据格式变化")
		}
		if len(envelope.PageContext.PageProps.PageData) == 0 {
			return fmt.Errorf("来源数据为空")
		}
		if err := json.Unmarshal(envelope.PageContext.PageProps.PageData, target); err != nil {
			return fmt.Errorf("来源数据结构变化")
		}
		return nil
	}
}
func plainHTML(input string) string {
	z := html.NewTokenizer(strings.NewReader(input))
	var b strings.Builder
	hidden := 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		t := z.Token()
		switch kind {
		case html.StartTagToken, html.SelfClosingTagToken:
			if t.Data == "script" || t.Data == "style" {
				hidden++
			}
			if hidden == 0 && (t.Data == "p" || t.Data == "br" || t.Data == "div") {
				b.WriteByte('\n')
			}
		case html.EndTagToken:
			if (t.Data == "script" || t.Data == "style") && hidden > 0 {
				hidden--
			}
			if hidden == 0 && (t.Data == "p" || t.Data == "div") {
				b.WriteByte('\n')
			}
		case html.TextToken:
			if hidden == 0 {
				b.WriteString(t.Data)
			}
		}
	}
	lines := strings.Split(b.String(), "\n")
	out := lines[:0]
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
