package diskhistory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

func nativeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "history")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	config, _ := json.Marshal(map[string]any{"roots": []string{target}})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Sync(filepath.Dir(dir), nil) })
	return dir, target
}

func nativeCall(t *testing.T, dir string, args ...string) map[string]json.RawMessage {
	t.Helper()
	out, err := Execute(context.Background(), dir, append([]string{"disk-history"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(out, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func waitSaved(t *testing.T, dir string, check func(saved) bool) saved {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, err := load(dir)
		if err == nil && check(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for saved scan")
	return saved{}
}

func TestHourlyTrackingResumesAnOverdueScanAndPausesWithoutDeletingHistory(t *testing.T) {
	dir, target := nativeFixture(t)
	old := snapshot{Time: time.Now().Add(-3 * time.Hour), Sizes: map[string]measurement{".": {Bytes: 99}}}
	if err := save(dir, saved{Version: 1, Tracking: true, Scans: map[string][]snapshot{target: {old}}}); err != nil {
		t.Fatal(err)
	}
	Sync(filepath.Dir(dir), []plugin.Plugin{{Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "disk-history", "detail"}}}}})
	s := waitSaved(t, dir, func(s saved) bool { return len(s.Scans[target]) == 2 })
	if !s.Tracking || s.Scans[target][1].Sizes["."].Bytes != 0 {
		t.Fatalf("resumed state %+v", s)
	}
	if got := nativeCall(t, dir, "action", "pause", id(target)); got["error"] != nil {
		t.Fatalf("pause %s", got["error"])
	}
	s, err := load(dir)
	if err != nil || s.Tracking || len(s.Scans[target]) != 2 {
		t.Fatalf("paused state %+v, %v", s, err)
	}
	Sync(filepath.Dir(dir), nil)
	if got := nativeCall(t, dir, "detail"); got["error"] != nil || !strings.Contains(string(got["items"]), "UTC") {
		t.Fatalf("reopened %s", got)
	}
}

func TestAFailedScanKeepsThePreviousSnapshotAndReportsTheFailure(t *testing.T) {
	dir, target := nativeFixture(t)
	old := snapshot{Time: time.Now().UTC(), Sizes: map[string]measurement{".": {Bytes: 99}}}
	if err := save(dir, saved{Version: 1, Scans: map[string][]snapshot{target: {old}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, dir, "action", "scan", id(target)); got["error"] != nil {
		t.Fatalf("scan %s", got["error"])
	}
	s := waitSaved(t, dir, func(s saved) bool { return s.Error != "" })
	if len(s.Scans[target]) != 1 || s.Scans[target][0].Sizes["."].Bytes != 99 {
		t.Fatalf("failed scan replaced saved observation %+v", s)
	}
	if got := nativeCall(t, dir, "card"); string(got["status"]) != `"warn"` || !strings.Contains(string(got["label"]), "上次扫描失败") {
		t.Fatalf("failure card %s", got)
	}
}

func TestCorruptHistoryIsReportedAndNeverOverwrittenByAScan(t *testing.T) {
	dir, target := nativeFixture(t)
	if err := os.Mkdir(filepath.Join(dir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "data", "history.json")
	if err := os.WriteFile(file, []byte("broken history"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"card"}, {"detail"}, {"action", "scan", id(target)}} {
		if got := nativeCall(t, dir, args...); got["error"] == nil {
			t.Fatalf("corrupt history accepted: %s", got)
		}
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "broken history" {
		t.Fatalf("corrupt history was replaced: %s %v", got, err)
	}
}

func TestHistoryWithoutItsVersionIsRefusedAndARepairedFileCanBeRead(t *testing.T) {
	dir, _ := nativeFixture(t)
	if err := os.Mkdir(filepath.Join(dir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "data", "history.json")
	if err := os.WriteFile(file, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, dir, "card"); got["error"] == nil {
		t.Fatalf("unversioned state accepted: %s", got)
	}
	if err := save(dir, saved{Version: 1, Scans: map[string][]snapshot{}}); err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, dir, "card"); got["error"] != nil {
		t.Fatalf("repaired history still refused: %s", got)
	}
}

func TestReplacingThePluginFolderDoesNotReuseThePreviousFoldersHistory(t *testing.T) {
	dir, target := nativeFixture(t)
	old := snapshot{Time: time.Now().UTC(), Sizes: map[string]measurement{".": {Bytes: 99}}}
	if err := save(dir, saved{Version: 1, Scans: map[string][]snapshot{target: {old}}}); err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, dir, "card"); string(got["value"]) != `"99 B"` {
		t.Fatalf("old reading %s", got)
	}
	if err := os.Rename(dir, dir+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"roots": []string{target}})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	Sync(filepath.Dir(dir), []plugin.Plugin{{Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "disk-history", "detail"}}}}})
	if got := nativeCall(t, dir, "card"); string(got["value"]) != `"—"` {
		t.Fatalf("replacement inherited old reading: %s", got)
	}
	if got := nativeCall(t, dir, "action", "scan", id(target)); got["error"] != nil {
		t.Fatalf("replacement scan: %s", got)
	}
	waitSaved(t, dir, func(s saved) bool { return len(s.Scans[target]) == 1 })
}

func TestASecondWriterIsRefusedUntilTheOwnerReleasesTheHistory(t *testing.T) {
	dir, target := nativeFixture(t)
	if got := nativeCall(t, dir, "action", "scan", id(target)); got["error"] != nil {
		t.Fatalf("scan %s", got["error"])
	}
	waitSaved(t, dir, func(s saved) bool { return len(s.Scans[target]) == 1 })
	f, err := claim(dir)
	if err == nil {
		f.Close()
		t.Fatal("second history writer was accepted")
	}
	Sync(filepath.Dir(dir), nil)
	f, err = claim(dir)
	if err != nil {
		t.Fatalf("writer was not released: %v", err)
	}
	f.Close()
}

func TestNewScansPruneOldHistoryAndKeepTheSnapshotCountBounded(t *testing.T) {
	for _, mode := range []string{"age", "count"} {
		t.Run(mode, func(t *testing.T) {
			dir, target := nativeFixture(t)
			var scans []snapshot
			if mode == "age" {
				scans = []snapshot{{Time: time.Now().Add(-31 * 24 * time.Hour), Sizes: map[string]measurement{".": {Bytes: 99}}}}
			} else {
				for i := range maxScans {
					scans = append(scans, snapshot{Time: time.Now().Add(-time.Duration(maxScans-i) * time.Minute), Sizes: map[string]measurement{".": {Bytes: 99}}})
				}
			}
			first := scans[0].Time
			if err := save(dir, saved{Version: 1, Scans: map[string][]snapshot{target: scans}}); err != nil {
				t.Fatal(err)
			}
			if got := nativeCall(t, dir, "action", "scan", id(target)); got["error"] != nil {
				t.Fatalf("scan: %s", got)
			}
			s := waitSaved(t, dir, func(s saved) bool {
				current := s.Scans[target]
				return len(current) > 0 && current[len(current)-1].Sizes["."].Bytes == 0
			})
			want := 1
			if mode == "count" {
				want = maxScans
			}
			if len(s.Scans[target]) != want || !s.Scans[target][0].Time.After(first) {
				t.Fatalf("history was not pruned: %d scans, first %s", len(s.Scans[target]), s.Scans[target][0].Time)
			}
		})
	}
}
