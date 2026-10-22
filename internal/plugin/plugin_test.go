package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writePlugin puts manifest at <root>/<id>/plugin.yaml and returns its path.
func writePlugin(t *testing.T, root, id, manifest string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plugin.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestADeclarativeCardPluginLoads(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "demo", `
schema: 1
name: Demo
icon: D
card:
  http:
    url: http://127.0.0.1:3000/api/health
  value: database
  label: {en: Database, zh-CN: 数据库}
`)
	plugins, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{{
		ID:   "demo",
		Name: Text{All: "Demo"},
		Icon: "D",
		Card: &Card{
			HTTP:  &HTTP{URL: "http://127.0.0.1:3000/api/health"},
			Value: "database",
			Label: Text{ByLang: map[string]string{"en": "Database", "zh-CN": "数据库"}},
		},
	}}
	if !reflect.DeepEqual(plugins, want) {
		t.Errorf("got %+v, want %+v", plugins, want)
	}
}

func TestAnUnknownFieldIsReportedWithFileLineAndName(t *testing.T) {
	root := t.TempDir()
	path := writePlugin(t, root, "demo", `schema: 1
name: Demo
card:
  http:
    url: http://127.0.0.1:3000/
    methd: POST
  value: status
`)
	_, err := Load(root)
	if err == nil {
		t.Fatal("loaded a manifest with an unknown field")
	}
	for _, part := range []string{path, "line 6", `"methd"`} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %s", err, part)
		}
	}
}

func TestInvalidManifestsAreRejectedNamingTheProblem(t *testing.T) {
	const card = "card: {http: {url: 'http://127.0.0.1:3000/'}, value: status}\n"
	for _, tc := range []struct {
		manifest string
		mention  string
	}{
		{"name: Demo\n" + card, "schema"},
		{"schema: 2\nname: Demo\n" + card, "schema 2"},
		{"schema: 1\n" + card, "name"},
		{"schema: 1\nname: Demo\n", "card"},
		{"schema: 1\nname: Demo\ncard: {value: status}\n", "card.http"},
		{"schema: 1\nname: Demo\ncard: {http: {}, value: status}\n", "card.http.url"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/'}}\n", "card.value"},
		{"schema: 1\nname: {fr: Démo}\n" + card, `"fr"`},
	} {
		root := t.TempDir()
		path := writePlugin(t, root, "demo", tc.manifest)
		_, err := Load(root)
		if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tc.mention) {
			t.Errorf("manifest %q: err = %v, want one naming %s and %s", tc.manifest, err, path, tc.mention)
		}
	}
}
