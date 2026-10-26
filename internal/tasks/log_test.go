package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASecretSplitAcrossWritesAndTheCaptureBoundaryIsStillRedacted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "stream")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := newLog(f, []string{"abcdefghijklmnop"})
	w.Write([]byte(strings.Repeat("x", (1<<20)-7)))
	for _, b := range []byte("abcdefghijklmnop") {
		w.Write([]byte{b})
	}
	w.Write([]byte("tail-more"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != (1<<20) || !w.Truncated || strings.Contains(string(b), "abcdef") || !strings.Contains(string(b), "***") {
		t.Fatal("capture or cross-write redaction failed")
	}
}
