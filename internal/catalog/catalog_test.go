package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

var builtIn = fstest.MapFS{
	"disk/plugin.yaml": {Data: []byte("schema: 1\nname: {en: Disk space, zh-CN: 磁盘空间}\ndescription: {en: Free space., zh-CN: 可用空间。}\ncard: {exec: {command: [sh, card.sh]}}\ndetail: {exec: {command: [sh, detail.sh]}}\nactions: {exec: {command: [sh, action.sh]}}\n")},
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

// writeFolder makes <pluginDir>/<name>/plugin.yaml holding manifest.
func writeFolder(t *testing.T, pluginDir, name, manifest string) string {
	t.Helper()
	dir := filepath.Join(pluginDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstalledListsEnabledAndDisabledPlugins(t *testing.T) {
	pluginDir := t.TempDir()
	writeFolder(t, pluginDir, "mine", "schema: 1\nname: {en: Mine, zh-CN: 我的}\n")
	writeFolder(t, pluginDir, ".off", "schema: 1\nname: Off\ndescription: Was on.\n")
	writeFolder(t, pluginDir, "broken", "this is: [not yaml")
	// Neither of these is a disabled plugin.
	if err := os.MkdirAll(filepath.Join(pluginDir, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Installed(pluginDir)

	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{
		{ID: "broken", Name: plugin.Text{All: "broken"}},
		{ID: "mine", Name: plugin.Text{ByLang: map[string]string{"en": "Mine", "zh-CN": "我的"}}},
		{ID: "off", Name: plugin.Text{All: "Off"}, Description: plugin.Text{All: "Was on."}, Disabled: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestAPluginListsThePageCardDetailAndActionsItProvides(t *testing.T) {
	pluginDir := t.TempDir()
	writeFolder(t, pluginDir, "board", "schema: 1\nname: Board\npage: {proxy: 'http://127.0.0.1:9/'}\ncard: {http: {url: 'http://127.0.0.1:9/health'}, value: v}\n")
	writeFolder(t, pluginDir, "list", "schema: 1\nname: List\ndetail: {exec: {command: [sh, d.sh]}}\nactions: {exec: {command: [sh, a.sh]}}\n")
	writeFolder(t, pluginDir, "only", "schema: 1\nname: Only\ncard: {exec: {command: [sh, c.sh]}}\n")
	writeFolder(t, pluginDir, "out", "schema: 1\nname: Out\npage: {link: 'https://example.test/'}\n")

	got, err := Installed(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	offers := map[string]Offers{}
	for _, p := range got {
		offers[p.ID] = p.Offers
	}
	want := map[string]Offers{
		"board": {Page: true, Card: true},
		"list":  {Detail: true, Actions: true},
		"only":  {Card: true},
		"out":   {Page: true},
	}
	if !reflect.DeepEqual(offers, want) {
		t.Errorf("installed offers %v, want %v", offers, want)
	}

	listed, err := List(builtIn, pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	if listed[0].ID != "disk" || listed[0].Offers != (Offers{Card: true, Detail: true, Actions: true}) {
		t.Errorf("built-in disk offers %+v, want a card, a detail page and actions", listed[0].Offers)
	}
}

func TestDisablingAndEnablingRenameTheFolderAndKeepItsFiles(t *testing.T) {
	pluginDir := t.TempDir()
	writeFolder(t, pluginDir, "mine", "schema: 1\nname: Mine # edited by hand\n")

	if err := Disable(pluginDir, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "mine")); err == nil {
		t.Error("the folder is still where hostdeck loads it")
	}
	if got, _ := os.ReadFile(filepath.Join(pluginDir, ".mine", "plugin.yaml")); string(got) != "schema: 1\nname: Mine # edited by hand\n" {
		t.Errorf("disabled manifest %q", got)
	}

	if err := Enable(pluginDir, "mine"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(pluginDir, "mine", "plugin.yaml")); string(got) != "schema: 1\nname: Mine # edited by hand\n" {
		t.Errorf("enabled manifest %q", got)
	}
}

func TestDisablingAndEnablingNeverReplaceAFolder(t *testing.T) {
	pluginDir := t.TempDir()
	writeFolder(t, pluginDir, "a", "schema: 1\nname: A on\n")
	writeFolder(t, pluginDir, ".a", "schema: 1\nname: A off\n")

	if err := Disable(pluginDir, "a"); !errors.Is(err, ErrTaken) {
		t.Errorf("disabling over a disabled copy: err = %v, want ErrTaken", err)
	}
	if err := Enable(pluginDir, "a"); !errors.Is(err, ErrTaken) {
		t.Errorf("enabling over an enabled copy: err = %v, want ErrTaken", err)
	}
	for name, want := range map[string]string{"a": "A on", ".a": "A off"} {
		if got, _ := os.ReadFile(filepath.Join(pluginDir, name, "plugin.yaml")); !strings.Contains(string(got), want) {
			t.Errorf("%s now holds %q", name, got)
		}
	}
}

func TestOnlyInstalledPluginsCanBeDisabledOrEnabled(t *testing.T) {
	pluginDir := filepath.Join(t.TempDir(), "plugins")
	writeFolder(t, pluginDir, "on", "schema: 1\nname: On\n")
	writeFolder(t, pluginDir, ".off", "schema: 1\nname: Off\n")
	if err := os.MkdirAll(filepath.Join(pluginDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", ".", "..", "../plugins", "nope", ".on", "on/plugin.yaml", "off"} {
		if err := Disable(pluginDir, id); !errors.Is(err, ErrUnknown) {
			t.Errorf("Disable(%q): err = %v, want ErrUnknown", id, err)
		}
	}
	for _, id := range []string{"", ".", "..", "nope", "on", ".off", "git"} {
		if err := Enable(pluginDir, id); !errors.Is(err, ErrUnknown) {
			t.Errorf("Enable(%q): err = %v, want ErrUnknown", id, err)
		}
	}
}

func TestABuiltInPluginThatIsDisabledCountsAsAdded(t *testing.T) {
	pluginDir := t.TempDir()
	writeFolder(t, pluginDir, ".disk", "schema: 1\nname: My disk\n")

	if _, err := Add(builtIn, pluginDir, "disk"); !errors.Is(err, ErrAdded) {
		t.Errorf("adding disk next to a disabled disk: err = %v, want ErrAdded", err)
	}
	entries, err := List(builtIn, pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	if !entries[0].Added {
		t.Errorf("disk: %+v, want it counted as added", entries[0])
	}
}

func TestAWebPageIsAddedAsAPluginThatEmbedsIt(t *testing.T) {
	pluginDir := t.TempDir()

	for _, tc := range []struct{ address, name, id, proxy, shown string }{
		{"http://127.0.0.1:8080/app/", "", "127-0-0-1-8080", "http://127.0.0.1:8080/app/", "127.0.0.1:8080"},
		{"localhost:3000", "我的: 应用", "localhost-3000", "http://localhost:3000", "我的: 应用"},
		{"https://192.168.1.20", "NAS", "192-168-1-20", "https://192.168.1.20", "NAS"},
		{"http://[::1]:9000/", "", "1-9000", "http://[::1]:9000/", "[::1]:9000"},
	} {
		id, err := AddPage(pluginDir, tc.address, tc.name)
		if err != nil || id != tc.id {
			t.Errorf("%s: id %q, %v; want %q", tc.address, id, err, tc.id)
			continue
		}
	}
	plugins, broken, err := plugin.Load(pluginDir)
	if err != nil || len(broken) != 0 {
		t.Fatalf("loading what was added: %v %v", broken, err)
	}
	got := map[string]string{}
	for _, p := range plugins {
		got[p.ID] = p.Name.All + " -> " + p.Page.Proxy
	}
	want := map[string]string{
		"127-0-0-1-8080": "127.0.0.1:8080 -> http://127.0.0.1:8080/app/",
		"localhost-3000": "我的: 应用 -> http://localhost:3000",
		"192-168-1-20":   "NAS -> https://192.168.1.20",
		"1-9000":         "[::1]:9000 -> http://[::1]:9000/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %v, want %v", got, want)
	}
}

func TestTheSamePageAddedTwiceGetsAFolderOfItsOwn(t *testing.T) {
	pluginDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(pluginDir, ".localhost-3000"), 0o755); err != nil {
		t.Fatal(err) // a disabled one counts
	}
	var ids []string
	for range 2 {
		id, err := AddPage(pluginDir, "localhost:3000", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if want := []string{"localhost-3000-2", "localhost-3000-3"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("got %v, want %v", ids, want)
	}
}

func TestOnlyPagesOnThisMachineOrTheLocalNetworkAreAdded(t *testing.T) {
	pluginDir := t.TempDir()
	for address, want := range map[string]error{
		"http://93.184.215.14/":  ErrNotLocal,
		"https://8.8.8.8:443":    ErrNotLocal,
		"http://[2001:db8::1]/":  ErrNotLocal,
		"ftp://127.0.0.1/":       ErrNotWeb,
		"http://":                ErrNotWeb,
		"":                       ErrNotWeb,
		"javascript:alert(1)":    ErrNotWeb,
		"http://10.0.0.5:8080/":  nil,
		"http://172.16.3.4/":     nil,
		"http://169.254.10.1/":   nil,
		"http://[fd00::5]:8080/": nil,
	} {
		if _, err := AddPage(pluginDir, address, ""); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", address, err, want)
		}
	}
	entries, _ := os.ReadDir(pluginDir)
	if len(entries) != 4 {
		t.Errorf("the plugins folder holds %d entries, want only the 4 local pages", len(entries))
	}
}
