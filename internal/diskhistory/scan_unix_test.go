//go:build !windows

package diskhistory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, size); err != nil {
		t.Fatal(err)
	}
}

func TestAHardLinkDoesNotCountTheSameFileTwice(t *testing.T) {
	root, exclude := t.TempDir(), t.TempDir()
	for _, dir := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, "a", "file")
	if err := os.WriteFile(file, []byte(strings.Repeat("a", 64<<10)), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := collect(context.Background(), root, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, filepath.Join(root, "b", "link")); err != nil {
		t.Fatal(err)
	}
	after, err := collect(context.Background(), root, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if before.Sizes["."].Bytes == 0 || after.Sizes["."].Bytes != before.Sizes["."].Bytes {
		t.Fatalf("root was %d bytes and %d with a second name for the same file", before.Sizes["."].Bytes, after.Sizes["."].Bytes)
	}
}
