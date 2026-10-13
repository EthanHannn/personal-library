// Package probe is the read-only M0 adapter and measurement harness.
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/EthanHannn/personal-library/internal/catalog"
	"github.com/EthanHannn/personal-library/internal/gateway"
	"github.com/EthanHannn/personal-library/internal/httpapi"
	"github.com/EthanHannn/personal-library/internal/legacy"
	"github.com/EthanHannn/personal-library/internal/reader"
)

type Manifest struct {
	Version int           `json:"version"`
	Files   []reader.File `json:"files"`
}

func LoadManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8*1024*1024+1))
	d.DisallowUnknownFields()
	var manifest Manifest
	if err := d.Decode(&manifest); err != nil {
		return manifest, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return manifest, fmt.Errorf("manifest has trailing data or exceeds limit")
	}
	if manifest.Version != 1 {
		return manifest, fmt.Errorf("unsupported manifest version")
	}
	return manifest, nil
}

type App struct{ api *httpapi.App }
type repository struct {
	*legacy.Store
	files map[string]reader.File
}

func (s repository) Chapter(ctx context.Context, id, idx int64) (catalog.Book, catalog.Chapter, reader.File, error) {
	b, c, err := s.Store.Chapter(ctx, id, idx)
	if err != nil {
		return b, c, reader.File{}, err
	}
	f, ok := s.files[strings.ReplaceAll(b.FilePath, "\\", "/")]
	if !ok {
		return b, c, f, catalog.ErrUnindexed
	}
	return b, c, f, nil
}

func New(s *legacy.Store, r *reader.Reader, m Manifest) (*App, error) {
	files := make(map[string]reader.File)
	if m.Version != 1 || len(m.Files) == 0 {
		return nil, fmt.Errorf("invalid file manifest")
	}
	for _, f := range m.Files {
		if f.Path == "" || f.Size < 0 || (f.Encoding != "utf-8" && f.Encoding != "gb18030") {
			return nil, fmt.Errorf("invalid file metadata")
		}
		f.Path = strings.ReplaceAll(f.Path, "\\", "/")
		if _, exists := files[f.Path]; exists {
			return nil, fmt.Errorf("duplicate manifest path")
		}
		files[f.Path] = f
	}
	api, err := httpapi.New(repository{s, files}, r)
	if err != nil {
		return nil, err
	}
	return &App{api}, nil
}

func (a *App) Handler(config gateway.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": "default-reader", "nickname": "我"}, "logoutUrl": config.AuthOrigin + "/logout", "mode": "read-only-probe"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { gateway.Error(w, 404, "not_found", "接口不存在") })
	return a.api.Handler(config, mux, "read-only-probe")
}
