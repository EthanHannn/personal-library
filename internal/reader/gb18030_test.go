package reader

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// Independently recorded from tools/m0/encoding-reference.mjs running inside
// the ec14e64 legacy image (Node v20.20.2, ICU 78.2). Covers every two-byte
// sequence and all 39,420 BMP four-byte pointers, including private-use areas
// and newer punctuation mappings absent from x/text's GBK table.
func TestGB18030MatchesLegacyReference(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		hash  string
	}{
		{"pairs", 23940, "85373408efe24c652c0a2f37997df8c657b0e8a654d07d9f9d97f1ad16609bc6"},
		{"quads", 39420, "efc8b18e905caa9e0ea4187852ab4d90bb671c8c722cca4d6bd4f58746eb3e00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data []byte
			for p := 0; p < tc.count; p++ {
				if tc.name == "pairs" {
					trail := p%190 + 0x40
					if trail >= 0x7f {
						trail++
					}
					data = append(data, byte(p/190+0x81), byte(trail))
				} else {
					n := p
					b4 := n%10 + 0x30
					n /= 10
					b3 := n%126 + 0x81
					n /= 126
					b2 := n%10 + 0x30
					n /= 10
					data = append(data, byte(n+0x81), byte(b2), byte(b3), byte(b4))
				}
			}
			for _, limit := range []int{7, MaxBlockBytes} {
				digest := sha256.New()
				for offset := 0; offset < len(data); {
					end := min(len(data), offset+limit+4)
					text, used, err := decode(data[offset:end], "gb18030", limit, false, end == len(data))
					if err != nil {
						t.Fatalf("offset %d, limit %d: %v", offset, limit, err)
					}
					if used <= 0 {
						t.Fatal("decoder made no progress")
					}
					digest.Write([]byte(text))
					offset += used
				}
				if got := hex.EncodeToString(digest.Sum(nil)); got != tc.hash {
					t.Fatalf("limit %d: %s", limit, got)
				}
			}
		})
	}
}
