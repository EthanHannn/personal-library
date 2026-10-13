package reader

import (
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func FuzzDecodingBlocks(f *testing.F) {
	for _, seed := range []string{"", "中文𠀀😀\r\n末行", "\ufffd", "\u0080Àü", strings.Repeat("超长行。", 100)} {
		f.Add(seed, uint8(7))
	}
	f.Fuzz(func(t *testing.T, text string, size uint8) {
		if !utf8.ValidString(text) || len(text) > 8192 {
			t.Skip()
		}
		limit := 4 + int(size)
		for _, encoding := range []string{"utf-8", "gb18030"} {
			data := []byte(text)
			want := text
			if encoding == "gb18030" {
				var err error
				data, _, err = transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), data)
				if err != nil {
					continue
				}
				// x/text's encoder cannot round-trip all private-use code points
				// and predates newer GB18030 mappings. Use it to synthesize only
				// lossless inputs for this partition-invariance property. The
				// independent legacy-runtime reference hash covers those mappings.
				decoded, _, err := decode(data, encoding, MaxBlockBytes, false, true)
				if err != nil || decoded != text {
					continue
				}
			}
			var got strings.Builder
			for offset := 0; offset < len(data); {
				end := min(len(data), offset+limit+4)
				block, used, err := decode(data[offset:end], encoding, limit, false, end == len(data))
				if err != nil {
					t.Fatalf("%s: %x: %v", encoding, data, err)
				}
				if used <= 0 || used > end-offset || len(block) > limit || !utf8.ValidString(block) {
					t.Fatal("invalid block boundary")
				}
				got.WriteString(block)
				offset += used
			}
			if got.String() != want {
				t.Fatalf("%s: content changed: %q != %q", encoding, got.String(), want)
			}
		}
	})
}
