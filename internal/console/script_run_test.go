package console

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func TestRunningAScriptRefusesTheWrongOriginAndACommandThatIsNotAllowed(t *testing.T) {
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
	for _, name := range []string{"hello.py", "notes.txt", "run.sh"} {
		resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", `{"name":"`+name+`","content":"print(1)\n"}`, fromConsole)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create %s HTTP %d", name, resp.StatusCode)
		}
	}
	configPath := filepath.Join(pluginDir, "scheduled-tasks", "tasks.json")
	before := mustReadFile(t, configPath)
	run := func(name, body string, header map[string]string) *http.Response {
		t.Helper()
		return postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts/"+name+"/run", body, header)
	}
	wrong := map[string]string{"Origin": "http://127.0.0.1:1", "Sec-Fetch-Site": "same-origin"}
	if resp := run("hello.py", `{}`, wrong); resp.StatusCode != http.StatusForbidden || errorCode(t, resp) != "not_console" {
		t.Fatalf("foreign origin HTTP %d", resp.StatusCode)
	}
	if resp := run("notes.txt", `{}`, fromConsole); resp.StatusCode != http.StatusBadRequest || errorCode(t, resp) != "bad_request" {
		t.Fatalf("notes.txt HTTP %d", resp.StatusCode)
	}
	if resp := run("hello.py", `{"command":["@hostdeck","scripts/hello.py"]}`, fromConsole); resp.StatusCode != http.StatusBadRequest || errorCode(t, resp) != "bad_request" {
		t.Fatalf("@hostdeck HTTP %d", resp.StatusCode)
	}
	if resp := run("hello.py", `{"command":[]}`, fromConsole); resp.StatusCode != http.StatusBadRequest || errorCode(t, resp) != "bad_request" {
		t.Fatalf("empty command HTTP %d", resp.StatusCode)
	}
	if resp := run("hello.py", `{"command":["`+strings.Repeat("x", 1<<20)+`"]}`, fromConsole); resp.StatusCode != http.StatusBadRequest || errorCode(t, resp) != "bad_request" {
		t.Fatalf("oversize HTTP %d", resp.StatusCode)
	}
	if resp := run("missing.py", `{}`, fromConsole); resp.StatusCode != http.StatusNotFound || errorCode(t, resp) != "script_missing" {
		t.Fatalf("missing HTTP %d", resp.StatusCode)
	}
	list := get(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", nil)
	var listed struct {
		Files []struct {
			Name     string `json:"name"`
			Command  string `json:"command"`
			Runnable bool   `json:"runnable"`
		}
	}
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	py := "python3 scripts/hello.py"
	shRun := true
	if runtime.GOOS == "windows" {
		py = "py -3 scripts/hello.py"
		shRun = false
	}
	seen := map[string]struct {
		command  string
		runnable bool
	}{}
	for _, file := range listed.Files {
		seen[file.Name] = struct {
			command  string
			runnable bool
		}{file.Command, file.Runnable}
	}
	if seen["hello.py"].command != py || !seen["hello.py"].runnable {
		t.Fatalf("hello.py %+v", seen["hello.py"])
	}
	if seen["notes.txt"].command != "" || seen["notes.txt"].runnable {
		t.Fatalf("notes.txt %+v", seen["notes.txt"])
	}
	if seen["run.sh"].runnable != shRun {
		t.Fatalf("run.sh %+v", seen["run.sh"])
	}
	if string(mustReadFile(t, configPath)) != string(before) {
		t.Fatal("refused script runs changed tasks.json")
	}
	state, err := os.ReadFile(filepath.Join(pluginDir, "scheduled-tasks", "data", "state.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(state), `"scripts"`) {
		t.Fatalf("refused runs were recorded: %s", state)
	}
}
