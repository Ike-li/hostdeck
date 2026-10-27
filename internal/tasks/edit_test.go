package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/process"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func TestThePageCanCreateATaskAndTheOpenDetailShowsIt(t *testing.T) {
	dir, root, p := emptyTasks(t)
	detail := operation(t, dir, "detail")
	note, _ := detail["note"].(map[string]any)
	en, _ := note["en"].(string)
	zh, _ := note["zh-CN"].(string)
	configPath := filepath.Join(dir, "tasks.json")
	if !strings.Contains(en, "New task") || !strings.Contains(zh, "新建任务") || !strings.Contains(en, configPath) || !strings.Contains(zh, configPath) || len(itemsOf(t, detail)) != 0 {
		t.Fatalf("empty detail %+v", detail)
	}

	enabled := true
	every := "24h"
	command := `bash -lc "echo hi"`
	id, err := tasks.CreateTask(dir, tasks.TaskForm{
		Name:     "  Nightly backup  ",
		Enabled:  &enabled,
		Schedule: tasks.ScheduleForm{Every: &every},
		Command:  &command,
	})
	if err != nil || id != "nightly-backup" {
		t.Fatalf("create %q %v", id, err)
	}
	raw := mustRead(t, filepath.Join(dir, "tasks.json"))
	if strings.Contains(string(raw), "30m0s") || !strings.Contains(string(raw), `"timeout":"30m"`) {
		t.Fatalf("timeout spelling %s", raw)
	}
	var saved struct {
		Tasks []struct {
			ID       string
			Name     string
			Enabled  bool
			Schedule struct{ Every string }
			Exec     struct {
				Command []string
				Windows []string
				Dir     string
				Env     map[string]string
			}
			Timeout string
		}
	}
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Tasks) != 1 {
		t.Fatalf("file %s %v", raw, err)
	}
	got := saved.Tasks[0]
	if got.ID != "nightly-backup" || got.Name != "Nightly backup" || !got.Enabled || got.Schedule.Every != "24h" || got.Timeout != "30m" || strings.Join(got.Exec.Command, "\n") != "bash\n-lc\necho hi" || got.Exec.Windows != nil || got.Exec.Dir != "" || got.Exec.Env != nil {
		t.Fatalf("stored %+v", got)
	}
	shown := operation(t, dir, "detail")
	items := itemsOf(t, shown)
	if len(items) != 1 || items[0]["id"] != "nightly-backup" || items[0]["name"] != "Nightly backup" {
		t.Fatalf("detail %+v", shown)
	}

	hand := []byte(`{"version":1,"tasks":[{"id":"hand","name":"Hand edited","enabled":false,"schedule":{"every":"2h"},"exec":{"command":["true"]}}]}`)
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), hand, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(root, []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	reloaded := operation(t, dir, "detail")
	items = itemsOf(t, reloaded)
	if len(items) != 1 || items[0]["id"] != "hand" || items[0]["name"] != "Hand edited" {
		t.Fatalf("reload %+v", reloaded)
	}
}

