package reader

import (
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"
)

func SupportedEncoding(encoding string) bool {
	return encoding == "utf-8" || encoding == "gb18030" || encoding == "utf-16le" || encoding == "utf-16be"
}

// Decode original UTF-16 bytes without rewriting files or chapter offsets.
// Like the other decoders, reject invalid input and never split a character.
func decodeUTF16(raw []byte, little bool, limit int, first, final bool) (string, int, error) {
	var order binary.ByteOrder = binary.BigEndian
	if little {
		order = binary.LittleEndian
	}
	n := 0
	if first && len(raw) >= 2 && order.Uint16(raw) == 0xfeff {
		n = 2
	}
	out := make([]byte, 0, min(limit, len(raw)*2))
	for n < len(raw) && len(out) < limit {
		if len(raw)-n < 2 {
			if final {
				return "", 0, ErrEncoding
			}
			break
		}
		ch, width := rune(order.Uint16(raw[n:])), 2
		if ch >= 0xdc00 && ch <= 0xdfff {
			return "", 0, ErrEncoding
		}
		if ch >= 0xd800 && ch <= 0xdbff {
			if len(raw)-n < 4 {
				if final {
					return "", 0, ErrEncoding
				}
				break
			}
			low := rune(order.Uint16(raw[n+2:]))
			if low < 0xdc00 || low > 0xdfff {
				return "", 0, ErrEncoding
			}
			ch, width = utf16.DecodeRune(ch, low), 4
		}
		if len(out)+utf8.RuneLen(ch) > limit {
			break
		}
		out = utf8.AppendRune(out, ch)
		n += width
	}
	return string(out), n, nil
}
