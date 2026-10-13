package probe

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestImageWorkloadBudgets(t *testing.T) {
	for _, size := range []image.Point{{X: 100, Y: 200}, {X: 1, Y: 8192}, {X: 8193, Y: 1}} {
		path := filepath.Join(t.TempDir(), "cover.png")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, size.X, size.Y))); err != nil {
			t.Fatal(err)
		}
		f.Close()
		err = DecodeCoverSample(context.Background(), path)
		if (size.X > 8192) != (err != nil) {
			t.Fatalf("size %v: %v", size, err)
		}
	}
	path := filepath.Join(t.TempDir(), "bad.jpg")
	if err := os.WriteFile(path, []byte("invalid image"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DecodeCoverSample(context.Background(), path); err == nil {
		t.Fatal("invalid image accepted")
	}
}
