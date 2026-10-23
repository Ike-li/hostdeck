package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/cli"
	"github.com/Ike-li/hostdeck/internal/console"
	"github.com/Ike-li/hostdeck/internal/layout"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/ports"
	"github.com/tidwall/gjson"
)

func runningConsole(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "plugins")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = console.Handler(console.Config{
		Origin: "http://" + srv.Listener.Addr().String(), PluginDir: root, BuiltIn: examples.Files,
		Reload: func() (console.Loaded, error) {
			ps, broken, err := plugin.Load(root)
			return console.Loaded{Plugins: ps, Broken: broken}, err
		},
	})
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL, root
}

func command(t *testing.T, want int, args ...string) gjson.Result {
	t.Helper()
	var out bytes.Buffer
	code := cli.Run(args, &out)
	if code != want || !json.Valid(out.Bytes()) {
		t.Fatalf("%v: exit %d, want %d; stdout %q", args, code, want, out.String())
	}
	return gjson.Parse(out.String())
}

func TestAnAgentCanAddAndEditAPageWithoutChangingItsOtherFields(t *testing.T) {
	url, root := runningConsole(t)
	added := command(t, 0, "page", "add", "--url", url, "--address", "https://example.test/", "--mode", "link", "--name", "Notes")
	id := added.Get("data.id").String()
	if !added.Get("ok").Bool() || id == "" || !added.Get("data.loaded").Bool() {
		t.Fatalf("missing loaded page: %s", added.Raw)
	}
	path := filepath.Join(root, id, "plugin.yaml")
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest = append(manifest, []byte("# Keep this comment\nicon: N\n")...)
	if err := os.WriteFile(path, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	command(t, 0, "page", "update", "--url", url, "--id", id, "--name", "New notes", "--link", "https://example.test/new")
	after, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(after), "# Keep this comment") || !strings.Contains(string(after), "icon: N") || !strings.Contains(string(after), "New notes") || !strings.Contains(string(after), "/new") {
		t.Fatalf("page update lost other fields: %s %v", after, err)
	}
}

