// Package reader reads bounded, character-aligned slices of immutable novel files.
package reader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"
)

const MaxBlockBytes = 256 * 1024

// DecoderVersion participates in API content versions whenever decoding rules
// change, even when the original file's bytes and timestamps remain unchanged.
const DecoderVersion = "text-v2-legacy-icu78-utf16"

var (
	ErrPath     = errors.New("invalid novel path")
	ErrRange    = errors.New("invalid chapter range")
	ErrChanged  = errors.New("source file changed; rebuild its index")
	ErrEncoding = errors.New("invalid or unsupported text encoding")
)

// File describes the immutable file version observed by an indexer. ModifiedNS
// uses nanoseconds since the Unix epoch; it is not a JavaScript numeric timestamp.
type File struct {
	Path       string `json:"path"`
	Encoding   string `json:"encoding"`
	Size       int64  `json:"size"`
	ModifiedNS int64  `json:"modifiedNs,string"`
	SHA256     string `json:"sha256,omitempty"`
}

type Block struct {
	Text  string `json:"text"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	Next  *int64 `json:"next"`
}

type Reader struct {
	root    *os.Root
	managed *os.Root
	slots   chan struct{}
}

func Open(root string, concurrency int, managed ...string) (*Reader, error) {
	if concurrency < 1 || concurrency > 16 {
		return nil, fmt.Errorf("reader concurrency must be between 1 and 16")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	result := &Reader{root: r, slots: make(chan struct{}, concurrency)}
	if len(managed) > 0 && managed[0] != "" {
		result.managed, err = os.OpenRoot(managed[0])
		if err != nil {
			r.Close()
			return nil, err
		}
	}
	return result, nil
}

func (r *Reader) Close() error {
	if r.managed != nil {
		defer r.managed.Close()
	}
	return r.root.Close()
}

// Collected files use a reserved virtual directory; original novels stay read-only.
func (r *Reader) source(name string) (*os.Root, string, error) {
	name, err := cleanPath(name)
	if err != nil {
		return nil, "", err
	}
	if strings.HasPrefix(name, "_collected/") && r.managed != nil {
		return r.managed, strings.TrimPrefix(name, "_collected/"), nil
	}
	return r.root, name, nil
}

func (r *Reader) OpenSource(name string) (*os.File, error) {
	root, relative, err := r.source(name)
	if err != nil {
		return nil, err
	}
	return root.Open(relative)
}

// DecodeLine shares the reader's encoding rules with the streaming indexer.
func DecodeLine(raw []byte, encoding string) (string, error) {
	text, _, err := decode(raw, encoding, len(raw)*4+4, true, true)
	return text, err
}

func cleanPath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, ":\x00") {
		return "", ErrPath
	}
	return name, nil
}

// Inspect captures metadata only. Encoding must be chosen during import, after
// inspecting the entire source or by an explicit user decision.
func (r *Reader) Inspect(name, encoding string) (File, error) {
	if !SupportedEncoding(encoding) {
		return File{}, ErrEncoding
	}
	name, err := cleanPath(name)
	if err != nil {
		return File{}, err
	}
	f, err := r.OpenSource(name)
	if err != nil {
		return File{}, fmt.Errorf("%w: %w", ErrPath, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, ErrPath
	}
	return File{Path: name, Encoding: encoding, Size: info.Size(), ModifiedNS: info.ModTime().UnixNano()}, nil
}

// Read returns at most limit bytes of decoded UTF-8 without splitting a rune.
// offset must be the chapter start or a Next value returned for this file and
// chapter. API callers must authenticate cursors rather than accept arbitrary
// offsets: the middle of a GB18030 character can also look like a valid start.
// The file must stay immutable for the lifetime of its index. Size/mtime checks
// catch ordinary replacements; import verification must additionally hash files.
func (r *Reader) Read(ctx context.Context, ref File, start, end, offset int64, limit int) (Block, error) {
	if start < 0 || start >= end || end > ref.Size || offset < start || offset >= end || limit < 4 || limit > MaxBlockBytes {
		return Block{}, ErrRange
	}
	if (ref.Encoding == "utf-16le" || ref.Encoding == "utf-16be") && (start%2 != 0 || end%2 != 0 || offset%2 != 0) {
		return Block{}, ErrEncoding
	}
	if err := ctx.Err(); err != nil {
		return Block{}, err
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-ctx.Done():
		return Block{}, ctx.Err()
	}
	name, err := cleanPath(ref.Path)
	if err != nil {
		return Block{}, err
	}
	f, err := r.OpenSource(name)
	if err != nil {
		return Block{}, fmt.Errorf("%w: %w", ErrPath, err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Block{}, err
	}
	if !before.Mode().IsRegular() {
		return Block{}, ErrPath
	}
	if !matches(before, ref) {
		return Block{}, ErrChanged
	}

	// Four lookahead bytes cover all supported code points and an initial BOM.
	want := min(int64(limit+4), end-offset)
	raw := make([]byte, int(want))
	n, err := f.ReadAt(raw, offset)
	if err != nil && err != io.EOF {
		return Block{}, err
	}
	if int64(n) != want {
		return Block{}, ErrChanged
	}
	text, used, err := decode(raw, ref.Encoding, limit, offset == 0, offset+want == end)
	if err != nil {
		return Block{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return Block{}, err
	}
	if !matches(after, ref) {
		return Block{}, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return Block{}, err
	}
	if used == 0 {
		return Block{}, ErrEncoding
	}
	block := Block{Text: text, Start: offset, End: offset + int64(used)}
	if block.End < end {
		next := block.End
		block.Next = &next
	}
	return block, nil
}

func matches(info fs.FileInfo, ref File) bool {
	return info.Size() == ref.Size && info.ModTime().UnixNano() == ref.ModifiedNS
}

func decode(raw []byte, encoding string, limit int, first, final bool) (string, int, error) {
	skip := 0
	if encoding == "utf-8" && first && bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		skip = 3
	}
	src := raw[skip:]
	switch encoding {
	case "utf-16le", "utf-16be":
		return decodeUTF16(raw, encoding == "utf-16le", limit, first, final)
	case "utf-8":
		n := 0
		for n < len(src) && n < limit {
			if !utf8.FullRune(src[n:]) {
				if final {
					return "", 0, ErrEncoding
				}
				break
			}
			ch, size := utf8.DecodeRune(src[n:])
			if ch == utf8.RuneError && size == 1 {
				return "", 0, ErrEncoding
			}
			if n+size > limit {
				break
			}
			n += size
		}
		return string(src[:n]), n + skip, nil
	case "gb18030":
		// Validate only the prefix we may emit. Budget conservatively by the
		// maximum UTF-8 size of each source character (3 for pairs, 4 for quads).
		n, budget := 0, 0
		for n < len(src) && budget < limit {
			width, cost, err := gbCharacter(src[n:])
			if err == io.ErrUnexpectedEOF && !final {
				break
			}
			if err != nil {
				return "", 0, ErrEncoding
			}
			if budget+cost > limit {
				break
			}
			n += width
			budget += cost
		}
		decoded, err := decodeGB(src[:n], limit)
		if err != nil || !utf8.Valid(decoded) {
			return "", 0, ErrEncoding
		}
		// Reject replacement introduced by decoding an unmapped sequence. A
		// whole-buffer encode/decode round trip is not a validity test: GB18030
		// has aliases, including the WHATWG-compatible single-byte euro sign.
		if bytes.Contains(decoded, []byte("\ufffd")) {
			input, output := 0, 0
			for input < n {
				width, _, _ := gbCharacter(src[input:n])
				ch, size := utf8.DecodeRune(decoded[output:])
				if ch == utf8.RuneError && !bytes.Equal(src[input:input+width], []byte{0x84, 0x31, 0xa4, 0x37}) {
					return "", 0, ErrEncoding
				}
				input += width
				output += size
			}
			if output != len(decoded) {
				return "", 0, ErrEncoding
			}
		}
		return string(decoded), n, nil
	default:
		return "", 0, ErrEncoding
	}
}

func gbCharacter(src []byte) (width, maxUTF8 int, err error) {
	if src[0] <= 0x7f {
		return 1, 1, nil
	}
	// Match the previous TextDecoder('gb18030') WHATWG behavior.
	if src[0] == 0x80 {
		return 1, 3, nil
	}
	if src[0] < 0x81 || src[0] > 0xfe {
		return 0, 0, ErrEncoding
	}
	if len(src) < 2 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	if src[1] >= 0x40 && src[1] <= 0xfe && src[1] != 0x7f {
		return 2, 3, nil
	}
	if src[1] < 0x30 || src[1] > 0x39 {
		return 0, 0, ErrEncoding
	}
	if len(src) < 4 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	if src[2] < 0x81 || src[2] > 0xfe || src[3] < 0x30 || src[3] > 0x39 {
		return 0, 0, ErrEncoding
	}
	return 4, 4, nil
}
