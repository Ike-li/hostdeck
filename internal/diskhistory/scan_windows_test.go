package diskhistory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsJunctionsDoNotPullAnotherTreeIntoTheScan(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "local"), []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "outside"), []byte("123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, "junction"), external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %s %v", out, err)
	}
	s, err := collect(context.Background(), root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Sizes["."].Bytes != 3 || s.Sizes["."].Partial || len(s.Sizes) != 1 {
		t.Fatalf("junction was traversed: %+v", s)
	}
}
