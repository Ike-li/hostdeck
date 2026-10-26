package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/process"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--internal-task-worker" {
		os.Exit(process.Worker())
	}
	if len(os.Args) == 3 && os.Args[1] == "--task-owner-probe" {
		dir := os.Args[2]
		p := plugin.Plugin{ID: "scheduled-tasks", Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "tasks", "detail"}}}}
		if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
			os.Exit(2)
		}
		b, err := tasks.Execute(context.Background(), dir, []string{"tasks", "action", "run", "sample"})
		if err != nil || strings.Contains(string(b), `"error"`) {
			os.Exit(3)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestTaskCommand(t *testing.T) {
	mode := os.Getenv("HOSTDECK_TASK_MODE")
	if mode == "" {
		return
	}
	if mode == "block" {
		fmt.Println("started")
		d := time.Hour
		if v := os.Getenv("HOSTDECK_TASK_BLOCK_DURATION"); v != "" {
			d, _ = time.ParseDuration(v)
		}
		time.Sleep(d)
	}
	if marker := os.Getenv("HOSTDECK_TASK_MARKER"); marker != "" {
		if err := os.WriteFile(marker, []byte("ran"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "gate" {
		until := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(os.Getenv("HOSTDECK_TASK_RELEASE")); err == nil {
				break
			}
			if time.Now().After(until) {
				t.Fatal("release never arrived")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "failed fixture")
		os.Exit(7)
	}
	fmt.Fprint(os.Stdout, "hello 中文 ")
	secret := os.Getenv("HOSTDECK_TASK_SECRET")
	for _, c := range []byte(secret) {
		os.Stdout.Write([]byte{c})
	}
	fmt.Fprintln(os.Stderr, "stderr fixture")
	if mode == "large" {
		fmt.Fprint(os.Stdout, strings.Repeat("x", 2<<20))
	}
}

func readState(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "data/state.json"))
	// Windows can briefly refuse a reader while the writer atomically replaces
	// this file. Retry only that OS boundary, retaining the bounded failure.
	until := time.Now().Add(2 * time.Second)
	for runtime.GOOS == "windows" && (errors.Is(err, syscall.Errno(32)) || os.IsPermission(err)) && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
		b, err = os.ReadFile(filepath.Join(dir, "data/state.json"))
	}
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func saveState(t *testing.T, dir string, s map[string]any) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data/state.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryMarksAnUnfinishedRunInterruptedAndRequiresExplicitResume(t *testing.T) {
	dir, p := fixture(t, "success", "5s")
	operation(t, dir, "action", "run", "sample")
	r := waitRecord(t, dir, "succeeded")
	if err := tasks.Close(context.Background(), filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	s := readState(t, dir)
	record := s["runs"].([]any)[0].(map[string]any)
	record["status"] = "starting"
	record["started_at"] = nil
	record["finished_at"] = nil
	record["exit_code"] = nil
	s["active"] = map[string]any{r["id"].(string): map[string]any{"pid": 0, "token": r["id"]}}
	saveState(t, dir, s)
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	waitRecord(t, dir, "interrupted")
	if v := operation(t, dir, "action", "run", "sample"); v["error"] == nil {
		t.Fatal("interrupted task ran without Resume")
	}
	if v := operation(t, dir, "action", "resume", "sample"); v["error"] != nil {
		t.Fatalf("resume %+v", v)
	}
	if len(readState(t, dir)["runs"].([]any)) != 1 {
		t.Fatal("Resume replayed a run")
	}
	if v := operation(t, dir, "action", "run", "sample"); v["error"] != nil {
		t.Fatalf("run after resume %+v", v)
	}
}

func TestASecondConsoleCannotWriteAndLosingTheOwnerCleansItsWorker(t *testing.T) {
	dir, p := fixture(t, "block", "30s")
	if err := tasks.Close(context.Background(), filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOSTDECK_TASK_BLOCK_DURATION", "10s")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "--task-owner-probe", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	r := waitRecord(t, dir, "running")
	s := readState(t, dir)
	raw := s["active"].(map[string]any)[r["id"].(string)]
	b, _ := json.Marshal(raw)
	var identity process.Identity
	json.Unmarshal(b, &identity)
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	if v := operation(t, dir, "action", "run", "sample"); v["error"] == nil {
		t.Fatal("second writer accepted")
	}
	if len(readState(t, dir)["runs"].([]any)) != 1 {
		t.Fatal("second console wrote history")
	}
	cmd.Process.Kill()
	cmd.Wait()
	until := time.Now().Add(2 * time.Second)
	for !process.Gone(identity) {
		if time.Now().After(until) {
			t.Fatal("owner exit left its worker tree running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	waitRecord(t, dir, "interrupted")
}

func blockStateWrites(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "data/state.tmp")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "held"), []byte("held"), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestARefusedStartRecordRunsNothingAndKeepsThePreviousState(t *testing.T) {
	dir, _ := fixture(t, "success", "5s")
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "succeeded")
	prior, err := os.ReadFile(filepath.Join(dir, "data/state.json"))
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	t.Setenv("HOSTDECK_TASK_MARKER", marker)
	blockStateWrites(t, dir)
	if v := operation(t, dir, "action", "run", "sample"); v["error"] == nil {
		t.Fatal("run accepted without a durable start")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("script ran despite refused state write")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
	if string(after) != string(prior) {
		t.Fatal("refused start changed durable history")
	}
}

func TestAnUndurableCompletionIsNeverDisplayedAsSuccess(t *testing.T) {
	dir, _ := fixture(t, "gate", "5s")
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv("HOSTDECK_TASK_RELEASE", release)
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "running")
	blockStateWrites(t, dir)
	os.WriteFile(release, []byte("go"), 0600)
	until := time.Now().Add(5 * time.Second)
	for {
		v := operation(t, dir, "detail")
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "_diagnostic") {
			if strings.Contains(string(b), `"en":"Succeeded"`) {
				t.Fatalf("reported undurable success: %s", b)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("no storage failure appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCorruptStateIsRefusedWithoutRepairingOrRunningIt(t *testing.T) {
	dir, _ := fixture(t, "success", "5s")
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "succeeded")
	seed := readState(t, dir)
	encoded, _ := json.Marshal(seed)
	for name, change := range map[string]func(map[string]any){
		"null pause": func(s map[string]any) { s["tasks"].(map[string]any)["sample"].(map[string]any)["paused"] = nil },
		"unknown reason": func(s map[string]any) {
			r := s["runs"].([]any)[0].(map[string]any)
			r["status"] = "failed"
			r["reason"] = "invented"
		},
		"nonzero success": func(s map[string]any) { s["runs"].([]any)[0].(map[string]any)["exit_code"] = float64(7) },
		"missing start":   func(s map[string]any) { s["runs"].([]any)[0].(map[string]any)["started_at"] = nil },
		"bad worker": func(s map[string]any) {
			s["active"] = map[string]any{"absent": map[string]any{"pid": 123, "token": "absent"}}
		},
		"escaped log": func(s map[string]any) {
			s["runs"].([]any)[0].(map[string]any)["logs"].(map[string]any)["stdout"] = "../private"
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, p := fixture(t, "success", "5s")
			var s map[string]any
			json.Unmarshal(encoded, &s)
			change(s)
			if err := os.MkdirAll(filepath.Join(dir, "data"), 0700); err != nil {
				t.Fatal(err)
			}
			saveState(t, dir, s)
			before, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
			tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p})
			if v := operation(t, dir, "action", "run", "sample"); v["error"] == nil {
				t.Fatalf("corruption accepted: %+v", v)
			}
			after, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
			if string(before) != string(after) {
				t.Fatal("corrupt state was overwritten")
			}
		})
	}
}

func TestRetentionBoundsHistoryAndExpiresOldLogsBeforeReservingNewOutput(t *testing.T) {
	dir, p := fixture(t, "large", "5s")
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "succeeded")
	if err := tasks.Close(context.Background(), filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	s := readState(t, dir)
	seed := s["runs"].([]any)[0].(map[string]any)
	runs := []any{}
	for i := 0; i < 64; i++ {
		b, _ := json.Marshal(seed)
		var r map[string]any
		json.Unmarshal(b, &r)
		id := fmt.Sprintf("history-%04d", i)
		path := "data/logs/" + id + ".stdout.log"
		r["id"] = id
		r["logs"] = map[string]any{"stdout": path, "stderr": nil, "stdout_truncated": false, "stderr_truncated": false}
		f, err := os.OpenFile(filepath.Join(dir, path), os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(1 << 20); err != nil {
			t.Fatal(err)
		}
		f.Close()
		runs = append(runs, r)
	}
	s["runs"] = runs
	saveState(t, dir, s)
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	if v := operation(t, dir, "action", "run", "sample"); v["error"] != nil {
		t.Fatalf("reserve failed %+v", v)
	}
	until := time.Now().Add(10 * time.Second)
	for {
		state := readState(t, dir)
		records := state["runs"].([]any)
		last := records[len(records)-1].(map[string]any)
		if last["status"] == "succeeded" {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("new result %+v", last)
		}
		time.Sleep(10 * time.Millisecond)
	}
	s = readState(t, dir)
	for i := 0; i < 2; i++ {
		r := s["runs"].([]any)[i].(map[string]any)
		if r["logs"].(map[string]any)["stdout"] != nil {
			t.Fatalf("old log not expired %+v", r)
		}
	}
	files, err := os.ReadDir(filepath.Join(dir, "data/logs"))
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	if total > 64<<20 {
		t.Fatalf("physical log bytes %d exceed cap", total)
	}
}

func TestResumeRefusesALiveOldProcessTreeWithoutKillingIt(t *testing.T) {
	dir, p := fixture(t, "success", "5s")
	operation(t, dir, "action", "run", "sample")
	r := waitRecord(t, dir, "succeeded")
	tasks.Close(context.Background(), filepath.Dir(dir))
	t.Setenv("HOSTDECK_TASK_MODE", "block")
	t.Setenv("HOSTDECK_TASK_BLOCK_DURATION", "10s")
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestTaskCommand$")
	tree, err := process.NewTree(cmd, r["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { tree.Close(); cmd.Wait() }()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := tree.Started(); err != nil {
		t.Fatal(err)
	}
	s := readState(t, dir)
	record := s["runs"].([]any)[0].(map[string]any)
	record["status"] = "running"
	record["finished_at"] = nil
	record["exit_code"] = nil
	s["active"] = map[string]any{r["id"].(string): map[string]any{"pid": cmd.Process.Pid, "token": r["id"]}}
	saveState(t, dir, s)
	tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p})
	if v := operation(t, dir, "action", "resume", "sample"); v["error"] == nil {
		t.Fatal("live process tree was declared clear")
	}
	if process.Gone(process.Identity{PID: cmd.Process.Pid, Token: r["id"].(string)}) {
		t.Fatal("recovery killed the old process")
	}
	tree.Stop()
	cmd.Wait()
	if v := operation(t, dir, "action", "resume", "sample"); v["error"] != nil {
		t.Fatalf("cleared tree could not resume: %+v", v)
	}
}

func TestReplacingAPluginFolderRetiresTheOldWorkerBeforeLoadingTheReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses moving an owned directory; normal moves use Suspend")
	}
	dir, p := fixture(t, "gate", "5s")
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv("HOSTDECK_TASK_RELEASE", release)
	operation(t, dir, "action", "run", "sample")
	oldRun := waitRecord(t, dir, "running")["id"]
	old := filepath.Join(t.TempDir(), "retired")
	if err := os.Rename(dir, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(old, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "tasks.json"), b, 0600)
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(release, []byte("go"), 0600)
	if v := operation(t, dir, "action", "run", "sample"); v["error"] != nil {
		t.Fatalf("replacement kept the retired worker: %+v", v)
	}
	waitRecord(t, dir, "succeeded")
	for _, raw := range readState(t, dir)["runs"].([]any) {
		if raw.(map[string]any)["id"] == oldRun {
			t.Fatal("old worker published into replacement")
		}
	}
}

func TestHistoryKeepsAtMostAThousandRunsAndThirtyDays(t *testing.T) {
	dir, p := fixture(t, "success", "5s")
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "succeeded")
	tasks.Close(context.Background(), filepath.Dir(dir))
	s := readState(t, dir)
	seed := s["runs"].([]any)[0].(map[string]any)
	oldID := seed["id"].(string)
	oldTime := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	seed["accepted_at"] = oldTime
	seed["started_at"] = oldTime
	seed["finished_at"] = oldTime
	runs := []any{seed}
	for i := 1; i < 1000; i++ {
		b, _ := json.Marshal(seed)
		var r map[string]any
		json.Unmarshal(b, &r)
		r["id"] = fmt.Sprintf("history-%04d", i)
		stamp := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
		r["accepted_at"] = stamp
		r["started_at"] = stamp
		r["finished_at"] = stamp
		r["logs"] = map[string]any{"stdout": nil, "stderr": nil, "stdout_truncated": false, "stderr_truncated": false}
		runs = append(runs, r)
	}
	s["runs"] = runs
	saveState(t, dir, s)
	tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p})
	if v := operation(t, dir, "action", "run", "sample"); v["error"] != nil {
		t.Fatalf("run %+v", v)
	}
	until := time.Now().Add(10 * time.Second)
	for {
		s = readState(t, dir)
		records := s["runs"].([]any)
		last := records[len(records)-1].(map[string]any)
		if last["status"] == "succeeded" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("new run did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(s["runs"].([]any)); n > 1000 {
		t.Fatalf("retained %d records", n)
	}
	for _, raw := range s["runs"].([]any) {
		if raw.(map[string]any)["id"] == oldID {
			t.Fatal("forty-day-old run retained")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "data/logs", oldID+".stdout.log")); !os.IsNotExist(err) {
		t.Fatalf("old log retained: %v", err)
	}
}

