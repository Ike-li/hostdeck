package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

// loadAll loads root and turns the first plugin that did not load into the
// error.
func loadAll(root string) ([]Plugin, error) {
	plugins, broken, err := Load(root)
	if err == nil && len(broken) > 0 {
		err = broken[0].Err
	}
	return plugins, err
}

func TestABrokenPluginIsSetAsideAndTheOthersLoad(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "good", "schema: 1\nname: Good\npage: {link: 'https://wiki.example/'}\n")
	bad := writePlugin(t, root, "bad", "schema: 1\nname: Bad\npage: {lnk: 'https://wiki.example/'}\n")
	os.MkdirAll(filepath.Join(root, "empty"), 0o755)

	plugins, broken, err := Load(root)

	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].ID != "good" {
		t.Errorf("loaded %+v, want only good", plugins)
	}
	if len(broken) != 2 || broken[0].ID != "bad" || broken[1].ID != "empty" {
		t.Fatalf("broken %+v, want bad and empty", broken)
	}
	if msg := broken[0].Err.Error(); !strings.Contains(msg, bad) || !strings.Contains(msg, `"lnk"`) {
		t.Errorf("bad: %q, want its file and the unknown field", msg)
	}
	if msg := broken[1].Err.Error(); !strings.Contains(msg, "plugin.yaml") {
		t.Errorf("empty: %q, want it to say plugin.yaml is missing", msg)
	}
}

func TestABrokenManifestKeepsItsName(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "bad", "schema: 1\nname: {en: Broken, zh-CN: 坏了}\npage: {lnk: 'http://127.0.0.1:1/'}\n")

	_, broken, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 1 {
		t.Fatalf("broken %+v, want the one plugin", broken)
	}
	want := Text{ByLang: map[string]string{"en": "Broken", "zh-CN": "坏了"}}
	if !reflect.DeepEqual(broken[0].Name, want) {
		t.Errorf("name %+v, want %+v", broken[0].Name, want)
	}
}

func TestAMissingEnvironmentVariableNamesTheVariable(t *testing.T) {
	if _, ok := os.LookupEnv("HOSTDECK_TEST_ABSENT"); ok {
		t.Fatal("HOSTDECK_TEST_ABSENT is set; this test needs it unset")
	}
	root := t.TempDir()
	writePlugin(t, root, "sync", `schema: 1
name: Syncthing
card:
  http:
    url: http://127.0.0.1:9/rest
    headers: {X-API-Key: "${env:HOSTDECK_TEST_ABSENT}"}
  value: n
`)

	_, broken, err := Load(root)
	if err != nil || len(broken) != 1 {
		t.Fatalf("broken %+v, err %v; want the one plugin", broken, err)
	}
	var missing *EnvMissing
	var located *Located
	if !errors.As(broken[0].Err, &located) || !errors.As(broken[0].Err, &missing) {
		t.Fatalf("err %T %v, want a located missing variable", broken[0].Err, broken[0].Err)
	}
	if missing.Name != "HOSTDECK_TEST_ABSENT" || broken[0].Name.All != "Syncthing" {
		t.Errorf("variable %q name %q, want HOSTDECK_TEST_ABSENT and Syncthing", missing.Name, broken[0].Name.All)
	}
	if strings.Contains(missing.Error(), located.Path) {
		t.Errorf("the reason %q contains the path", missing.Error())
	}
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
	plugins, err := loadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{{
		ID:   "demo",
		Dir:  filepath.Join(root, "demo"),
		Name: Text{All: "Demo"},
		Icon: "D",
		Card: &Card{
			Interval: DefaultInterval,
			HTTP:     &HTTP{URL: "http://127.0.0.1:3000/api/health"},
			Value:    "database",
			Label:    Text{ByLang: map[string]string{"en": "Database", "zh-CN": "数据库"}},
		},
	}}
	if !reflect.DeepEqual(plugins, want) {
		t.Errorf("got %+v, want %+v", plugins, want)
	}
}