func TestUpdatingATaskWithoutTimeoutKeepsTheCustomTimeout(t *testing.T) {
	dir, _, _ := emptyTasks(t)
	original := []byte(`{"version":1,"tasks":[{"id":"backup","name":"Backup","enabled":true,"schedule":{"every":"2h"},"exec":{"command":["true"]},"timeout":"2h"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{{ID: "scheduled-tasks", Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "tasks", "detail"}}}}}); err != nil {
		t.Fatal(err)
	}
	enabled := false
	command := "echo still"
	if err := tasks.UpdateTask(dir, "backup", tasks.TaskForm{
		Name:    "Backup",
		Enabled: &enabled,
		Command: &command,
	}); err != nil {
		t.Fatal(err)
	}
	raw := mustRead(t, filepath.Join(dir, "tasks.json"))
	var saved struct {
		Tasks []struct {
			Timeout string
			Enabled bool
		}
	}
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Tasks) != 1 {
		t.Fatalf("file %s %v", raw, err)
	}
	if saved.Tasks[0].Timeout != "2h" || saved.Tasks[0].Enabled {
		t.Fatalf("timeout/enabled = %q/%v, want 2h kept and disabled", saved.Tasks[0].Timeout, saved.Tasks[0].Enabled)
	}
}

func TestEditingATaskKeepsWhatTheFormDoesNotShowAndABadScheduleDoesNotWrite(t *testing.T) {
	dir, _, _ := emptyTasks(t)
	original := []byte(`{"version":1,"tasks":[{"id":"backup","name":"Backup","enabled":true,"schedule":{"every":"2h"},"exec":{"command":["python3","scripts/backup.py"],"windows":["py","-3","scripts/backup.py"],"dir":"work","env":{"TOKEN":"${env:BACKUP_TOKEN}"}},"timeout":"5m"},{"id":"other","name":"Other","enabled":true,"schedule":{"cron":"0 4 * * *","timezone":"UTC"},"exec":{"command":["true"]},"timeout":"5m"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{{ID: "scheduled-tasks", Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "tasks", "detail"}}}}}); err != nil {
		t.Fatal(err)
	}
	enabled := false
	cron, zone := "15 3 * * *", "Asia/Shanghai"
	command := "echo kept"
	err := tasks.UpdateTask(dir, "backup", tasks.TaskForm{
		Name:     "Nightly",
		Enabled:  &enabled,
		Schedule: tasks.ScheduleForm{Cron: &cron, Timezone: &zone},
		Timeout:  "10m",
		Command:  &command,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := mustRead(t, filepath.Join(dir, "tasks.json"))
	var saved struct {
		Tasks []struct {
			ID, Name, Timeout string
			Enabled           bool
			Schedule          struct {
				Cron, Timezone, Every *string
			}
			Exec struct {
				Command, Windows []string
				Dir              string
				Env              map[string]string
			}
		}
	}
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Tasks) != 2 {
		t.Fatalf("file %s %v", raw, err)
	}
	got := saved.Tasks[0]
	commandLine, windowsLine := "echo kept", "py -3 scripts/backup.py"
	if runtime.GOOS == "windows" {
		commandLine, windowsLine = "python3 scripts/backup.py", "echo kept"
	}
	if got.ID != "backup" || got.Name != "Nightly" || got.Enabled || got.Timeout != "10m" || got.Schedule.Cron == nil || *got.Schedule.Cron != "15 3 * * *" || got.Schedule.Timezone == nil || *got.Schedule.Timezone != "Asia/Shanghai" || strings.Join(got.Exec.Command, " ") != commandLine || strings.Join(got.Exec.Windows, " ") != windowsLine || got.Exec.Dir != "work" || got.Exec.Env["TOKEN"] != "${env:BACKUP_TOKEN}" {
		t.Fatalf("edited %+v", got)
	}
	if saved.Tasks[1].ID != "other" || saved.Tasks[1].Timeout != "5m" {
		t.Fatalf("other task changed %+v", saved.Tasks[1])
	}
	shown, err := tasks.ReadTask(dir, "backup")
	if err != nil || shown.Command == nil || *shown.Command != "echo kept" || shown.ID != "backup" {
		t.Fatalf("read %+v %v", shown, err)
	}

	before := mustRead(t, filepath.Join(dir, "tasks.json"))
	bad := tasks.ScheduleForm{Cron: &cron, Timezone: strPtr("Local")}
	if err := tasks.UpdateTask(dir, "backup", tasks.TaskForm{Name: "Nightly", Schedule: bad, Command: &command}); !errors.Is(err, tasks.ErrTaskForm) {
		t.Fatalf("local timezone %v", err)
	}
	four := "0 0 * *"
	if err := tasks.UpdateTask(dir, "backup", tasks.TaskForm{Name: "Nightly", Schedule: tasks.ScheduleForm{Cron: &four, Timezone: &zone}, Command: &command}); !errors.Is(err, tasks.ErrTaskForm) {
		t.Fatalf("four fields %v", err)
	}
	open := `echo "hi`
	if err := tasks.UpdateTask(dir, "backup", tasks.TaskForm{Name: "Nightly", Schedule: tasks.ScheduleForm{Cron: &cron, Timezone: &zone}, Command: &open}); !errors.Is(err, tasks.ErrTaskForm) {
		t.Fatalf("unclosed quote %v", err)
	}
	if string(mustRead(t, filepath.Join(dir, "tasks.json"))) != string(before) {
		t.Fatal("a refused edit changed tasks.json")
	}

	quoted := []byte(`{"version":1,"tasks":[{"id":"q","name":"Quoted","enabled":true,"schedule":{"every":"24h"},"exec":{"command":["echo","say \"hi\""]}}]}`)
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), quoted, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{{ID: "scheduled-tasks", Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "tasks", "detail"}}}}}); err != nil {
		t.Fatal(err)
	}
	locked, err := tasks.ReadTask(dir, "q")
	if err != nil || !locked.CommandLocked || locked.Command != nil {
		t.Fatalf("quoted command presented as a line: %+v %v", locked, err)
	}
	if err := tasks.UpdateTask(dir, "q", tasks.TaskForm{Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	again, err := tasks.ReadTask(dir, "q")
	if err != nil || again.Name != "Renamed" || !again.CommandLocked {
		t.Fatalf("rename dropped the command %+v %v", again, err)
	}
	raw = mustRead(t, filepath.Join(dir, "tasks.json"))
	if !strings.Contains(string(raw), `say \"hi\"`) && !strings.Contains(string(raw), `say "hi"`) {
		t.Fatalf("argv lost its quote: %s", raw)
	}
}

func TestATaskThatWouldExceedOneMiBIsNotWritten(t *testing.T) {
	dir, _, _ := emptyTasks(t)
	command := "true"
	every := "24h"
	if _, err := tasks.CreateTask(dir, tasks.TaskForm{Name: "Kept", Schedule: tasks.ScheduleForm{Every: &every}, Command: &command}); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, filepath.Join(dir, "tasks.json"))
	huge := strings.Repeat("a", 1<<20)
	err := tasks.UpdateTask(dir, "kept", tasks.TaskForm{Name: "Kept", Command: &huge})
	if !errors.Is(err, tasks.ErrTaskForm) {
		t.Fatalf("oversized %v", err)
	}
	if string(mustRead(t, filepath.Join(dir, "tasks.json"))) != string(before) {
		t.Fatal("an oversized edit changed tasks.json")
	}
}

