//go:build !windows

package observation

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAFileSourceNeverBlocksOnOrConsumesAPipe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir())
	id, _ := s.Create("jobs", "one", "", Target{Files: []string{path}})
	done := make(chan Snapshot, 1)
	go func() { got, _ := s.Read(context.Background(), id, "", Target{Files: []string{path}}); done <- got }()
	select {
	case got := <-done:
		if got.Sources[0].State != "not_regular" {
			t.Fatalf("pipe %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("opened a pipe for blocking consumption")
	}
}
func TestPermissionDenialKeepsThePreviouslyShownContent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	path := filepath.Join(t.TempDir(), "log")
	os.WriteFile(path, []byte("kept"), 0600)
	s := New(t.TempDir())
	target := Target{Files: []string{path}}
	id, _ := s.Create("jobs", "one", "start", target)
	s.Begin(id, "jobs", "one", "start")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("new")
	f.Close()
	first, _ := s.Read(context.Background(), id, "", target)
	os.Chmod(path, 0000)
	defer os.Chmod(path, 0600)
	next, err := s.Read(context.Background(), id, first.Cursor, target)
	if err != nil || next.Sources[0].State != "permission" || next.Log != "" {
		t.Fatalf("denied %+v %v", next, err)
	}
	reset, _ := s.Read(context.Background(), id, "", target)
	if reset.Log != "new" {
		t.Fatalf("lost content %+v", reset)
	}
}
