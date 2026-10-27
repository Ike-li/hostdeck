package console

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func TestTheScheduledTaskPageCanSaveAndDeleteAScript(t *testing.T) {
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
	script := filepath.Join(pluginDir, "scheduled-tasks", "scripts", "hello.py")

	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", `{"name":"hello.py","content":"print(1)\n"}`, map[string]string{"Origin": "http://127.0.0.1:4679", "Sec-Fetch-Site": "same-site"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin HTTP %d", resp.StatusCode)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatal("foreign origin wrote a script")
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", `{"name":"../hello.py","content":"x"}`, fromConsole); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("traversal HTTP %d", resp.StatusCode)
	}
	resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", `{"name":"hello.py","content":"print('你好')\n"}`, fromConsole)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create HTTP %d %s", resp.StatusCode, body)
	}
	if got := string(mustReadFile(t, script)); got != "print('你好')\n" {
		t.Fatalf("file %q", got)
	}
	list := get(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", nil)
	var listed struct {
		Files []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
	}
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil || len(listed.Files) != 1 || listed.Files[0].Name != "hello.py" {
		t.Fatalf("list %+v %v", listed, err)
	}
	one := get(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts/hello.py", nil)
	var file struct{ Content string }
	if err := json.NewDecoder(one.Body).Decode(&file); err != nil || file.Content != "print('你好')\n" {
		t.Fatalf("read %q %v", file.Content, err)
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts/hello.py", `{"content":"print(2)\n"}`, fromConsole); resp.StatusCode != http.StatusOK {
		t.Fatalf("update HTTP %d", resp.StatusCode)
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", `{"name":"hello.py","content":"x"}`, fromConsole); resp.StatusCode != http.StatusConflict || errorCode(t, resp) != "script_exists" {
		t.Fatalf("duplicate HTTP %d", resp.StatusCode)
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts/hello.py/delete", ``, fromConsole); resp.StatusCode != http.StatusOK {
		t.Fatalf("delete HTTP %d", resp.StatusCode)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatal("script remains")
	}
	missing := get(h, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts/hello.py", nil)
	if missing.StatusCode != http.StatusNotFound || errorCode(t, missing) != "script_missing" {
		t.Fatalf("missing HTTP %d", missing.StatusCode)
	}

	absent := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: t.TempDir(), Reload: func() (Loaded, error) { return Loaded{}, nil }})
	if resp := postJSON(absent, "http://127.0.0.1:4678/api/plugins/scheduled-tasks/scripts", `{"name":"hello.py","content":"x"}`, fromConsole); resp.StatusCode != http.StatusNotFound || errorCode(t, resp) != "not_installed" {
		t.Fatalf("absent plugin HTTP %d", resp.StatusCode)
	}
}

func errorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Error struct{ Code string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Error.Code
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
