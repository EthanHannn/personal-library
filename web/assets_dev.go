//go:build !release

package web

import (
	"fmt"
	"io/fs"
)

func Assets() (fs.FS, error) {
	return nil, fmt.Errorf("frontend not embedded: build web/ and pass --web-dir web/dist, or compile with -tags release")
}
