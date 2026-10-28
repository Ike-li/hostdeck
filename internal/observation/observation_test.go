package observation

import (
	"context"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnObservationFollowsDelayedOutputAndResetsAfterRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.log")
	os.WriteFile(path, []byte("before\n"), 0600)
	s := New(filepath.Join(dir, "data"))
	id, err := s.Create("jobs", "demo", "start", Target{Files: []string{path}, State: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Read(context.Background(), id, "", Target{Files: []string{path}, State: "idle"})
	if err != nil || !first.Reset || !strings.Contains(first.Log, "before") {
		t.Fatalf("first: %+v %v", first, err)
	}
	if err = s.Begin(id, "jobs", "demo", "start"); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("after\n")
	f.Close()
	next, err := s.Read(context.Background(), id, first.Cursor, Target{Files: []string{path}, State: "idle", ExitCode: intp(0)})
	if err != nil || !strings.Contains(next.Log, "after") || strings.Contains(next.Log, "before") || next.ExitCode == nil {
		t.Fatalf("next: %+v %v", next, err)
	}
	os.Rename(path, path+".old")
	os.WriteFile(path, []byte("rotated\n"), 0600)
	last, err := s.Read(context.Background(), id, next.Cursor, Target{Files: []string{path}, State: "idle"})
	if err != nil || !last.Reset || !strings.Contains(last.Log, "rotated") {
		t.Fatalf("rotation: %+v %v", last, err)
	}
	if err = s.Finish(id, "returned", plugin.Text{All: "operation returned"}); err != nil {
		t.Fatal(err)
	}
	reopened := New(filepath.Join(dir, "data"))
	recent, err := reopened.Recent("jobs")
	if err != nil || len(recent) != 1 || recent[0].Message.All != "operation returned" || !strings.Contains(recent[0].Log, "rotated") {
		t.Fatalf("recent: %+v %v", recent, err)
	}
}

func TestASessionCannotAuthorizeADifferentActionOrRepeatOne(t *testing.T) {
	s := New(t.TempDir())
	id, err := s.Create("jobs", "one", "start", Target{State: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][3]string{{"other", "one", "start"}, {"jobs", "two", "start"}, {"jobs", "one", "stop"}} {
		if s.Begin(id, args[0], args[1], args[2]) == nil {
			t.Fatal("accepted mismatched action")
		}
	}
	if err = s.Begin(id, "jobs", "one", "start"); err != nil {
		t.Fatal(err)
	}
	if s.Begin(id, "jobs", "one", "start") == nil {
		t.Fatal("executed twice")
	}
}

func TestMissingEmptyAndNonRegularSourcesHaveDifferentReasons(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	os.WriteFile(empty, nil, 0600)
	for _, tc := range []struct {
		target Target
		want   string
	}{
		{Target{}, "unconfigured"}, {Target{Files: []string{empty}}, "empty"},
		{Target{Files: []string{empty + "missing"}}, "waiting"}, {Target{Files: []string{dir}}, "not_regular"},
	} {
		s := New(t.TempDir())
		id, _ := s.Create("jobs", "one", "", tc.target)
		got, err := s.Read(context.Background(), id, "", tc.target)
		if err != nil || len(got.Sources) != 1 || got.Sources[0].State != tc.want {
			t.Fatalf("%s: %+v %v", tc.want, got, err)
		}
	}
}
func intp(n int) *int { return &n }
