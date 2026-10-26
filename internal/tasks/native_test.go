package tasks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	os.Exit(m.Run())
}

func TestTaskCommand(t *testing.T) {
	mode := os.Getenv("HOSTDECK_TASK_MODE")
	if mode == "" {
		return
	}
	if mode == "block" {
		fmt.Println("started")
		time.Sleep(time.Hour)
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

func fixture(t *testing.T, mode, timeout string) (string, plugin.Plugin) {
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
	tasks.Sync(root, []plugin.Plugin{p})
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