func TestARefusedHistoryPruneKeepsDurableLogReferencesAndFiles(t *testing.T) {
	dir, p := fixture(t, "success", "5s")
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "succeeded")
	tasks.Close(context.Background(), filepath.Dir(dir))
	s := readState(t, dir)
	record := s["runs"].([]any)[0].(map[string]any)
	stamp := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	record["accepted_at"] = stamp
	record["started_at"] = stamp
	record["finished_at"] = stamp
	saveState(t, dir, s)
	before, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
	blockStateWrites(t, dir)
	tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p})
	if v := operation(t, dir, "action", "run", "sample"); v["error"] == nil {
		t.Fatal("prune succeeded without a state write")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
	if string(before) != string(after) {
		t.Fatal("refused prune changed state")
	}
	if _, err := os.Stat(filepath.Join(dir, record["logs"].(map[string]any)["stdout"].(string))); err != nil {
		t.Fatalf("durably referenced log was deleted: %v", err)
	}
}

func TestCorruptionDuringARunIsNotOverwrittenByItsCompletion(t *testing.T) {
	dir, _ := fixture(t, "gate", "5s")
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv("HOSTDECK_TASK_RELEASE", release)
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "running")
	bad := []byte(`{"version":1,"tasks":null,"runs":[],"active":{}}`)
	if err := os.WriteFile(filepath.Join(dir, "data/state.json"), bad, 0600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(release, []byte("go"), 0600)
	until := time.Now().Add(5 * time.Second)
	for {
		v := operation(t, dir, "detail")
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "_diagnostic") {
			if strings.Contains(string(b), `"en":"Succeeded"`) {
				t.Fatal("corruption was reported as success")
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("corrupt durable state was not reported")
		}
		time.Sleep(10 * time.Millisecond)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
	if string(after) != string(bad) {
		t.Fatal("completion overwrote corrupt state")
	}
}

func fixture(t *testing.T, mode, timeout string, options ...tasks.Option) (string, plugin.Plugin) {
	t.Helper()
	t.Setenv("HOSTDECK_TASK_MODE", mode)
	t.Setenv("HOSTDECK_TASK_SECRET", "private-fixture-value")
	root := t.TempDir()
	dir := filepath.Join(root, "scheduled-tasks")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"version": 1, "tasks": []any{map[string]any{"id": "sample", "name": "Sample 中文", "enabled": true, "schedule": map[string]any{"every": "1m"}, "exec": map[string]any{"command": []string{os.Args[0], "-test.run=^TestTaskCommand$"}, "env": map[string]string{"TOKEN": "${env:HOSTDECK_TASK_SECRET}"}}, "timeout": timeout}}}
	b, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	p := plugin.Plugin{ID: "scheduled-tasks", Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "tasks", "detail"}}}}
	tasks.Sync(root, []plugin.Plugin{p}, options...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tasks.Close(ctx, root); err != nil {
			t.Error(err)
		}
	})
	return dir, p
}