func TestALiveOldProcessTreeRefusesATaskWrite(t *testing.T) {
	dir, p := fixture(t, "success", "5s")
	operation(t, dir, "action", "run", "sample")
	r := waitRecord(t, dir, "succeeded")
	if err := tasks.Close(context.Background(), filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestTaskCommand$")
	cmd.Env = append(os.Environ(), "HOSTDECK_TASK_MODE=block", "HOSTDECK_TASK_BLOCK_DURATION=10s")
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
	record["status"] = "interrupted"
	record["reason"] = "cleanup"
	s["active"] = map[string]any{r["id"].(string): map[string]any{"pid": cmd.Process.Pid, "token": r["id"]}}
	saveState(t, dir, s)
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, filepath.Join(dir, "tasks.json"))
	err = tasks.UpdateTask(dir, "sample", tasks.TaskForm{Name: "Renamed"})
	if err == nil || !strings.Contains(err.Error(), "local inspection") {
		t.Fatalf("live process tree accepted a write: %v", err)
	}
	if string(mustRead(t, filepath.Join(dir, "tasks.json"))) != string(before) {
		t.Fatal("a refused edit changed tasks.json")
	}
	if process.Gone(process.Identity{PID: cmd.Process.Pid, Token: r["id"].(string)}) {
		t.Fatal("the refusal killed the old process")
	}
	tree.Stop()
	cmd.Wait()
	if err := tasks.UpdateTask(dir, "sample", tasks.TaskForm{Name: "Renamed"}); err != nil {
		t.Fatalf("cleared tree could not edit: %v", err)
	}
}

