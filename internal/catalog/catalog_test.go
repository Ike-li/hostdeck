package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

var builtIn = fstest.MapFS{
	"disk/plugin.yaml": {Data: []byte("schema: 1\nname: {en: Disk space, zh-CN: 磁盘空间}\ndescription: {en: Free space., zh-CN: 可用空间。}\ncard: {exec: {command: [sh, card.sh]}}\n")},
	"disk/card.sh":     {Data: []byte("echo '{}'\n")},
	// A plugin whose manifest needs an environment variable is still listed.
	"keyed/plugin.yaml": {Data: []byte("schema: 1\nname: Keyed\ndescription: Needs a key.\ncard: {http: {url: 'http://127.0.0.1:1/?k=${env:HOSTDECK_TEST_UNSET}'}, value: v}\n")},
}

func TestTheBuiltInPluginsAreListedWithWhetherTheyAreAdded(t *testing.T) {
	pluginDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(pluginDir, "keyed"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := List(builtIn, pluginDir)

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want disk and keyed", got)
	}
	disk, keyed := got[0], got[1]
	if disk.ID != "disk" || disk.Name.ByLang["zh-CN"] != "磁盘空间" || disk.Description.ByLang["en"] != "Free space." || disk.Added {
		t.Errorf("disk: %+v", disk)
	}
	if keyed.ID != "keyed" || keyed.Description.All != "Needs a key." || !keyed.Added {
		t.Errorf("keyed: %+v, want it listed as added", keyed)
	}
}

func TestAddingABuiltInPluginCopiesItsFolder(t *testing.T) {
	pluginDir := t.TempDir()

	path, err := Add(builtIn, pluginDir, "disk")

	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(pluginDir, "disk") {
		t.Errorf("added at %q", path)
	}
	for _, name := range []string{"plugin.yaml", "card.sh"} {
		got, err := os.ReadFile(filepath.Join(pluginDir, "disk", name))
		if err != nil || string(got) != string(builtIn["disk/"+name].Data) {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
	if entries, _ := os.ReadDir(pluginDir); len(entries) != 1 {
		t.Errorf("the plugins folder holds %v, want only disk", entries)
	}
}

func TestAddingNeverOverwritesAFolderThatIsThere(t *testing.T) {
	pluginDir := t.TempDir()
	mine := filepath.Join(pluginDir, "disk", "plugin.yaml")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("my own"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Add(builtIn, pluginDir, "disk")

	if !errors.Is(err, ErrAdded) {
		t.Errorf("err = %v, want ErrAdded", err)
	}
	if got, _ := os.ReadFile(mine); string(got) != "my own" {
		t.Errorf("the folder that was there now holds %q", got)
	}
}

func TestOnlyBuiltInPluginsCanBeAdded(t *testing.T) {
	pluginDir := filepath.Join(t.TempDir(), "plugins")
	if err := os.Mkdir(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"nope", "", ".", "..", "../disk", "disk/card.sh", "disk/../disk"} {
		if _, err := Add(builtIn, pluginDir, id); !errors.Is(err, ErrUnknown) {
			t.Errorf("Add(%q): err = %v, want ErrUnknown", id, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Dir(pluginDir)); len(entries) != 1 {
		t.Errorf("next to the plugins folder: %v, want nothing written", entries)
	}
}
