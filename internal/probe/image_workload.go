package probe

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"os"
	"time"
)

// CreateCoverFixture makes a synthetic 4096x4096 image for the M0 resource test.
// It refuses to replace an existing file. This is test input, not a user cover.
func CreateCoverFixture(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, 4096, 4096))
	for y := range 4096 {
		for x := range 4096 {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255})
		}
	}
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
}

// ImageWorkload exercises one bounded background decoder at a time. It does not
// fetch network images, publish covers, write the database, or implement jobs.
// M3 must separately implement durable tasks, proper resizing and atomic files.
func ImageWorkload(ctx context.Context, path string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			started := time.Now()
			if err := DecodeCoverSample(ctx, path); err != nil {
				slog.Error("M0 image workload failed", "error", err)
			} else {
				slog.Info("M0 image workload completed", "elapsed", time.Since(started))
			}
		}
	}
}

func DecodeCoverSample(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 10*1024*1024 {
		return fmt.Errorf("image exceeds byte budget")
	}
	cfg, format, err := image.DecodeConfig(io.LimitReader(f, 10*1024*1024))
	if err != nil {
		return err
	}
	if (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16*1024*1024 {
		return fmt.Errorf("image exceeds pixel budget")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	img, _, err := image.Decode(io.LimitReader(f, 10*1024*1024))
	if err != nil {
		return err
	}
	for _, requested := range []int{160, 320, 480} {
		if err := ctx.Err(); err != nil {
			return err
		}
		width := min(requested, cfg.Width)
		height := max(1, cfg.Height*width/cfg.Width)
		thumb := image.NewRGBA(image.Rect(0, 0, width, height))
		bounds := img.Bounds()
		for y := range height {
			for x := range width {
				thumb.Set(x, y, img.At(bounds.Min.X+x*cfg.Width/width, bounds.Min.Y+y*cfg.Height/height))
			}
		}
		if err := jpeg.Encode(io.Discard, thumb, &jpeg.Options{Quality: 80}); err != nil {
			return err
		}
	}
	return nil
}
