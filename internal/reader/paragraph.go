package reader

import "strings"

const ParagraphRunes = 1000

type Paragraph struct {
	Start int
	Text  string
}

// Paragraphs preserves the original text and Unicode offsets. Very long lines
// are split deterministically so a single line cannot create an unbounded TTS
// utterance or a giant DOM paragraph. Keep web/src/reader/text.ts in agreement.
func Paragraphs(text string) []Paragraph {
	var out []Paragraph
	start := 0
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		for i := 0; i < len(runes); i += ParagraphRunes {
			part := string(runes[i:min(i+ParagraphRunes, len(runes))])
			if strings.TrimSpace(part) != "" {
				out = append(out, Paragraph{start + i, part})
			}
		}
		start += len(runes) + 1
	}
	return out
}
