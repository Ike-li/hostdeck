package examples

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

func installedTasks(t *testing.T) (string, plugin.Plugin) {
	t.Helper()
	dir, err := catalog.Add(Files, t.TempDir(), "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plugin.ReadFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, p
}

func taskConfig(t *testing.T, dir string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func validTask() map[string]any {
	return map[string]any{"id": "backup", "name": "Backup 中文", "enabled": true, "schedule": map[string]any{"cron": "0 2 * * *", "timezone": "Asia/Shanghai"}, "exec": map[string]any{"command": []string{"missing-task-program", "argument with spaces"}}}
}

func TestScheduledTasksInstallAndShowConfigurationWithoutRunningAProgram(t *testing.T) {
	dir, p := installedTasks(t)
	c := card.Fetch(context.Background(), p)
	if c.Error != nil || c.Value != "0" {
		t.Fatalf("empty card %+v", c)
	}
	m := validTask()
	marker := filepath.Join(t.TempDir(), "script-ran")
	m["exec"] = map[string]any{"command": []string{os.Args[0], "-test.run=^TestScheduledTaskReadProbe$", "--", marker}}
	taskConfig(t, dir, map[string]any{"version": 1, "tasks": []any{m}})
	v := detail.Read(context.Background(), p)
	if v.Error != nil || len(v.Items) != 1 || v.Items[0].ID != "backup" || v.Items[0].Name.All != "Backup 中文" || len(v.Items[0].Actions) != 0 {
		t.Fatalf("configuration %+v error %+v", v, v.Error)
	}
	b, _ := json.Marshal(v)
	for _, want := range []string{"Asia/Shanghai", "0 2 * * *", "script-ran", "not available", "尚未"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q in %s", want, b)
		}
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 2 {
		t.Fatalf("read wrote files: %v", files)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("reading executed the program: %v", err)
	}
}

func TestScheduledTaskReadProbe(t *testing.T) {
	if len(os.Args) > 2 && os.Args[len(os.Args)-2] == "--" {
		if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("ran"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScheduledTaskConfigurationRefusesMistakesInsteadOfLosingTasks(t *testing.T) {
	for _, bad := range []string{
		`{"version":1,"tasks":null}`, `{"version":2,"tasks":[]}`, `{"version":1,"version":1,"tasks":[]}`, `{"version":1,"tasks":[],"typo":true}`,
		`{"version":1,"tasks":[]} {}`, `{"version":1,"tasks":[null]}`,
	} {
		t.Run(bad, func(t *testing.T) {
			dir, p := installedTasks(t)
			os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(bad), 0600)
			v := detail.Read(context.Background(), p)
			if v.Error == nil || v.Error.Text.ByLang["en"] == "" || v.Error.Text.ByLang["zh-CN"] == "" {
				t.Fatalf("accepted %s: %+v", bad, v)
			}
		})
	}
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { m["timeout"] = "" },
		func(m map[string]any) { m["exec"] = map[string]any{"command": []string{"program"}, "dir": ""} },
		func(m map[string]any) { delete(m, "enabled") }, func(m map[string]any) { m["name"] = "  " }, func(m map[string]any) { m["id"] = "bad\n" }, func(m map[string]any) { m["timeout"] = "25h" },
		func(m map[string]any) { m["schedule"] = map[string]any{"cron": "0 0 31 2 *", "timezone": "UTC"} },
		func(m map[string]any) { m["schedule"] = map[string]any{"cron": "0 0 * * * *", "timezone": "UTC"} },
		func(m map[string]any) { m["schedule"] = map[string]any{"every": "1s"} },
		func(m map[string]any) { m["exec"] = map[string]any{"command": []string{"@hostdeck", "tasks", "card"}} },
		func(m map[string]any) {
			m["exec"] = map[string]any{"command": []string{"program"}, "env": map[string]string{"TOKEN": "literal"}}
		},
	} {
		t.Run("invalid task", func(t *testing.T) {
			dir, p := installedTasks(t)
			m := validTask()
			mutate(m)
			taskConfig(t, dir, map[string]any{"version": 1, "tasks": []any{m}})
			if v := detail.Read(context.Background(), p); v.Error == nil {
				t.Fatalf("accepted %+v", m)
			}
		})
	}
}