func TestADeclarativeCardWithThresholdsRowsAndSecretsLoads(t *testing.T) {
	t.Setenv("DEMO_KEY", "k")
	t.Setenv("DEMO_TOKEN", "t")
	root := t.TempDir()
	writePlugin(t, root, "demo", `schema: 1
name: Demo
card:
  interval: 30s
  http:
    url: http://127.0.0.1:3000/api/health?key=${env:DEMO_KEY}
    headers: {Authorization: "Bearer ${env:DEMO_TOKEN}"}
  value: cpu
  warn: {above: 80}
  error: {above: 95}
  rows:
    - {name: {en: Database, zh-CN: 数据库}, value: database, error: {not: ok}}
    - {name: Replicas, value: replicas, error: {equals: 0}}
`)
	plugins, err := loadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	above := func(f float64) *float64 { return &f }
	scalar := func(s Scalar) *Scalar { return &s }
	want := &Card{
		Interval: Duration(30 * time.Second),
		HTTP: &HTTP{
			URL:     "http://127.0.0.1:3000/api/health?key=${env:DEMO_KEY}",
			Headers: map[string]string{"Authorization": "Bearer ${env:DEMO_TOKEN}"},
		},
		Value: "cpu",
		Warn:  &Threshold{Above: above(80)},
		Error: &Threshold{Above: above(95)},
		Rows: []Row{
			{Name: Text{ByLang: map[string]string{"en": "Database", "zh-CN": "数据库"}}, Value: "database", Error: &Threshold{Not: scalar("ok")}},
			{Name: Text{All: "Replicas"}, Value: "replicas", Error: &Threshold{Equals: scalar("0")}},
		},
	}
	if !reflect.DeepEqual(plugins[0].Card, want) {
		t.Errorf("got %+v, want %+v", plugins[0].Card, want)
	}
}

func TestEnvironmentReferencesAreFilledInWithTheirValues(t *testing.T) {
	t.Setenv("DEMO_TOKEN", "s3cret")
	got, secrets := Expand("Bearer ${env:DEMO_TOKEN}, again ${env:DEMO_TOKEN}")
	if got != "Bearer s3cret, again s3cret" || !reflect.DeepEqual(secrets, []string{"s3cret", "s3cret"}) {
		t.Errorf("got %q and %q", got, secrets)
	}
}

func TestPagePluginsLoadWithoutACard(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "grafana", "schema: 1\nname: Grafana\npage: {proxy: 'http://127.0.0.1:3000/grafana/'}\n")
	writePlugin(t, root, "wiki", "schema: 1\nname: Wiki\npage: {link: 'https://wiki.example/'}\n")
	plugins, err := loadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{
		{ID: "grafana", Dir: filepath.Join(root, "grafana"), Name: Text{All: "Grafana"}, Page: &Page{Proxy: "http://127.0.0.1:3000/grafana/"}},
		{ID: "wiki", Dir: filepath.Join(root, "wiki"), Name: Text{All: "Wiki"}, Page: &Page{Link: "https://wiki.example/"}},
	}
	if !reflect.DeepEqual(plugins, want) {
		t.Errorf("got %+v, want %+v", plugins, want)
	}
}

func TestAnExecCardPluginLoads(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "disk", `schema: 1
name: Disk
card:
  exec:
    command: [sh, card.sh]
    windows: [powershell, -File, card.ps1]
`)
	plugins, err := loadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{{
		ID:   "disk",
		Dir:  filepath.Join(root, "disk"),
		Name: Text{All: "Disk"},
		Card: &Card{Interval: DefaultInterval, Exec: &Exec{Command: []string{"sh", "card.sh"}, Windows: []string{"powershell", "-File", "card.ps1"}}},
	}}
	if !reflect.DeepEqual(plugins, want) {
		t.Errorf("got %+v, want %+v", plugins, want)
	}
}

