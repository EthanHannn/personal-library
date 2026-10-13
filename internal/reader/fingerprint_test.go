package reader

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintUsesContainedExactPaths(t *testing.T) {
	dir := t.TempDir()
	data := []byte("中文正文😀")
	if err := os.WriteFile(filepath.Join(dir, "Book.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ref, err := r.Inspect("Book.txt", "utf-8")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := r.Fingerprint(context.Background(), ref)
	if err != nil || hash != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("%s %v", hash, err)
	}
	for _, path := range []string{"book.txt", "../Book.txt", "C:/Book.txt"} {
		bad := ref
		bad.Path = path
		if _, err := r.Fingerprint(context.Background(), bad); err == nil {
			t.Fatal("bad path accepted", path)
		}
	}
}
