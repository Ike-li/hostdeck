package diskhistory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func sparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var n uint32
	if err := windows.DeviceIoControl(windows.Handle(f.Fd()), windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &n, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
}

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