func TestADetailPageWithActionsLoads(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "disk", `schema: 1
name: Disk
detail:
  exec: {command: [sh, detail.sh]}
actions:
  exec: {command: [sh, action.sh], windows: [powershell, -File, action.ps1]}
`)
	plugins, err := loadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{{
		ID:      "disk",
		Dir:     filepath.Join(root, "disk"),
		Name:    Text{All: "Disk"},
		Detail:  &Detail{Exec: &Exec{Command: []string{"sh", "detail.sh"}}},
		Actions: &Actions{Exec: &Exec{Command: []string{"sh", "action.sh"}, Windows: []string{"powershell", "-File", "action.ps1"}}},
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
	_, err := loadAll(root)
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
		{"schema: 1\nname: Demo\n", "page, detail or card"},
		{"schema: 1\nname: Demo\npage: {link: 'https://wiki.example/'}\ndetail: {exec: {command: [sh, d.sh]}}\n", "page or detail"},
		{"schema: 1\nname: Demo\ndetail: {}\n", "detail.exec.command"},
		{"schema: 1\nname: Demo\ndetail: {exec: {}}\n", "detail.exec.command"},
		{"schema: 1\nname: Demo\npage: {link: 'https://wiki.example/'}\nactions: {exec: {command: [sh, a.sh]}}\n", "actions"},
		{"schema: 1\nname: Demo\ndetail: {exec: {command: [sh, d.sh]}}\nactions: {exec: {}}\n", "actions.exec.command"},
		{"schema: 1\nname: Demo\npage: {}\n", "page.proxy or page.link"},
		{"schema: 1\nname: Demo\npage: {proxy: 'http://127.0.0.1:3000/', link: 'http://127.0.0.1:3000/'}\n", "page.proxy or page.link"},
		{"schema: 1\nname: Demo\npage: {proxy: '127.0.0.1:3000'}\n", "page.proxy"},
		{"schema: 1\nname: Demo\npage: {link: 'ftp://files.example/'}\n", "page.link"},
		{"schema: 1\nname: Demo\ncard: {value: status}\n", "card.http or card.exec"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/'}, exec: {command: [sh]}, value: v}\n", "card.http or card.exec"},
		{"schema: 1\nname: Demo\ncard: {exec: {}}\n", "card.exec.command"},
		{"schema: 1\nname: Demo\ncard: {exec: {command: [sh, card.sh]}, value: status}\n", "card.value"},
		{"schema: 1\nname: Demo\ncard: {http: {}, value: status}\n", "card.http.url"},
		{"schema: 1\nname: Demo\ncard: {http: {url: '127.0.0.1:8080/health'}, value: status}\n", "card.http.url"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'ftp://127.0.0.1/x'}, value: status}\n", "card.http.url"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/'}}\n", "card.value"},
		{"schema: 1\nname: {fr: Démo}\n" + card, `"fr"`},
		{"schema: 1\nname: Demo\ncard: {interval: soon, http: {url: 'http://127.0.0.1:3000/'}, value: v}\n", "card.interval"},
		{"schema: 1\nname: Demo\ncard: {interval: -5s, http: {url: 'http://127.0.0.1:3000/'}, value: v}\n", "card.interval"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/'}, value: v, warn: {}}\n", "card.warn"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/'}, value: v, warn: {above: 1, below: 2}}\n", "card.warn"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/'}, value: v, rows: [{name: X, error: {not: ok}}]}\n", "card.rows[0].value"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/'}, value: v, rows: [{name: X, value: x, error: {}}]}\n", "card.rows[0].error"},
		{"schema: 1\nname: Demo\ncard: {exec: {command: [sh, c.sh]}, rows: [{name: X, value: x}]}\n", "card.http"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/?k=${env:HOSTDECK_TEST_UNSET}'}, value: v}\n", "HOSTDECK_TEST_UNSET"},
		{"schema: 1\nname: Demo\ncard: {http: {url: 'http://127.0.0.1:3000/', headers: {X-Key: '${KEY}'}}, value: v}\n", "${env:NAME}"},
	} {
		root := t.TempDir()
		path := writePlugin(t, root, "demo", tc.manifest)
		_, err := loadAll(root)
		if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tc.mention) {
			t.Errorf("manifest %q: err = %v, want one naming %s and %s", tc.manifest, err, path, tc.mention)
		}
	}
}

func TestAPluginMayDescribeItself(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "demo", "schema: 1\nname: Demo\ndescription: {en: Shows a demo., zh-CN: 演示用。}\npage: {link: 'http://127.0.0.1:3000/'}\n")

	got, err := loadAll(root)

	if err != nil {
		t.Fatal(err)
	}
	if d := got[0].Description; d.ByLang["en"] != "Shows a demo." || d.ByLang["zh-CN"] != "演示用。" {
		t.Errorf("description %+v", d)
	}
}
