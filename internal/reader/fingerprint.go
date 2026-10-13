package reader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// Fingerprint streams the immutable source, using the same containment boundary
// as Read. Exact path casing is required even on Windows for portable imports.
func (r *Reader) Fingerprint(ctx context.Context, ref File) (string, error) {
	name, err := cleanPath(ref.Path)
	if err != nil {
		return "", err
	}
	parent := "."
	for _, part := range strings.Split(name, "/") {
		dir, err := r.root.Open(parent)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrPath, err)
		}
		found := false
		for !found {
			entries, err := dir.ReadDir(128)
			for _, entry := range entries {
				if entry.Name() == part {
					found = true
					break
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				dir.Close()
				return "", err
			}
		}
		dir.Close()
		if !found {
			return "", fmt.Errorf("%w: exact path casing required", ErrPath)
		}
		if parent == "." {
			parent = part
		} else {
			parent += "/" + part
		}
	}
	observed, err := r.Inspect(name, ref.Encoding)
	if err != nil {
		return "", err
	}
	expected := ref
	expected.Path = name
	if observed != expected {
		return "", ErrChanged
	}
	f, err := r.root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		hash.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	observed, err = r.Inspect(name, ref.Encoding)
	if err != nil {
		return "", err
	}
	if observed != expected || stat.Size() != ref.Size || stat.ModTime().UnixNano() != ref.ModifiedNS {
		return "", ErrChanged
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