func TestInvalidCLIArgumentsDoNotChangeData(t *testing.T) {
	url, root := runningConsole(t)
	for _, args := range [][]string{
		{"page", "add", "--url", url},
		{"page", "add", "--url", url, "--address", "https://example.test", "--mode", "other"},
		{"page", "update", "--url", url, "--id", "../outside", "--name", "No"},
		{"page", "update", "--url", url, "--id", "notes"},
		{"page", "update", "--url", url, "--id", "notes", "--proxy", "http://127.0.0.1:1", "--link", "https://example.test"},
		{"page", "add", "--url", url, "--address", "https://example.test", "unexpected"},
		{"plugin", "unknown"},
	} {
		got := command(t, 2, args...)
		if got.Get("ok").Bool() || got.Get("error.code").String() != "usage" {
			t.Fatalf("want a machine-readable usage error: %s", got.Raw)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid arguments wrote data: %v %v", entries, err)
	}
}

func TestValidationReadsTheManifestWithoutRunningPluginPrograms(t *testing.T) {
	source := t.TempDir()
	manifest := "schema: 1\nname: Demo\ncard: {exec: {command: [sh, card.sh]}}\n"
	if err := os.WriteFile(filepath.Join(source, "plugin.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "card.sh"), []byte("touch executed\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got := command(t, 0, "plugin", "validate", "--from", source)
	if !got.Get("data.valid").Bool() || got.Get("data.schema").Int() != 1 {
		t.Fatalf("validation result: %s", got.Raw)
	}
	if _, err := os.Stat(filepath.Join(source, "executed")); !os.IsNotExist(err) {
		t.Fatalf("validation ran the plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "plugin.yaml"), []byte(manifest+"unknown: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := command(t, 1, "plugin", "validate", "--from", source)
	if bad.Get("error.code").String() != "invalid_manifest" {
		t.Fatalf("invalid manifest not rejected: %s", bad.Raw)
	}
}

func TestAnAgentCanInstallBuiltInsAndChangeTheirEnablement(t *testing.T) {
	url, _ := runningConsole(t)
	installed := command(t, 0, "plugin", "install", "--url", url, "--id", "ollama")
	if !installed.Get("data.loaded").Bool() {
		t.Fatalf("built-in not loaded: %s", installed.Raw)
	}
	listed := command(t, 0, "plugin", "catalog", "--url", url)
	if listed.Get(`data.installed.#(id=="ollama").id`).String() != "ollama" {
		t.Fatal(listed.Raw)
	}
	disabled := command(t, 0, "plugin", "disable", "--url", url, "--id", "ollama")
	if !disabled.Get("data.disabled").Bool() {
		t.Fatal(disabled.Raw)
	}
	if _, err := os.Stat(disabled.Get("data.path").String()); err != nil {
		t.Fatalf("disabled path does not exist: %s %v", disabled.Raw, err)
	}
	command(t, 0, "plugin", "enable", "--url", url, "--id", "ollama")
	conflict := command(t, 1, "plugin", "install", "--url", url, "--id", "ollama")
	if conflict.Get("error.code").String() != "already_added" {
		t.Fatalf("wrong conflict code: %s", conflict.Raw)
	}
}

func TestALocalPluginIsValidatedCopiedAndReloadedWithoutOverwritingEitherState(t *testing.T) {
	url, root := runningConsole(t)
	source := t.TempDir()
	manifest := "schema: 1\nname: Local\n# Keep this comment\ncard: {exec: {command: [sh, card.sh]}}\n"
	if err := os.WriteFile(filepath.Join(source, "plugin.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "card.sh"), []byte("touch executed\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got := command(t, 0, "plugin", "install", "--url", url, "--id", "local", "--from", source)
	if !got.Get("data.loaded").Bool() {
		t.Fatalf("local plugin not loaded: %s", got.Raw)
	}
	bytes, err := os.ReadFile(filepath.Join(root, "local", "plugin.yaml"))
	if err != nil || string(bytes) != manifest {
		t.Fatalf("manifest changed: %s %v", bytes, err)
	}
	if info, err := os.Stat(filepath.Join(root, "local", "card.sh")); err != nil || info.Size() == 0 {
		t.Fatalf("script not copied: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0100 == 0 {
		t.Fatal("copying removed the script's executable bit")
	}
	if _, err := os.Stat(filepath.Join(root, "local", "executed")); !os.IsNotExist(err) {
		t.Fatalf("install ran the script: %v", err)
	}
	for _, disabled := range []bool{false, true} {
		if disabled {
			command(t, 0, "plugin", "disable", "--url", url, "--id", "local")
		}
		refused := command(t, 1, "plugin", "install", "--url", url, "--id", "local", "--from", source)
		if refused.Get("error.code").String() != "already_added" {
			t.Fatalf("existing ID overwritten: %s", refused.Raw)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "plugin.yaml"), []byte("schema: 99\nname: Broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := command(t, 1, "plugin", "install", "--url", url, "--id", "bad", "--from", source)
	if bad.Get("error.code").String() != "invalid_manifest" {
		t.Fatal(bad.Raw)
	}
	if _, err := os.Stat(filepath.Join(root, "bad")); !os.IsNotExist(err) {
		t.Fatalf("failed validation left a plugin: %v", err)
	}
}

func TestReloadReportsBrokenPluginsInsteadOfClaimingTheyAreAvailable(t *testing.T) {
	url, root := runningConsole(t)
	if err := os.Mkdir(filepath.Join(root, "bad"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bad", "plugin.yaml"), []byte("schema: 99\nname: Bad\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := command(t, 1, "plugin", "reload", "--url", url)
	if got.Get("error.code").String() != "plugins_unavailable" || !got.Get(`data.installed.#(id=="bad").broken`).Bool() {
		t.Fatalf("reload hid broken plugins: %s", got.Raw)
	}
}

func TestCLIRefusesRemoteTargetsCredentialsAndRedirects(t *testing.T) {
	for _, target := range []string{"https://127.0.0.1:4678", "http://localhost:4678", "http://192.168.1.1", "http://user:pass@127.0.0.1:4678", "http://127.0.0.1:4678/path", "http://127.0.0.1:4678?token=hidden", "http://127.0.0.1:0"} {
		command(t, 2, "plugin", "list", "--url", target)
	}
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`{}`)) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	got := command(t, 1, "plugin", "list", "--url", redirect.URL)
	if got.Get("error.code").String() != "connection_failed" || calls != 0 {
		t.Fatalf("CLI followed a redirect: %s calls %d", got.Raw, calls)
	}
}

func TestAForwardingFailureIsReportedAfterThePageWasSaved(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "web"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "plugin.yaml"), []byte("schema: 1\nname: Web\npage: {proxy: 'http://127.0.0.1:9/'}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := layout.Open(filepath.Join(t.TempDir(), "layout.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(layout.Layout{Hidden: []string{"web"}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = console.Handler(console.Config{Origin: "http://" + srv.Listener.Addr().String(), PluginDir: root, Layout: store, BuiltIn: examples.Files, Reload: func() (console.Loaded, error) {
		ps, broken, err := plugin.Load(root)
		return console.Loaded{Plugins: ps, Broken: broken, Served: map[string]console.Served{"web": {Err: &ports.InUse{Port: 4679}}}}, err
	}})
	srv.Start()
	defer srv.Close()
	got := command(t, 1, "page", "update", "--url", srv.URL, "--id", "web", "--name", "Updated")
	if !got.Get("data.saved").Bool() || got.Get("error.code").String() != "port_unavailable" {
		t.Fatalf("forwarding failure was hidden: %s", got.Raw)
	}
}

func TestASavedLocalInstallCanRecoverFromAReloadFailure(t *testing.T) {
	root := t.TempDir()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.yaml"), []byte("schema: 1\nname: Notes\npage: {link: 'https://example.test/'}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var blocked atomic.Bool
	blocked.Store(true)
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = console.Handler(console.Config{Origin: "http://" + srv.Listener.Addr().String(), PluginDir: root, BuiltIn: examples.Files, Reload: func() (console.Loaded, error) {
		if blocked.Load() {
			return console.Loaded{}, errors.New("cannot read plugins")
		}
		ps, broken, err := plugin.Load(root)
		return console.Loaded{Plugins: ps, Broken: broken}, err
	}})
	srv.Start()
	defer srv.Close()
	got := command(t, 1, "plugin", "install", "--url", srv.URL, "--id", "notes", "--from", source)
	if got.Get("error.code").String() != "reload_failed" || !got.Get("data.saved").Bool() || got.Get("data.loaded").Bool() {
		t.Fatalf("saved state was hidden: %s", got.Raw)
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	blocked.Store(false)
	recovered := command(t, 0, "plugin", "reload", "--url", srv.URL)
	if !recovered.Get(`data.installed.#(id=="notes").loaded`).Bool() {
		t.Fatal(recovered.Raw)
	}
	blocked.Store(true)
	edit := command(t, 1, "page", "update", "--url", srv.URL, "--id", "notes", "--name", "Changed")
	if !edit.Get("data.saved").Bool() || edit.Get("data.id").String() != "notes" || edit.Get("error.code").String() != "reload_failed" {
		t.Fatalf("API edit failure hid the saved change: %s", edit.Raw)
	}
}

func TestLocalInstallationRejectsSourcesThatContainItsDestinationOrSymlinks(t *testing.T) {
	url, root := runningConsole(t)
	manifest := []byte("schema: 1\nname: Notes\npage: {link: 'https://example.test/'}\n")
	source := filepath.Dir(root)
	if err := os.WriteFile(filepath.Join(source, "plugin.yaml"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	got := command(t, 1, "plugin", "install", "--url", url, "--id", "recursive", "--from", source)
	if got.Get("error.code").String() != "invalid_source" {
		t.Fatal(got.Raw)
	}
	t.Run("symlink", func(t *testing.T) {
		source := t.TempDir()
		if err := os.WriteFile(filepath.Join(source, "plugin.yaml"), manifest, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "not-a-file"), filepath.Join(source, "helper")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		got := command(t, 1, "plugin", "install", "--url", url, "--id", "symlink", "--from", source)
		if got.Get("error.code").String() != "invalid_source" {
			t.Fatal(got.Raw)
		}
	})
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("refused installs left files: %v %v", entries, err)
	}
}

func TestAnUnrelatedJSONServiceCannotMasqueradeAsTheConsole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	for _, action := range []string{"list", "catalog", "reload"} {
		got := command(t, 1, "plugin", action, "--url", srv.URL)
		if got.Get("error.code").String() != "invalid_response" {
			t.Fatal(got.Raw)
		}
	}
}
