package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestAFailedWriteLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "layout.json")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("{}")); err == nil {
		t.Fatal("Write over a directory succeeded")
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: stat err = %v", err)
	}
}

func TestWriteSyncsTheTempFileBeforeReplacing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "layout.json")
	var synced atomic.Bool
	prev := syncFile
	syncFile = func(f *os.File) error {
		synced.Store(true)
		return prev(f)
	}
	t.Cleanup(func() { syncFile = prev })

	if err := Write(target, []byte(`{"order":[]}`)); err != nil {
		t.Fatal(err)
	}
	if !synced.Load() {
		t.Fatal("Write replaced the file without Sync on the temp file")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"order":[]}` {
		t.Fatalf("got %q", got)
	}
}

func TestAFailedSyncLeavesNoTempFileAndKeepsTheOriginal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ports.json")
	if err := os.WriteFile(target, []byte(`{"old":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := syncFile
	syncFile = func(f *os.File) error {
		return errors.New("sync failed")
	}
	t.Cleanup(func() { syncFile = prev })

	if err := Write(target, []byte(`{"new":true}`)); err == nil {
		t.Fatal("Write succeeded despite Sync failure")
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind after Sync failure: stat err = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"old":true}` {
		t.Fatalf("original replaced after Sync failure: got %q", got)
	}
}
