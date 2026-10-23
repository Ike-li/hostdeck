package console

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/tidwall/gjson"
)

func TestExplicitReloadRequiresTheConsoleOriginAndLoadsFilesWithoutWritingThem(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "notes", "schema: 1\nname: Notes\npage: {link: 'https://example.test/'}\n")
	calls := 0
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: root, BuiltIn: fstest.MapFS{}, Reload: func() (Loaded, error) {
		calls++
		ps, broken, err := plugin.Load(root)
		return Loaded{Plugins: ps, Broken: broken}, err
	}})
	for _, origin := range []string{"", "http://127.0.0.1:4679", "https://outside.test"} {
		resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/reload", map[string]string{"Origin": origin})
		if resp.StatusCode != http.StatusForbidden || calls != 0 {
			t.Fatalf("untrusted reload: status %d, calls %d", resp.StatusCode, calls)
		}
	}
	resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/reload", map[string]string{"Origin": "http://127.0.0.1:4678"})
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || calls != 1 || !gjson.GetBytes(body, `installed.#(id=="notes").loaded`).Bool() {
		t.Fatalf("trusted reload did not load manual files: %d %s, calls %d", resp.StatusCode, body, calls)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "notes", "plugin.yaml"))
	if err != nil || string(manifest) != "schema: 1\nname: Notes\npage: {link: 'https://example.test/'}\n" {
		t.Fatalf("reload changed the manifest: %s %v", manifest, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("reload wrote plugin files: %v %v", entries, err)
	}
}

func TestAnExplicitReloadFailureKeepsThePreviousLoadedPlugins(t *testing.T) {
	h := Handler(Config{Origin: "http://127.0.0.1:4678", Plugins: []plugin.Plugin{{ID: "old", Name: plugin.Text{All: "Old"}, Page: &plugin.Page{Link: "https://example.test/"}}}, Reload: func() (Loaded, error) { return Loaded{}, errors.New("cannot read plugins") }})
	resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/reload", map[string]string{"Origin": "http://127.0.0.1:4678"})
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError || gjson.GetBytes(body, "error.code").String() != "reload_failed" {
		t.Fatalf("reload error: %d %s", resp.StatusCode, body)
	}
	loaded, _ := io.ReadAll(get(h, "http://127.0.0.1:4678/api/plugins", nil).Body)
	if gjson.GetBytes(loaded, `#(id=="old").name`).String() != "Old" {
		t.Fatalf("reload lost the old plugin: %s", loaded)
	}
}
