package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/listenerview"
	"github.com/Ike-li/hostdeck/internal/observation"
	"github.com/Ike-li/hostdeck/internal/run"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

func TestOnlyMatchingLoadedBuiltInExecutionCanCreateObservations(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "jobs"), 0700)
	// A third party using the familiar id has no observation file capability.
	p := plugin.Plugin{ID: "jobs", Dir: filepath.Join(dir, "jobs")}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: dir, Plugins: []plugin.Plugin{p}, BuiltIn: examples.Files})
	for _, origin := range []string{"http://other", "http://127.0.0.1:4678"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:4678/api/observations/jobs", bytes.NewBufferString(`{"item":"demo"}`))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := http.StatusNotFound
		if origin == "http://other" {
			want = http.StatusForbidden
		}
		if w.Code != want {
			t.Fatalf("origin %s: %d %s", origin, w.Code, w.Body.String())
		}
	}
	var response map[string]any
	r := httptest.NewRequest("GET", "http://127.0.0.1:4678/api/operations/jobs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		json.Unmarshal(w.Body.Bytes(), &response)
		t.Fatalf("third party recent: %d %+v", w.Code, response)
	}
}

func TestAConfirmedJobOperationKeepsItsDelayedOutputAndExitAfterTheRequest(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd process boundary")
	}
	root := t.TempDir()
	plugins := filepath.Join(root, "plugins")
	os.Mkdir(plugins, 0700)
	installed, err := catalog.Add(examples.Files, plugins, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plugin.ReadFolder(installed)
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "job.log")
	os.WriteFile(logPath, []byte("old\n"), 0600)
	bin := filepath.Join(root, "bin")
	os.Mkdir(bin, 0700)
	fake := fmt.Sprintf(`#!/bin/sh
case "$1" in
print) case "$2" in */com.example.test) printf '\tstate = not running\n\tlast exit code = 23\n\truns = 1\n\tstdout path = %%s\n' %q;; *) printf ' services = {\n0 0 com.example.test\n}\n';; esac;;
kickstart) printf 'new output\n' >> %q;;
esac
`, logPath, logPath)
	os.WriteFile(filepath.Join(bin, "launchctl"), []byte(fake), 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: plugins, Plugins: []plugin.Plugin{p}, BuiltIn: examples.Files})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:4678"+path, strings.NewReader(body))
		r.Header.Set("Origin", "http://127.0.0.1:4678")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w
	}
	w := request("POST", "/api/observations/jobs", `{"item":"com.example.test","action":"start"}`)
	var created struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &created)
	request("POST", "/api/action/jobs", fmt.Sprintf(`{"item":"com.example.test","action":"start","observation":%q}`, created.ID))
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("delayed\n")
	f.Close()
	w = request("POST", "/api/observations/session/"+created.ID+"/read", "{}")
	var snap observation.Snapshot
	if err = json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.State != "idle" || snap.ExitCode == nil || *snap.ExitCode != 23 || snap.Result != "returned" || !strings.Contains(snap.Log, "delayed") || !strings.Contains(snap.ExistingLog, "old") || strings.Contains(snap.Log, "old") {
		t.Fatalf("snapshot %+v", snap)
	}
	request("GET", "/api/operations/jobs", "")
}

func TestNativeExecutionConfigurationGrantsObservationEvenWithAnotherFolderName(t *testing.T) {
	for _, kind := range []string{"manual", "listeners"} {
		root := t.TempDir()
		path, err := catalog.Add(examples.Files, root, kind)
		if err != nil {
			t.Fatal(err)
		}
		renamed := filepath.Join(root, "renamed")
		if err = os.Rename(path, renamed); err != nil {
			t.Fatal(err)
		}
		p, err := plugin.ReadFolder(renamed)
		if err != nil {
			t.Fatal(err)
		}
		if got := observationKind(p); got != kind {
			t.Fatalf("kind %s, got %s", kind, got)
		}
		p.Detail.Exec.Command = []string{"custom-program"}
		if observationKind(p) != "" {
			t.Fatal("modified execution retained observation capability")
		}
	}
}

func TestObservationReadsThatRetainFragmentsRequireTheConsoleOrigin(t *testing.T) {
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: filepath.Join(t.TempDir(), "plugins")})
	for _, origin := range []string{"", "http://other"} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:4678/api/observations/session/unknown/read", strings.NewReader("{}"))
		req.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("origin %q: %d %s", origin, response.Code, response.Body.String())
		}
	}
}

func TestAnUnreadableJobConfigurationDoesNotClaimThereIsNoLogConfiguration(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd process boundary")
	}
	root := t.TempDir()
	plugins := filepath.Join(root, "plugins")
	os.Mkdir(plugins, 0700)
	installed, err := catalog.Add(examples.Files, plugins, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plugin.ReadFolder(installed)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	os.Mkdir(bin, 0700)
	script := `#!/bin/sh
case "$2" in */com.example.test) printf '\tstate = not running\n\tpath = /missing/job.plist\n';;*) printf 'services = {\n0 0 com.example.test\n}\n';;esac
`
	os.WriteFile(filepath.Join(bin, "launchctl"), []byte(script), 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := &server{cfg: Config{PluginDir: plugins}}
	s.use(Loaded{Plugins: []plugin.Plugin{p}})
	target, err := s.resolveObservation(context.Background(), p, "jobs", "com.example.test", observation.Identity{})
	if err != nil || target.Reason != "configuration_unreadable" {
		t.Fatalf("target %+v err %v", target, err)
	}
}

func TestANativeObservationRejectsAListingIdentityThatHasAlreadyChanged(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "plugins"), 0700)
	path, err := catalog.Add(examples.Files, filepath.Join(root, "plugins"), "listeners")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plugin.ReadFolder(path)
	if err != nil {
		t.Fatal(err)
	}
	// The native program's JSON output is the protocol boundary. No signal or
	// external service is used; the only kernel identity read is this test's.
	run.Register("listeners", func(context.Context, string, []string, run.Runner) ([]byte, error) {
		return []byte(fmt.Sprintf(`{"items":[{"id":"fixture","name":"Fixture","fields":[{"name":"PID","value":"%d"}]}]}`, os.Getpid())), nil
	})
	t.Cleanup(func() { run.Register("listeners", listenerview.Execute) })
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: filepath.Join(root, "plugins"), Plugins: []plugin.Plugin{p}})
	identity := observation.ProcessIdentity(os.Getpid())
	identity.Started = identity.Started.Add(-time.Second)
	payload, _ := json.Marshal(map[string]any{"item": "fixture", "expected": identity})
	req := httptest.NewRequest("POST", "http://127.0.0.1:4678/api/observations/listeners", bytes.NewReader(payload))
	req.Header.Set("Origin", "http://127.0.0.1:4678")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusConflict {
		t.Fatalf("changed listing: %d %s", response.Code, response.Body.String())
	}
}
