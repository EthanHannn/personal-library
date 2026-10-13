package probe

import (
	"context"
	"fmt"
	"strings"

	"github.com/EthanHannn/personal-library/internal/legacy"
	"github.com/EthanHannn/personal-library/internal/reader"
)

func Verify(ctx context.Context, store *legacy.Store, r *reader.Reader, manifest Manifest) (legacy.Verification, error) {
	files := make(map[string]reader.File)
	for _, f := range manifest.Files {
		path := strings.ReplaceAll(f.Path, "\\", "/")
		if _, ok := files[path]; ok {
			return legacy.Verification{}, fmt.Errorf("duplicate manifest path")
		}
		files[path] = f
	}
	return store.Verify(ctx, func(path string) (int64, error) {
		path = strings.ReplaceAll(path, "\\", "/")
		f, ok := files[path]
		if !ok {
			return 0, fmt.Errorf("file missing from manifest")
		}
		actual, err := r.Inspect(path, f.Encoding)
		if err != nil {
			return 0, err
		}
		if actual.Size != f.Size || actual.ModifiedNS != f.ModifiedNS {
			return 0, reader.ErrChanged
		}
		return actual.Size, nil
	})
}
