package observation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAReusedPIDCannotContinueAnObservation(t *testing.T) {
	original := Identity{PID: 42, Started: time.Unix(100, 0)}
	changed := Identity{PID: 42, Started: time.Unix(200, 0)}
	if original.Equal(changed) || original.Equal(Identity{PID: 42}) {
		t.Fatal("accepted reused or unverifiable PID")
	}
	s := New(t.TempDir())
	path := filepath.Join(t.TempDir(), "log")
	os.WriteFile(path, []byte("secret-new-process"), 0600)
	id, _ := s.Create("manual", "42", "", Target{Identity: original, State: "running"})
	got, err := s.Read(context.Background(), id, "", Target{Identity: changed, State: "running", Files: []string{path}})
	if err != nil || got.State != "unknown" || got.Log != "" || got.Sources[0].State != "identity_changed" {
		t.Fatalf("reused PID: %+v %v", got, err)
	}
}
func TestCopyTruncationThatRegrowsPastTheCursorResetsTheLog(t *testing.T) {
	s := New(t.TempDir())
	path := filepath.Join(t.TempDir(), "log")
	os.WriteFile(path, []byte("old content\n"), 0600)
	id, _ := s.Create("jobs", "one", "start", Target{Files: []string{path}})
	first, _ := s.Read(context.Background(), id, "", Target{Files: []string{path}})
	s.Begin(id, "jobs", "one", "start")
	os.WriteFile(path, []byte("replacement longer than the original file\n"), 0600)
	got, err := s.Read(context.Background(), id, first.Cursor, Target{Files: []string{path}})
	if err != nil || !got.Reset || !strings.Contains(got.Log, "replacement longer") {
		t.Fatalf("copytruncate: %+v %v", got, err)
	}
}
func TestOnlyRegularFileDescriptorsAreAcceptedAsProcessLogSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stdout.log")
	out := []byte("p42\nf1\ntREG\nn" + path + "\nf2\ntCHR\nn/dev/ttys001\nf0\ntREG\nn/tmp/input\n")
	got := descriptorFiles(out)
	if len(got) != 1 || got[0] != path {
		t.Fatalf("sources: %v", got)
	}
}

func TestMultiplePortsOfOneProcessShareTheSameSourceIdentity(t *testing.T) {
	s := New(t.TempDir())
	path := filepath.Join(t.TempDir(), "log")
	os.WriteFile(path, []byte("shared"), 0600)
	target := Target{Identity: Identity{PID: 42, Started: time.Unix(100, 0)}, Files: []string{path}}
	a, _ := s.Create("listeners", "42-one-80", "", target)
	b, _ := s.Create("listeners", "42-one-81", "", target)
	one, _ := s.Read(context.Background(), a, "", target)
	two, _ := s.Read(context.Background(), b, "", target)
	if one.Sources[0].ID == "" || one.Sources[0].ID != two.Sources[0].ID {
		t.Fatalf("sources not shared: %+v %+v", one, two)
	}
}
