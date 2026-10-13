// Package catalog defines the shared reader contract for legacy probes and the
// business database. Storage details and file paths never appear in JSON.
package catalog

import "errors"

var (
	ErrNotFound  = errors.New("book or chapter not found")
	ErrAmbiguous = errors.New("duplicate chapter index")
	ErrUnindexed = errors.New("unverified file version")
)

type Book struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	ChapterCount int64  `json:"chapterCount"`
	FilePath     string `json:"-"`
}

type Chapter struct {
	Index int64  `json:"idx"`
	Title string `json:"title"`
	Start int64  `json:"-"`
	End   int64  `json:"-"`
}