func TestANewTaskDoesNotReuseAnIDThatStillHasHistory(t *testing.T) {
	dir, root, p := emptyTasks(t)
	state := `{"version":1,"tasks":{},"runs":[{"id":"run-old","task_id":"old","revision":"rev","trigger":"manual","scheduled_at":null,"accepted_at":"2026-10-27T00:00:00Z","started_at":"2026-10-27T00:00:01Z","finished_at":"2026-10-27T00:00:02Z","status":"succeeded","exit_code":0,"reason":null,"logs":{"stdout":null,"stderr":null,"stdout_truncated":false,"stderr_truncated":false}}],"active":{}}`
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "state.json"), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{"version":1,"tasks":[{"id":"kept","name":"Kept","enabled":true,"schedule":{"every":"24h"},"exec":{"command":["true"]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(root, []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	command := "true"
	every := "24h"
	id, err := tasks.CreateTask(dir, tasks.TaskForm{Name: "Old", Schedule: tasks.ScheduleForm{Every: &every}, Command: &command})
	if err != nil || id != "old-2" {
		t.Fatalf("history id %q %v", id, err)
	}
	id, err = tasks.CreateTask(dir, tasks.TaskForm{Name: "Kept", Schedule: tasks.ScheduleForm{Every: &every}, Command: &command})
	if err != nil || id != "kept-2" {
		t.Fatalf("config id %q %v", id, err)
	}
	id, err = tasks.CreateTask(dir, tasks.TaskForm{Name: "备份", Schedule: tasks.ScheduleForm{Every: &every}, Command: &command})
	if err != nil || id != "task" {
		t.Fatalf("fallback id %q %v", id, err)
	}
}

func TestDeletingATaskLeavesItsHistoryAndCancelsARunningOne(t *testing.T) {
	dir, _ := fixture(t, "block", "30s")
	operation(t, dir, "action", "run", "sample")
	waitRecord(t, dir, "running")
	before := mustRead(t, filepath.Join(dir, "tasks.json"))
	if err := tasks.DeleteTask(dir, "missing"); !errors.Is(err, tasks.ErrTaskMissing) {
		t.Fatalf("missing %v", err)
	}
	if string(mustRead(t, filepath.Join(dir, "tasks.json"))) != string(before) {
		t.Fatal("a missing delete changed tasks.json")
	}
	if err := tasks.DeleteTask(dir, "sample"); err != nil {
		t.Fatal(err)
	}
	raw := mustRead(t, filepath.Join(dir, "tasks.json"))
	if strings.Contains(string(raw), "sample") {
		t.Fatalf("task remains %s", raw)
	}
	var saved struct{ Tasks []any }
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Tasks) != 0 {
		t.Fatalf("tasks %s %v", raw, err)
	}
	got := waitRecord(t, dir, "cancelled")
	if got["reason"] != "task_removed" || got["task_id"] != "sample" {
		t.Fatalf("record %+v", got)
	}
	history := string(mustRead(t, filepath.Join(dir, "data", "state.json")))
	if !strings.Contains(history, `"task_id":"sample"`) && !strings.Contains(history, `"task_id": "sample"`) {
		t.Fatal("history was removed")
	}
}

func strPtr(s string) *string { return &s }

func emptyTasks(t *testing.T) (string, string, plugin.Plugin) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "scheduled-tasks")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{"version":1,"tasks":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := plugin.Plugin{ID: "scheduled-tasks", Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "tasks", "detail"}}}}
	if err := tasks.Sync(root, []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tasks.Close(ctx, root); err != nil {
			t.Error(err)
		}
	})
	return dir, root, p
}

func itemsOf(t *testing.T, detail map[string]any) []map[string]any {
	t.Helper()
	raw, ok := detail["items"].([]any)
	if !ok {
		t.Fatalf("items %#v", detail["items"])
	}
	out := make([]map[string]any, len(raw))
	for i, item := range raw {
		out[i], ok = item.(map[string]any)
		if !ok {
			t.Fatalf("item %#v", item)
		}
	}
	return out
}
