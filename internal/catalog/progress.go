package catalog

// Paragraph and Character count Unicode scalar values, never UTF-16 units or
// encoded bytes. Block is a signed, version-bound source position.
type Anchor struct {
	Block     string `json:"block"`
	Paragraph int    `json:"paragraph"`
	Character int    `json:"character"`
}

type Position struct {
	ChapterIdx     int64   `json:"chapterIdx"`
	Percent        float64 `json:"percent"`
	ContentVersion string  `json:"contentVersion,omitempty"`
	Anchor         *Anchor `json:"anchor,omitempty"`
}

type Progress struct {
	Position
	Revision  int64 `json:"revision"`
	UpdatedAt int64 `json:"updatedAt"`
}

type ProgressMutation struct {
	Position
	BaseRevision int64  `json:"baseRevision"`
	MutationID   string `json:"mutationId"`
	DeviceID     string `json:"deviceId"`
	ClientSeq    int64  `json:"clientSeq"`
}
