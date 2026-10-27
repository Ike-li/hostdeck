package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

// A one-shot run uses a command this page already saved for that file.
// It must not become a task, and reloading tasks.json must not cancel it.
func TestAScriptRunDoesNotWriteTasksJSONAndSharesTheFourSlots(t *testing.T) {
	t.Setenv("HOSTDECK_TASK_BLOCK_DURATION", "30s")
	dir, p := fixture(t, "block", "30s")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"job.py", "other.py"} {
		if err := os.WriteFile(filepath.Join(dir, "scripts", name), []byte("print(1)\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	every := "24h"
	enabled := true
	job := []string{os.Args[0], "-test.run=^TestTaskCommand$", "scripts/job.py"}
	other := []string{os.Args[0], "-test.run=^TestTaskCommand$", "scripts/other.py"}
	jobLine := commandLine(t, job)
	if err := tasks.UpdateTask(dir, "sample", tasks.TaskForm{Name: "Sample 中文", Timeout: "30s", Schedule: tasks.ScheduleForm{Every: &every}, Command: &jobLine}); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		name string
		args []string
	}{
		{"Fill A", []string{os.Args[0], "-test.run=^TestTaskCommand$"}},
		{"Fill B", []string{os.Args[0], "-test.run=^TestTaskCommand$"}},
		{"Other", other},
	} {
		line := commandLine(t, spec.args)
		if _, err := tasks.CreateTask(dir, tasks.TaskForm{Name: spec.name, Enabled: &enabled, Timeout: "30s", Schedule: tasks.ScheduleForm{Every: &every}, Command: &line}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := tasks.RunScript(dir, "job.py", job)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("script run changed tasks.json\n%s", after)
	}
	state := readState(t, dir)
	if scriptRun(t, state, id)["file"] != "job.py" {
		t.Fatalf("script record %+v", state["scripts"])
	}
	if taskRun(state, id) != nil {
		t.Fatal("script run was stored in the task run array")
	}
	active, _ := state["active"].(map[string]any)
	if len(active) != 1 || active[id] == nil {
		t.Fatalf("slot %+v", active)
	}
	if _, err := tasks.RunScript(dir, "job.py", job); err == nil {
		t.Fatal("overlapping script run accepted")
	}
	if err := tasks.DeleteTask(dir, "sample"); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	state = readState(t, dir)
	got := scriptRun(t, state, id)
	if got["status"] != "starting" && got["status"] != "running" || got["reason"] != nil {
		t.Fatalf("reload cancelled the script run: %+v", got)
	}
	removed, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil || strings.Contains(string(removed), `"id":"sample"`) {
		t.Fatal("sample task was not removed")
	}
	for _, taskID := range []string{"fill-a", "fill-b", "other"} {
		if v := operation(t, dir, "action", "run", taskID); v["error"] != nil {
			t.Fatalf("start %s: %+v", taskID, v)
		}
	}
	if _, err := tasks.RunScript(dir, "other.py", other); err == nil {
		t.Fatal("fifth execution slot accepted a script run")
	}
	final, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil || string(final) != string(removed) {
		t.Fatal("a refused script run changed tasks.json")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tasks.Close(ctx, filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	done := waitScriptStatus(t, dir, id, "cancelled")
	if done["reason"] != "shutdown" {
		t.Fatalf("close %+v", done)
	}
}

func commandLine(t *testing.T, args []string) string {
	t.Helper()
	parts := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, "\"\n\r") {
			t.Fatalf("argument cannot be shown as a command line: %q", arg)
		}
		if arg == "" || strings.IndexFunc(arg, unicode.IsSpace) >= 0 {
			parts[i] = `"` + arg + `"`
		} else {
			parts[i] = arg
		}
	}
	return strings.Join(parts, " ")
}

func scriptRun(t *testing.T, state map[string]any, id string) map[string]any {
	t.Helper()
	scripts, _ := state["scripts"].([]any)
	for _, raw := range scripts {
		r := raw.(map[string]any)
		if r["id"] == id {
			return r
		}
	}
	b, _ := json.Marshal(state)
	t.Fatalf("no script run %s in %s", id, b)
	return nil
}

func taskRun(state map[string]any, id string) map[string]any {
	runs, _ := state["runs"].([]any)
	for _, raw := range runs {
		r := raw.(map[string]any)
		if r["id"] == id {
			return r
		}
	}
	return nil
}

func waitScriptStatus(t *testing.T, dir, id, status string) map[string]any {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		state := readState(t, dir)
		if got := scriptRun(t, state, id); got["status"] == status {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := readState(t, dir)
	t.Fatalf("script %s did not become %s: %+v", id, status, state["scripts"])
	return nil
}

func TestAScriptListNamesTheDefaultCommandAndRefusesARunWithoutOne(t *testing.T) {
	dir, _ := fixture(t, "success", "5s")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hello.py", "app.js", "mod.mjs", "run.sh", "tool.ps1", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, "scripts", name), []byte("print(1)\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "scripts", "link.py")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := tasks.ListScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]tasks.ScriptInfo{}
	for _, info := range listed {
		got[info.Name] = info
	}
	if _, ok := got["link.py"]; ok {
		t.Fatal("symlink was listed")
	}
	py := "python3 scripts/hello.py"
	sh := "bash scripts/run.sh"
	shRun := true
	if runtime.GOOS == "windows" {
		py = "py -3 scripts/hello.py"
		sh = ""
		shRun = false
	}
	want := map[string]struct {
		command  string
		runnable bool
	}{
		"hello.py":  {py, true},
		"app.js":    {"node scripts/app.js", true},
		"mod.mjs":   {"node scripts/mod.mjs", true},
		"run.sh":    {sh, shRun},
		"tool.ps1":  {"powershell -NoProfile -File scripts/tool.ps1", true},
		"notes.txt": {"", false},
	}
	for name, spec := range want {
		info, ok := got[name]
		if !ok || info.Command != spec.command || info.Runnable != spec.runnable {
			t.Fatalf("%s command %q runnable %v, want %q %v", name, info.Command, info.Runnable, spec.command, spec.runnable)
		}
	}
	if _, err := tasks.RunScript(dir, "notes.txt", nil); !errors.Is(err, tasks.ErrTaskForm) {
		t.Fatalf("notes.txt: %v", err)
	}
	if _, err := tasks.RunScript(dir, "hello.py", []string{"python3", "scripts/hello.py", "extra"}); !errors.Is(err, tasks.ErrTaskForm) {
		t.Fatalf("unsaved command: %v", err)
	}
	if _, err := tasks.RunScript(dir, "hello.py", []string{"@hostdeck", "scripts/hello.py"}); !errors.Is(err, tasks.ErrTaskForm) {
		t.Fatalf("@hostdeck: %v", err)
	}
	if _, err := tasks.RunScript(dir, "link.py", nil); !errors.Is(err, tasks.ErrScriptRefused) {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := tasks.RunScript(dir, "../outside.txt", nil); !errors.Is(err, tasks.ErrScriptName) {
		t.Fatalf("escape: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("a refused script run changed tasks.json")
	}
	if scripts, ok := readState(t, dir)["scripts"]; ok && scripts != nil {
		t.Fatalf("refused runs were recorded: %+v", scripts)
	}
	if string(mustRead(t, outside)) != "keep" {
		t.Fatal("symlink run changed the target")
	}
}

func TestAnUnfinishedScriptRunIsInterruptedWithoutPausingItsTask(t *testing.T) {
	t.Setenv("HOSTDECK_TASK_BLOCK_DURATION", "30s")
	dir, p := fixture(t, "block", "30s")
	if err := os.WriteFile(filepath.Join(dir, "scripts", "job.py"), []byte("print(1)\n"), 0600); err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(filepath.Join(dir, "scripts", "job.py"), []byte("print(1)\n"), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	job := []string{os.Args[0], "-test.run=^TestTaskCommand$", "scripts/job.py"}
	line := commandLine(t, job)
	every := "24h"
	if err := tasks.UpdateTask(dir, "sample", tasks.TaskForm{Name: "Sample 中文", Timeout: "30s", Schedule: tasks.ScheduleForm{Every: &every}, Command: &line}); err != nil {
		t.Fatal(err)
	}
	id, err := tasks.RunScript(dir, "job.py", job)
	if err != nil {
		t.Fatal(err)
	}
	waitScriptStatus(t, dir, id, "running")
	snapshot, err := os.ReadFile(filepath.Join(dir, "data", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tasks.Close(ctx, filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "state.json"), snapshot, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	done := waitScriptStatus(t, dir, id, "interrupted")
	if done["reason"] != "recovery" {
		t.Fatalf("recovery %+v", done)
	}
	state := readState(t, dir)
	if active, _ := state["active"].(map[string]any); active[id] != nil {
		t.Fatalf("recovered script still holds a slot: %+v", active)
	}
	sample := state["tasks"].(map[string]any)["sample"].(map[string]any)
	if sample["paused"] == true {
		t.Fatal("script recovery paused the task")
	}
	listed, err := tasks.ListScripts(dir)
	if err != nil || len(listed) != 1 || listed[0].Status != "interrupted" || listed[0].Result.ByLang["en"] == "" || listed[0].Result.ByLang["zh-CN"] == "" {
		t.Fatalf("list %+v %v", listed, err)
	}
	if _, err := tasks.RunScript(dir, "job.py", job); err != nil {
		t.Fatalf("slot stayed busy: %v", err)
	}
}

func TestDisablingThePluginCancelsAScriptRun(t *testing.T) {
	t.Setenv("HOSTDECK_TASK_BLOCK_DURATION", "30s")
	dir, _ := fixture(t, "block", "30s")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "job.py"), []byte("print(1)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	job := []string{os.Args[0], "-test.run=^TestTaskCommand$", "scripts/job.py"}
	line := commandLine(t, job)
	every := "24h"
	if err := tasks.UpdateTask(dir, "sample", tasks.TaskForm{Name: "Sample 中文", Timeout: "30s", Schedule: tasks.ScheduleForm{Every: &every}, Command: &line}); err != nil {
		t.Fatal(err)
	}
	id, err := tasks.RunScript(dir, "job.py", job)
	if err != nil {
		t.Fatal(err)
	}
	waitScriptStatus(t, dir, id, "running")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tasks.Suspend(ctx, dir); err != nil {
		t.Fatal(err)
	}
	done := waitScriptStatus(t, dir, id, "cancelled")
	if done["reason"] != "plugin_disabled" {
		t.Fatalf("disable %+v", done)
	}
}

func TestAScriptRunUsesTheMatchedTaskDirAndEnv(t *testing.T) {
	dir, plug := fixture(t, "success", "30s")
	work := filepath.Join(dir, "workdir")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "job.py"), []byte("print(1)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOSTDECK_TASK_MODE", "success")
	t.Setenv("BACKUP_TOKEN", "secret-token")
	marker := filepath.Join(t.TempDir(), "report")
	t.Setenv("HOSTDECK_TASK_MARKER", marker)
	t.Setenv("HOSTDECK_TASK_REPORT", "1")

	job := []string{os.Args[0], "-test.run=^TestTaskCommand$", "scripts/job.py"}
	cfg := map[string]any{
		"version": 1,
		"tasks": []any{map[string]any{
			"id": "sample", "name": "Sample", "enabled": true,
			"schedule": map[string]any{"every": "24h"},
			"timeout":  "5m",
			"exec": map[string]any{
				"command": job,
				"dir":     "workdir",
				"env":     map[string]string{"SCRIPT_TOKEN": "${env:BACKUP_TOKEN}"},
			},
		}},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{plug}); err != nil {
		t.Fatal(err)
	}
	id, err := tasks.RunScript(dir, "job.py", job)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitScriptStatus(t, dir, id, "succeeded")
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) < 2 {
		t.Fatalf("report %q", body)
	}
	wantWork, err := filepath.EvalSymlinks(work)
	if err != nil {
		wantWork = work
	}
	gotWork, err := filepath.EvalSymlinks(lines[0])
	if err != nil {
		gotWork = lines[0]
	}
	if gotWork != wantWork {
		t.Fatalf("cwd = %q, want %q", lines[0], work)
	}
	if lines[1] != "secret-token" {
		t.Fatalf("SCRIPT_TOKEN = %q, want secret-token", lines[1])
	}
}

func TestDeletingARunningScriptCancelsItBeforeTheFileIsGone(t *testing.T) {
	t.Setenv("HOSTDECK_TASK_BLOCK_DURATION", "30s")
	dir, plug := fixture(t, "block", "30s")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "job.py"), []byte("print(1)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	job := []string{os.Args[0], "-test.run=^TestTaskCommand$", "scripts/job.py"}
	every := "24h"
	jobLine := commandLine(t, job)
	if err := tasks.UpdateTask(dir, "sample", tasks.TaskForm{Name: "Sample", Timeout: "30s", Schedule: tasks.ScheduleForm{Every: &every}, Command: &jobLine}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{plug}); err != nil {
		t.Fatal(err)
	}
	id, err := tasks.RunScript(dir, "job.py", job)
	if err != nil {
		t.Fatal(err)
	}
	waitScriptStatus(t, dir, id, "running")
	if err := tasks.DeleteScript(dir, "job.py"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "job.py")); !os.IsNotExist(err) {
		t.Fatalf("script file still present: %v", err)
	}
	got := waitScriptStatus(t, dir, id, "cancelled")
	if got["reason"] != "stop" && got["reason"] != "task_removed" {
		t.Fatalf("cancel reason %+v", got)
	}
	state := readState(t, dir)
	active, _ := state["active"].(map[string]any)
	if active[id] != nil {
		t.Fatalf("active slot left after delete: %+v", active)
	}
}
