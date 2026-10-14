package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func fixture(t *testing.T) (*Manager, string) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	dbPath := filepath.Join(base, "library.db")
	if err := storage.Migrate(ctx, dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, dir := range []string{"novels", "covers", "collected"} {
		if err := os.Mkdir(filepath.Join(base, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	r, err := reader.Open(filepath.Join(base, "novels"), 2, filepath.Join(base, "collected"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	m, err := Open(db, r, filepath.Join(base, "novels"), filepath.Join(base, "covers"), filepath.Join(base, "collected"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.Remote.Interval = 0
	return m, base
}
func state(t *testing.T, m *Manager, id, want string) Job {
	t.Helper()
	j, err := m.Job(context.Background(), id)
	if err != nil || j.Status != want {
		t.Fatalf("job %+v err=%v; want %s", j, err, want)
	}
	return j
}
func enqueue(t *testing.T, m *Manager, kind string, in Input) Job {
	t.Helper()
	j, err := m.Enqueue(context.Background(), kind, in)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func file(t *testing.T, base, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, "novels", name), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestTXTPreviewConfirmAndDuplicate(t *testing.T) {
	for _, encoding := range []string{"utf-8", "gb18030"} {
		t.Run(encoding, func(t *testing.T) {
			m, base := fixture(t)
			ctx := context.Background()
			text := "说明\r\n第一章 开始\r\n春天的正文。\r\n第二章 结束\r\n结束。"
			raw := []byte(text)
			if encoding == "gb18030" {
				var err error
				raw, err = simplifiedchinese.GB18030.NewEncoder().Bytes(raw)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				raw = append([]byte{0xef, 0xbb, 0xbf}, raw...)
			}
			file(t, base, "novel.txt", raw)
			in := Input{Metadata: Metadata{Title: "书", Author: "作者"}, Path: "novel.txt", Encoding: encoding}
			j := enqueue(t, m, "import", in)
			duplicate := enqueue(t, m, "import", in)
			if duplicate.ID != j.ID {
				t.Fatal("active import not deduplicated")
			}
			m.RunOne(ctx)
			j = state(t, m, j.ID, "review")
			var p Preview
			if err := json.Unmarshal(j.Result, &p); err != nil {
				t.Fatal(err)
			}
			if p.ChapterCount != 3 || p.Chapters[0].Title != "卷首" || p.Chapters[1].Title != "第一章 开始" {
				t.Fatalf("chapter boundaries %+v", p)
			}
			var count int
			m.DB.Read.QueryRow(`SELECT count(*) FROM books`).Scan(&count)
			if count != 0 {
				t.Fatal("preview published book")
			}
			for range 2 {
				if err := m.Action(ctx, j.ID, "confirm"); err != nil {
					t.Fatal(err)
				}
			}
			m.RunOne(ctx)
			j = state(t, m, j.ID, "completed")
			if err := json.Unmarshal(j.Result, &p); err != nil {
				t.Fatal(err)
			}
			if err := m.Action(ctx, j.ID, "confirm"); err != nil {
				t.Fatal("acknowledgement retry", err)
			}
			_, c, f, err := storage.NewRepository(m.DB).Chapter(ctx, p.BookID, 2)
			if err != nil {
				t.Fatal(err)
			}
			block, err := m.Reader.Read(ctx, f, c.Start, c.End, c.Start, 4096)
			if err != nil || !strings.Contains(block.Text, "春天的正文") || strings.Contains(block.Text, "第二章") {
				t.Fatal(block, err)
			}
			j2 := enqueue(t, m, "import", in)
			m.RunOne(ctx)
			state(t, m, j2.ID, "review")
			m.Action(ctx, j2.ID, "confirm")
			m.RunOne(ctx)
			state(t, m, j2.ID, "failed")
			m.DB.Read.QueryRow(`SELECT count(*) FROM books`).Scan(&count)
			if count != 1 {
				t.Fatal("duplicate publication", count)
			}
		})
	}
}
func TestChangedFileAndBackfillPreserveHistory(t *testing.T) {
	m, base := fixture(t)
	ctx := context.Background()
	raw := []byte("第一章 初始\n原始正文\n")
	file(t, base, "novel.txt", raw)
	if _, err := m.DB.Write.Exec(`INSERT INTO books VALUES(42,'书','作者','主类','副类','','简介','APPROVED',NULL,NULL,NULL,0,0,0,0,10);INSERT INTO rating_snapshots VALUES(42,100,850,8.5,50,25,20,3,2)`); err != nil {
		t.Fatal(err)
	}
	j := enqueue(t, m, "import", Input{Metadata: Metadata{Title: "书", Author: "作者"}, Path: "novel.txt", Encoding: "utf-8", BookID: 42})
	m.RunOne(ctx)
	state(t, m, j.ID, "review")
	info, _ := os.Stat(filepath.Join(base, "novels", "novel.txt"))
	file(t, base, "novel.txt", bytes.Replace(raw, []byte("原始"), []byte("修改"), 1))
	os.Chtimes(filepath.Join(base, "novels", "novel.txt"), info.ModTime(), info.ModTime())
	m.Action(ctx, j.ID, "confirm")
	m.RunOne(ctx)
	state(t, m, j.ID, "failed")
	var has int
	var score float64
	m.DB.Read.QueryRow(`SELECT has_content,score FROM books JOIN rating_snapshots ON book_id=id WHERE id=42`).Scan(&has, &score)
	if has != 0 || score != 8.5 {
		t.Fatal("history changed", has, score)
	}
	if _, err := m.DB.Write.Exec(`UPDATE library_jobs SET next_at=0 WHERE id=?`, j.ID); err != nil {
		t.Fatal(err)
	}
	m.Action(ctx, j.ID, "retry")
	m.RunOne(ctx)
	state(t, m, j.ID, "review")
	m.Action(ctx, j.ID, "confirm")
	m.RunOne(ctx)
	state(t, m, j.ID, "completed")
	m.DB.Read.QueryRow(`SELECT has_content,score FROM books JOIN rating_snapshots ON book_id=id WHERE id=42`).Scan(&has, &score)
	if has != 1 || score != 8.5 {
		t.Fatal("backfill changed score", has, score)
	}
}

type transport func(*http.Request) (*http.Response, error)

func (fn transport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func response(status int, data []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data))}
}
func page(v any) []byte {
	b, _ := json.Marshal(map[string]any{"pageContext": map[string]any{"pageProps": map[string]any{"pageData": v}}})
	return []byte(`<script id="vite-plugin-ssr_pageContext">` + string(b) + `</script>`)
}

func TestQidianResumesPublicChaptersAndMarksPartial(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	calls := map[string]int{}
	fail := true
	m.Remote.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/book/123/":
			return response(200, page(map[string]any{"bookInfo": map[string]any{"bookId": 123, "bookName": "测试采集", "authorName": "作者", "desc": "<p>简介</p>"}})), nil
		case "/book/123/catalog/":
			return response(200, page(map[string]any{"bookId": 123, "chapterTotalCnt": 3, "vs": []any{map[string]any{"vS": 0, "cs": []any{map[string]any{"id": 10, "cN": "开始"}, map[string]any{"id": 11, "cN": "继续"}}}, map[string]any{"vS": 1, "cs": []any{map[string]any{"id": 12, "cN": "付费"}}}}})), nil
		case "/chapter/123/10/", "/chapter/123/11/":
			if strings.Contains(r.URL.Path, "/11/") && fail {
				return response(429, []byte("limited")), nil
			}
			id := 10
			if strings.Contains(r.URL.Path, "/11/") {
				id = 11
			}
			return response(200, page(map[string]any{"bookInfo": map[string]any{"bookId": 123}, "chapterInfo": map[string]any{"chapterId": id, "vipStatus": 0, "price": 0, "content": "<p>测试正文</p><p>第一章 这是正文中提到的标题</p>"}})), nil
		default:
			t.Fatal("unexpected remote request", r.URL)
			return nil, nil
		}
	})
	j := enqueue(t, m, "collect", Input{URL: "https://www.qidian.com/book/123/", MaxChapters: 10})
	m.RunOne(ctx)
	j = state(t, m, j.ID, "failed")
	if j.NextAt <= time.Now().UnixMilli() {
		t.Fatal("429 did not persist cooldown")
	}
	if !strings.Contains(string(j.Result), `"downloaded":1`) {
		t.Fatal("checkpoint not exposed", string(j.Result))
	}
	// Recreate the client and clear the simulated cooldown as if it elapsed.
	fail = false
	m.DB.Write.Exec(`UPDATE source_cooldowns SET next_at=0;UPDATE library_jobs SET next_at=0`)
	m.Action(ctx, j.ID, "retry")
	m.RunOne(ctx)
	j = state(t, m, j.ID, "review")
	var p Preview
	json.Unmarshal(j.Result, &p)
	if p.ChapterCount != 2 || p.Source == nil || p.Source.Complete || p.Source.SkippedPaid != 1 || len(p.Warnings) == 0 {
		t.Fatalf("partial result %+v", p)
	}
	if calls["/chapter/123/10/"] != 1 || calls["/book/123/"] != 1 || calls["/chapter/123/11/"] != 2 {
		t.Fatal("checkpoint not resumed", calls)
	}
	m.Action(ctx, j.ID, "confirm")
	m.RunOne(ctx)
	j = state(t, m, j.ID, "completed")
	json.Unmarshal(j.Result, &p)
	_, c, f, err := storage.NewRepository(m.DB).Chapter(ctx, p.BookID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if block, err := m.Reader.Read(ctx, f, c.Start, c.End, c.Start, 4096); err != nil || !strings.Contains(block.Text, "正文中") {
		t.Fatal(block, err)
	}
}

func TestRealCoverMatchingValidationAndPreservation(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	m.DB.Write.Exec(`INSERT INTO books VALUES(1,'书名','作者','','','','','APPROVED',NULL,NULL,NULL,0,0,0,0,0);INSERT INTO rating_snapshots VALUES(1,0,0,0,0,0,0,0,0)`)
	img := image.NewRGBA(image.Rect(0, 0, 600, 900))
	img.Set(1, 1, color.White)
	var b bytes.Buffer
	png.Encode(&b, img)
	calls := 0
	m.Remote.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "m.qidian.com" {
			return response(200, page(map[string]any{"bookInfo": map[string]any{"records": []any{map[string]any{"bid": 3, "bName": "书名", "bAuth": "别人"}, map[string]any{"bid": 4, "bName": "《书名》", "bAuth": "作者"}}}})), nil
		}
		if r.URL.Host != "bookcover.yuewen.com" || !strings.Contains(r.URL.Path, "/4/") {
			t.Fatal("wrong identity image", r.URL)
		}
		return response(200, b.Bytes()), nil
	})
	j := enqueue(t, m, "cover", Input{BookID: 1})
	m.RunOne(ctx)
	state(t, m, j.ID, "completed")
	f, media, err := m.Cover(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || media != "image/jpeg" || cfg.Width != 300 || cfg.Height != 450 {
		t.Fatal(cfg, media, err)
	}
	j = enqueue(t, m, "cover", Input{BookID: 1})
	m.RunOne(ctx)
	state(t, m, j.ID, "completed")
	if calls != 2 {
		t.Fatal("existing cover replaced", calls)
	}
	for _, raw := range []string{"http://m.qidian.com/", "https://m.qidian.com.evil.test/", "https://127.0.0.1/a", "https://m.qidian.com:123/a", "https://x@m.qidian.com/a"} {
		u, _ := url.Parse(raw)
		if allowedURL(u) {
			t.Fatal("unsafe URL", raw)
		}
	}
	if _, err := thumbnail([]byte("<html>blocked</html>")); err == nil {
		t.Fatal("html accepted as image")
	}
	if _, err := qidianCover(page(map[string]any{"bookInfo": map[string]any{"records": []any{map[string]any{"bid": 1, "bName": "书名", "bAuth": "别人"}}}}), Metadata{Title: "书名", Author: "作者"}); err != nil {
		t.Fatal(err)
	}
}
func TestRestartResumesConfirmationAndQueuePagingTies(t *testing.T) {
	m, base := fixture(t)
	ctx := context.Background()
	file(t, base, "novel.txt", []byte("第一章\n正文\n"))
	j := enqueue(t, m, "import", Input{Metadata: Metadata{Title: "书"}, Path: "novel.txt", Encoding: "utf-8"})
	m.RunOne(ctx)
	state(t, m, j.ID, "review")
	m.Action(ctx, j.ID, "confirm")
	m.DB.Write.Exec(`UPDATE library_jobs SET status='running' WHERE id=?`, j.ID)
	stop, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := m.Job(ctx, j.ID)
		if got.Status == "completed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	state(t, m, j.ID, "completed")
	stop()
	for i := 0; i < 45; i++ {
		if _, err := m.DB.Write.Exec(`INSERT INTO library_jobs(id,kind,dedupe,status,payload,created_at,updated_at) VALUES(?,'scan',?,'completed','{}',1,1)`, fmt.Sprintf("test-%02d", i), fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := m.Jobs(ctx, "")
	if err != nil || len(page) != 41 {
		t.Fatal(len(page), err)
	}
	next, err := m.Jobs(ctx, page[39].ID)
	if err != nil || len(next) != 6 {
		t.Fatal(len(next), err)
	}
	seen := map[string]bool{}
	for _, item := range append(page[:40], next...) {
		if seen[item.ID] {
			t.Fatal("duplicate job page")
		}
		seen[item.ID] = true
	}
	if len(seen) != 46 {
		t.Fatal(len(seen))
	}
}

func TestRunningCancellationAndHTTP200Challenge(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	began := make(chan struct{})
	finished := make(chan struct{})
	m.Remote.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		close(began)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	j := enqueue(t, m, "collect", Input{URL: "https://www.qidian.com/book/123/", MaxChapters: 1})
	go func() { m.RunOne(ctx); close(finished) }()
	<-began
	if err := m.Action(ctx, j.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(4 * time.Second):
		t.Fatal("cancelled request kept running")
	}
	state(t, m, j.ID, "cancelled")
	m.Remote.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return response(200, []byte(`<script>document.cookie='x-waf-captcha';</script>`)), nil
	})
	_, err := m.Remote.Get(ctx, "qidian", "https://m.qidian.com/book/123/", 2048)
	if _, ok := err.(*BlockedError); !ok {
		t.Fatalf("challenge not stopped: %v", err)
	}
	var next int64
	if err := m.DB.Read.QueryRow(`SELECT next_at FROM source_cooldowns WHERE source='qidian'`).Scan(&next); err != nil || next < time.Now().Add(5*time.Hour).UnixMilli() {
		t.Fatal("cooldown not durable", next, err)
	}
}
