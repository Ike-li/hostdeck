package console

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/appearance"
)

func appearanceConsole(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	return Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: filepath.Join(dir, "plugins"), Appearance: appearance.Open(filepath.Join(dir, "appearance.json"))}), dir
}

func saveAppearance(h http.Handler, origin, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4678/api/appearance", strings.NewReader(body))
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestOnlyTheConsoleCanSaveTheAppearanceAndInvalidInputWritesNothing(t *testing.T) {
	h, dir := appearanceConsole(t)
	valid := `{"version":1,"preset":"sand","mode":"dark","accent":"#AB1234"}`
	for _, origin := range []string{"", "http://127.0.0.1:4679", "https://other.test"} {
		if w := saveAppearance(h, origin, valid); w.Code != 403 || !strings.Contains(w.Body.String(), "not_console") {
			t.Fatalf("origin %q: %d %s", origin, w.Code, w.Body)
		}
	}
	if w := saveAppearance(h, "http://127.0.0.1:4678", `{}`); w.Code != 400 {
		t.Fatalf("invalid input: %d %s", w.Code, w.Body)
	}
	file := filepath.Join(dir, "appearance.json")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("refused save created file: %v", err)
	}
	w := saveAppearance(h, "http://127.0.0.1:4678", valid)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accent":"#ab1234"`) {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	read := get(h, "http://127.0.0.1:4678/api/appearance", nil)
	if got := decodeJSON(t, read); !strings.Contains(stringMustJSON(got), `"preset":"sand"`) {
		t.Fatalf("read: %+v", got)
	}
	if err := os.Mkdir(file+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if w := saveAppearance(h, "http://127.0.0.1:4678", `{"version":1,"preset":"forest","mode":"light"}`); w.Code != 500 || !strings.Contains(w.Body.String(), "appearance_failed") {
		t.Fatalf("failed save: %d %s", w.Code, w.Body)
	}
	if got, _ := appearance.Open(file).Get(); got.Preset != "sand" {
		t.Fatalf("failed save changed file: %+v", got)
	}
}

func stringMustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestTheFirstPageCarriesTheSavedAppearanceWithoutCachingOrInlineScripts(t *testing.T) {
	h, _ := appearanceConsole(t)
	if w := saveAppearance(h, "http://127.0.0.1:4678", `{"version":1,"preset":"dusk","mode":"dark"}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
	for _, path := range []string{"/", "/index.html"} {
		resp := get(h, "http://127.0.0.1:4678"+path, nil)
		data, _ := io.ReadAll(resp.Body)
		m := regexp.MustCompile(`data-appearance="([^"]+)"`).FindSubmatch(data)
		if len(m) != 2 || !strings.Contains(html.UnescapeString(string(m[1])), `"preset":"dusk"`) {
			t.Fatalf("%s has no saved appearance: %s", path, data[:min(len(data), 500)])
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("page may cache stale settings")
		}
		if strings.Contains(resp.Header.Get("Content-Security-Policy"), "unsafe-inline") {
			t.Fatal("appearance weakened CSP")
		}
		init, body := strings.Index(string(data), `src="/theme.js"`), strings.Index(string(data), "<body>")
		if init < 0 || init > body {
			t.Fatal("theme does not initialize before the body")
		}
	}
}

func TestABrokenAppearanceCanBeReadAndExplicitlyReplacedWithoutBlockingTheConsole(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "appearance.json")
	if err := os.WriteFile(file, []byte(`broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: filepath.Join(dir, "plugins"), Appearance: appearance.Open(file)})
	resp := get(h, "http://127.0.0.1:4678/api/appearance", nil)
	if got := stringMustJSON(decodeJSON(t, resp)); !strings.Contains(got, "appearance_invalid") || !strings.Contains(got, `"preset":"default"`) {
		t.Fatal(got)
	}
	if data, _ := os.ReadFile(file); string(data) != "broken" {
		t.Fatal("read overwrote broken file")
	}
	if w := saveAppearance(h, "http://127.0.0.1:4678", `{"version":1,"preset":"default","mode":"system"}`); w.Code != 200 || strings.Contains(w.Body.String(), "appearance_invalid") {
		t.Fatalf("repair: %d %s", w.Code, w.Body)
	}
}
