package console

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func TestTheScheduledTaskPageCanCreateEditAndDeleteATask(t *testing.T) {
	pluginDir := t.TempDir()
	if _, err := catalog.Add(examples.Files, pluginDir, "scheduled-tasks"); err != nil {
		t.Fatal(err)
	}
	reload := func() (Loaded, error) {
		plugins, broken, err := plugin.Load(pluginDir)
		if err != nil {
			return Loaded{}, err
		}
		if err := tasks.Sync(pluginDir, plugins); err != nil {
			return Loaded{}, err
		}
		return Loaded{Plugins: plugins, Broken: broken}, nil
	}
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tasks.Close(context.Background(), pluginDir) })
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: examples.Files, Plugins: loaded.Plugins, Reload: reload})
	configPath := filepath.Join(pluginDir, "scheduled-tasks", "tasks.json")
	before := mustReadFile(t, configPath)

	body := `{"name":"Nightly backup","enabled":true,"schedule":{"every":"24h"},"command":"echo hello"}`
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks", body, map[string]string{"Origin": "http://127.0.0.1:4679", "Sec-Fetch-Site": "same-site"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin HTTP %d", resp.StatusCode)
	}
	if string(mustReadFile(t, configPath)) != string(before) {
		t.Fatal("foreign origin wrote tasks.json")
	}
	resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks", body, fromConsole)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create HTTP %d %s", resp.StatusCode, b)
	}
	var created struct{ ID string }
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.ID != "nightly-backup" {
		t.Fatalf("created %+v %v", created, err)
	}
	list := get(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks", nil)
	var listed struct {
		Tasks []struct{ ID string }
	}
	if list.StatusCode != http.StatusOK || json.NewDecoder(list.Body).Decode(&listed) != nil || len(listed.Tasks) != 1 || listed.Tasks[0].ID != "nightly-backup" {
		t.Fatalf("list %+v HTTP %d", listed, list.StatusCode)
	}
	one := get(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks/nightly-backup", nil)
	var form struct {
		Name    string
		Command string
	}
	if one.StatusCode != http.StatusOK || json.NewDecoder(one.Body).Decode(&form) != nil || form.Name != "Nightly backup" || form.Command != "echo hello" {
		t.Fatalf("read %+v HTTP %d", form, one.StatusCode)
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks/nightly-backup", `{"name":"Nightly","schedule":{"cron":"0 0 *","timezone":"UTC"},"command":"echo hello"}`, fromConsole); resp.StatusCode != http.StatusBadRequest || errorCode(t, resp) != "bad_request" {
		t.Fatalf("bad cron HTTP %d", resp.StatusCode)
	}
	if !strings.Contains(string(mustReadFile(t, configPath)), `"every":"24h"`) {
		t.Fatal("refused edit changed the schedule")
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks/nightly-backup", `{"name":"Renamed","enabled":false,"schedule":{"every":"24h"},"timeout":"10m","command":"echo renamed"}`, fromConsole); resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update HTTP %d %s", resp.StatusCode, b)
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks/nightly-backup/delete", ``, fromConsole); resp.StatusCode != http.StatusOK {
		t.Fatalf("delete HTTP %d", resp.StatusCode)
	}
	missing := get(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks/nightly-backup", nil)
	if missing.StatusCode != http.StatusNotFound || errorCode(t, missing) != "task_missing" {
		t.Fatalf("missing HTTP %d", missing.StatusCode)
	}
	absent := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: t.TempDir(), Reload: func() (Loaded, error) { return Loaded{}, nil }})
	if resp := postJSON(absent, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/tasks", body, fromConsole); resp.StatusCode != http.StatusNotFound || errorCode(t, resp) != "not_installed" {
		t.Fatalf("absent plugin HTTP %d", resp.StatusCode)
	}
}
