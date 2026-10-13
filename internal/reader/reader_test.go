package reader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func fixture(t testing.TB, data []byte, encoding string) (*Reader, File) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "测试.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	ref, err := r.Inspect("测试.txt", encoding)
	if err != nil {
		t.Fatal(err)
	}
	return r, ref
}

func TestCharacterAlignedBlocks(t *testing.T) {
	want := "第一章\r\n\r\n　中文𠀀😀，标点。\n末行没有换行" + strings.Repeat("超长的一行𠀀", 100)
	for _, encoding := range []string{"utf-8", "utf-8-bom", "gb18030"} {
		t.Run(encoding, func(t *testing.T) {
			data := []byte(want)
			name := encoding
			if encoding == "utf-8-bom" {
				data = append([]byte{0xef, 0xbb, 0xbf}, data...)
				name = "utf-8"
			}
			if encoding == "gb18030" {
				var err error
				data, _, err = transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), data)
				if err != nil {
					t.Fatal(err)
				}
			}
			r, ref := fixture(t, data, name)
			for _, limit := range []int{4, 5, 7, 31, 256, MaxBlockBytes} {
				var got strings.Builder
				for offset := int64(0); offset < ref.Size; {
					block, err := r.Read(context.Background(), ref, 0, ref.Size, offset, limit)
					if err != nil {
						t.Fatalf("limit %d offset %d: %v", limit, offset, err)
					}
					if len(block.Text) > limit || !utf8.ValidString(block.Text) || block.End <= offset {
						t.Fatalf("invalid block: %+v", block)
					}
					got.WriteString(block.Text)
					if block.Next == nil {
						if block.End != ref.Size {
							t.Fatal("truncated file")
						}
						break
					}
					offset = *block.Next
				}
				if got.String() != want {
					t.Fatalf("limit %d: decoded text differs", limit)
				}
			}
		})
	}
}

func TestChapterRangeAndVersion(t *testing.T) {
	r, ref := fixture(t, []byte("前章\n本章𠀀\r\n后章"), "utf-8")
	start, end := int64(len("前章\n")), int64(len("前章\n本章𠀀\r\n"))
	block, err := r.Read(context.Background(), ref, start, end, start, 100)
	if err != nil || block.Text != "本章𠀀\r\n" || block.Next != nil {
		t.Fatalf("%+v %v", block, err)
	}
	for _, span := range [][3]int64{{-1, 2, 0}, {0, ref.Size + 1, 0}, {5, 4, 5}, {start, end, start - 1}, {start, end, end}} {
		_, err := r.Read(context.Background(), ref, span[0], span[1], span[2], 32)
		if !errors.Is(err, ErrRange) {
			t.Fatalf("range %v: %v", span, err)
		}
	}
	changed := ref
	changed.ModifiedNS--
	if _, err := r.Read(context.Background(), changed, 0, changed.Size, 0, 100); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	changed = ref
	changed.Size++
	if _, err := r.Read(context.Background(), changed, 0, changed.Size, 0, 100); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
}

func TestRejectInvalidEncoding(t *testing.T) {
	for _, tc := range []struct {
		encoding string
		data     []byte
	}{
		{"utf-8", []byte{0xff}}, {"utf-8", []byte{0xe4, 0xb8}},
		{"gb18030", []byte{0x81}}, {"gb18030", []byte{0x81, 0x30, 0x81}},
		{"gb18030", []byte{0x81, 0x7f}}, {"gb18030", []byte{0xfe, 0x39, 0xfe, 0x39}},
	} {
		r, ref := fixture(t, tc.data, tc.encoding)
		if _, err := r.Read(context.Background(), ref, 0, ref.Size, 0, 20); !errors.Is(err, ErrEncoding) {
			t.Fatalf("%x: %v", tc.data, err)
		}
	}
}

func TestGB18030AliasesAndReplacementCharacter(t *testing.T) {
	// WHATWG single-byte euro followed by an intentionally encoded U+FFFD.
	r, ref := fixture(t, []byte{0x80, 0x84, 0x31, 0xa4, 0x37}, "gb18030")
	block, err := r.Read(context.Background(), ref, 0, ref.Size, 0, 32)
	if err != nil || block.Text != "€\ufffd" {
		t.Fatalf("%q: %v", block.Text, err)
	}
}

func TestRejectEscapingPaths(t *testing.T) {
	r, ref := fixture(t, []byte("safe"), "utf-8")
	for _, name := range []string{"../other.txt", "..\\other.txt", "/etc/passwd", "C:\\secret.txt", "测试.txt:stream", "sub/../测试.txt", ".", ""} {
		bad := ref
		bad.Path = name
		if _, err := r.Read(context.Background(), bad, 0, bad.Size, 0, 16); !errors.Is(err, ErrPath) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.root.Name(), "escape.txt")); err != nil {
		t.Logf("symlink unavailable: %v", err)
		return
	}
	bad := ref
	bad.Path = "escape.txt"
	if _, err := r.Read(context.Background(), bad, 0, bad.Size, 0, 16); !errors.Is(err, ErrPath) {
		t.Fatalf("symlink escape: %v", err)
	}
}

func TestCancellationAndParallelRead(t *testing.T) {
	r, ref := fixture(t, []byte(strings.Repeat("并发读取", 10000)), "utf-8")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Read(ctx, ref, 0, ref.Size, 0, 32); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			block, err := r.Read(context.Background(), ref, 0, ref.Size, 0, 1024)
			if err != nil || !strings.HasPrefix(block.Text, "并发读取") {
				t.Errorf("%v", err)
			}
		})
	}
	wg.Wait()
	r.slots <- struct{}{}
	r.slots <- struct{}{}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := r.Read(ctx, ref, 0, ref.Size, 0, 32); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-r.slots
	<-r.slots
}

func BenchmarkReadBlock(b *testing.B) {
	r, ref := fixture(b, []byte(strings.Repeat("中文测试𠀀\n", 100000)), "utf-8")
	b.ReportAllocs()
	b.SetBytes(24 * 1024)
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Read(context.Background(), ref, 0, ref.Size, 0, 24*1024); err != nil {
			b.Fatal(err)
		}
	}
}