func operation(t *testing.T, dir string, args ...string) map[string]any {
	t.Helper()
	b, err := tasks.Execute(context.Background(), dir, append([]string{"tasks"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func waitRecord(t *testing.T, dir, status string) map[string]any {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		b, err := os.ReadFile(filepath.Join(dir, "data", "state.json"))
		if err == nil {
			var s struct{ Runs []map[string]any }
			if json.Unmarshal(b, &s) == nil {
				for _, r := range s.Runs {
					if r["status"] == status {
						return r
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "data", "state.json"))
	t.Fatalf("no %s record: %s", status, b)
	return nil
}

func TestAManualRunRecordsItsResultAndKeepsSecretsOutOfLogs(t *testing.T) {
	dir, _ := fixture(t, "success", "5s")
	if v := operation(t, dir, "action", "run", "sample"); v["error"] != nil {
		t.Fatalf("run refused %+v", v)
	}
	r := waitRecord(t, dir, "succeeded")
	if r["trigger"] != "manual" || r["scheduled_at"] != nil || r["exit_code"] != float64(0) || r["reason"] != nil {
		t.Fatalf("record %+v", r)
	}
	b, err := os.ReadFile(filepath.Join(dir, "data", "logs", r["id"].(string)+".stdout.log"))
	if err != nil || !strings.Contains(string(b), "hello 中文") || !strings.Contains(string(b), "***") || strings.Contains(string(b), "private-fixture-value") {
		t.Fatalf("log %s error %v", b, err)
	}
	state, _ := os.ReadFile(filepath.Join(dir, "data", "state.json"))
	if strings.Contains(string(state), "private-fixture-value") {
		t.Fatal("state contains environment value")
	}
}

func TestStopBindsTheRunAndASecondManualRunCannotOverlap(t *testing.T) {
	dir, _ := fixture(t, "block", "10s")
	operation(t, dir, "action", "run", "sample")
	r := waitRecord(t, dir, "running")
	if v := operation(t, dir, "action", "run", "sample"); v["error"] == nil {
		t.Fatal("overlapping manual run accepted")
	}
	if v := operation(t, dir, "action", "stop:stale", "sample"); v["error"] == nil {
		t.Fatal("stale stop accepted")
	}
	if v := operation(t, dir, "action", "stop:"+r["id"].(string), "sample"); v["error"] != nil {
		t.Fatalf("stop %+v", v)
	}
	if got := waitRecord(t, dir, "cancelled"); got["reason"] != "stop" {
		t.Fatalf("cancelled %+v", got)
	}
}

func TestTimeoutAndNonzeroExitRemainDifferentFailures(t *testing.T) {
	for _, tc := range []struct{ mode, status, reason string }{{"block", "timed_out", "timeout"}, {"fail", "failed", "exit"}} {
		t.Run(tc.mode, func(t *testing.T) {
			dir, _ := fixture(t, tc.mode, "1s")
			operation(t, dir, "action", "run", "sample")
			r := waitRecord(t, dir, tc.status)
			if r["reason"] != tc.reason {
				t.Fatalf("record %+v", r)
			}
		})
	}
}

func TestARejectedConfigurationReloadKeepsTheRunningTaskAndItsStopAction(t *testing.T) {
	dir, p := fixture(t, "block", "10s")
	operation(t, dir, "action", "run", "sample")
	r := waitRecord(t, dir, "running")
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{"version":1,"tasks":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	v := operation(t, dir, "detail")
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), r["id"].(string)) || !strings.Contains(string(b), "_diagnostic") {
		t.Fatalf("lost loaded task: %s", b)
	}
	if v := operation(t, dir, "action", "stop:"+r["id"].(string), "sample"); v["error"] != nil {
		t.Fatalf("lost stop: %+v", v)
	}
	waitRecord(t, dir, "cancelled")
}

func TestRestoringASuspendedPluginDoesNotStopAnotherInstalledTaskPlugin(t *testing.T) {
	dir, p := fixture(t, "block", "10s")
	other := filepath.Join(filepath.Dir(dir), "other-tasks")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "tasks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	q := p
	q.Dir = other
	q.ID = "other-tasks"
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p, q}); err != nil {
		t.Fatal(err)
	}
	operation(t, other, "action", "run", "sample")
	r := waitRecord(t, other, "running")
	resume, err := tasks.Suspend(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	resume()
	if v := operation(t, other, "action", "stop:"+r["id"].(string), "sample"); v["error"] != nil {
		t.Fatalf("another plugin was suspended: %+v", v)
	}
	waitRecord(t, other, "cancelled")
}

func TestOversizedLogsAreTruncatedAndMissingReferencesFailBeforeStarting(t *testing.T) {
	t.Run("bounded output", func(t *testing.T) {
		dir, _ := fixture(t, "large", "5s")
		operation(t, dir, "action", "run", "sample")
		r := waitRecord(t, dir, "succeeded")
		logs := r["logs"].(map[string]any)
		if logs["stdout_truncated"] != true {
			t.Fatalf("unreported truncation %+v", r)
		}
		info, err := os.Stat(filepath.Join(dir, logs["stdout"].(string)))
		if err != nil || info.Size() != (1<<20) {
			t.Fatalf("log size %v %v", info, err)
		}
	})
	t.Run("missing environment", func(t *testing.T) {
		dir, p := fixture(t, "success", "5s")
		b, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
		if err != nil {
			t.Fatal(err)
		}
		b = []byte(strings.ReplaceAll(string(b), "${env:HOSTDECK_TASK_SECRET}", "${env:HOSTDECK_TASK_ABSENT_FIXTURE}"))
		if _, ok := os.LookupEnv("HOSTDECK_TASK_ABSENT_FIXTURE"); ok {
			t.Fatal("fixture reference is unexpectedly set")
		}
		if err := os.WriteFile(filepath.Join(dir, "tasks.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p})
		operation(t, dir, "action", "run", "sample")
		r := waitRecord(t, dir, "failed")
		if r["reason"] != "environment" || r["started_at"] != nil {
			t.Fatalf("started despite missing environment %+v", r)
		}
	})
}
