package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/console"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/tasks"
	"github.com/tidwall/gjson"
)

func TestStartupRefusesAddressesOtherThanLoopback(t *testing.T) {
	for _, addr := range []string{
		"0.0.0.0:4678",
		":4678", // an empty host listens on every interface
		"192.168.1.20:4678",
		"[::]:4678",
		"example.com:4678",
		"127.0.0.1",
	} {
		err := run([]string{"--addr", addr, "--dir", t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("--addr %s: err = %v, want a refusal naming 127.0.0.1", addr, err)
		}
	}
}

func TestTheFolderComesFromTheFlagThenTheEnvironment(t *testing.T) {
	t.Setenv("HOSTDECK_DIR", "env-dir")
	if got, _ := configDir("flag-dir"); got != "flag-dir" {
		t.Errorf("with --dir: got %q, want flag-dir", got)
	}
	if got, _ := configDir(""); got != "env-dir" {
		t.Errorf("without --dir: got %q, want env-dir", got)
	}
}

// service answers every request with its name.
func service(t *testing.T, name string) string {
	t.Helper()
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, name) }))
	t.Cleanup(svc.Close)
	return svc.URL + "/"
}

func proxied(id, target string) plugin.Plugin {
	return plugin.Plugin{ID: id, Page: &plugin.Page{Proxy: target}}
}

// answer is what url answers, or "" if nothing listens there.
func answer(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestEmbeddedServicesStartAndStopAsThePluginsChange(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	f := &forwarders{portsFile: filepath.Join(t.TempDir(), "ports.json"), consolePort: ln.Addr().(*net.TCPAddr).Port, tokens: onetime.New(time.Now)}
	defer f.close()
	a, b := service(t, "a"), service(t, "b")

	served, err := f.sync([]plugin.Plugin{proxied("svc", a)})
	if err != nil {
		t.Fatal(err)
	}
	url := served["svc"].URL
	if got := answer(t, url); got != "a" {
		t.Fatalf("%s answers %q, want a", url, got)
	}

	if served, err = f.sync(nil); err != nil || len(served) != 0 {
		t.Fatalf("without the plugin: %v, %v", served, err)
	}
	if got := answer(t, url); got != "" {
		t.Errorf("the stopped service's port still answers %q", got)
	}

	if served, err = f.sync([]plugin.Plugin{proxied("svc", a)}); err != nil || served["svc"].URL != url {
		t.Fatalf("back again: %v, %v; want it at %s", served, err, url)
	}
	if got := answer(t, url); got != "a" {
		t.Errorf("back again, it answers %q", got)
	}

	if served, err = f.sync([]plugin.Plugin{proxied("svc", b)}); err != nil || served["svc"].URL != url {
		t.Fatalf("pointed elsewhere: %v, %v; want it at %s", served, err, url)
	}
	if got := answer(t, url); got != "b" {
		t.Errorf("pointed elsewhere, it answers %q, want b", got)
	}
}

func TestAFailedSyncKeepsTheRunningForwardersAsTheyWere(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	portsFile := filepath.Join(t.TempDir(), "ports.json")
	f := &forwarders{portsFile: portsFile, consolePort: ln.Addr().(*net.TCPAddr).Port, tokens: onetime.New(time.Now)}
	defer f.close()
	a, b := service(t, "a"), service(t, "b")

	served, err := f.sync([]plugin.Plugin{proxied("svc", a)})
	if err != nil {
		t.Fatal(err)
	}
	url := served["svc"].URL
	if got := answer(t, url); got != "a" {
		t.Fatalf("%s answers %q, want a", url, got)
	}

	// Make ports.json unreadable/unwritable by replacing it with a folder.
	if err := os.Remove(portsFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(portsFile, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := f.sync([]plugin.Plugin{proxied("svc", b)}); err == nil {
		t.Fatal("retargeting with broken ports.json succeeded, want an error")
	}
	if got := answer(t, url); got != "a" {
		t.Fatalf("after failed retarget, %s answers %q, want a", url, got)
	}

	if _, err := f.sync(nil); err == nil {
		t.Fatal("removing with broken ports.json succeeded, want an error")
	}
	if got := answer(t, url); got != "a" {
		t.Fatalf("after failed removal, %s answers %q, want a", url, got)
	}
}

func TestAFailedReloadKeepsForwardersPortsAndTasksMatchingTheConsole(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	portsFile := filepath.Join(dir, "ports.json")
	t.Cleanup(func() { _ = tasks.Close(t.Context(), pluginDir) })

	a, b, c := service(t, "a"), service(t, "b"), service(t, "c")
	svcDir := filepath.Join(pluginDir, "svc")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "plugin.yaml"), []byte("schema: 1\nname: Service\npage:\n  proxy: "+a+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tasksDir := filepath.Join(pluginDir, "scheduled-tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tasksManifest := "schema: 1\nname: Scheduled tasks\ndetail:\n  exec:\n    command: [\"@hostdeck\", \"tasks\", \"detail\"]\n"
	if err := os.WriteFile(filepath.Join(tasksDir, "plugin.yaml"), []byte(tasksManifest), 0o600); err != nil {
		t.Fatal(err)
	}

	consolePort := ln.Addr().(*net.TCPAddr).Port
	origin := "http://127.0.0.1:" + strings.Split(ln.Addr().String(), ":")[1]
	tokens := onetime.New(time.Now)
	f := &forwarders{portsFile: portsFile, consolePort: consolePort, tokens: tokens}
	defer f.close()

	loaded, err := f.reload(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	url := loaded.Served["svc"].URL
	if got := answer(t, url); got != "a" {
		t.Fatalf("%s answers %q, want a", url, got)
	}
	initialPorts, err := os.ReadFile(portsFile)
	if err != nil {
		t.Fatal(err)
	}

	h := console.Handler(console.Config{
		Origin:    origin,
		Plugins:   loaded.Plugins,
		Broken:    loaded.Broken,
		PluginDir: pluginDir,
		Served:    loaded.Served,
		Tokens:    tokens,
		Reload:    func() (console.Loaded, error) { return f.reload(pluginDir) },
	})

	// 1. A reload that fails in f.sync after retargeting svc on disk keeps
	// both the console's sidebar URL and the running proxy on "a".
	if err := os.WriteFile(filepath.Join(svcDir, "plugin.yaml"), []byte("schema: 1\nname: Service\npage:\n  proxy: "+b+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(portsFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(portsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, origin+"/api/plugins/reload", nil)
	req.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError || gjson.GetBytes(w.Body.Bytes(), "error.code").String() != "reload_failed" {
		t.Fatalf("reload status = %d %s, want 500 reload_failed", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, origin+"/api/plugins", nil))
	if got := gjson.GetBytes(w.Body.Bytes(), `#(id=="svc").url`).String(); got != url {
		t.Fatalf("sidebar svc url = %q, want %q", got, url)
	}
	if got := answer(t, url); got != "a" {
		t.Fatalf("after failed reload, %s answers %q, want a", url, got)
	}

	// Restore ports.json and verify that a failure in tasks.Sync rolls back
	// f.sync and ports.json to the previously loaded plugins.
	if err := os.Remove(portsFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portsFile, initialPorts, 0o600); err != nil {
		t.Fatal(err)
	}
	brokenTasks := plugin.Plugin{
		ID:     "missing-tasks",
		Dir:    filepath.Join(pluginDir, "missing-tasks"),
		Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "tasks", "detail"}}},
	}
	if _, err := f.apply(pluginDir, []plugin.Plugin{proxied("svc", b), proxied("svc2", c), brokenTasks}); err == nil {
		t.Fatal("apply with unreadable tasks folder succeeded, want an error")
	}
	if got := answer(t, url); got != "a" {
		t.Fatalf("after failed tasks.Sync, %s answers %q, want a", url, got)
	}
	if _, ok := f.running["svc2"]; ok {
		t.Fatalf("after failed tasks.Sync, svc2 is still running: %+v", f.running["svc2"])
	}
	afterPorts, err := os.ReadFile(portsFile)
	if err != nil || string(afterPorts) != string(initialPorts) {
		t.Fatalf("ports.json = %q (%v), want %q", afterPorts, err, initialPorts)
	}
}

func TestClosingForwardersReleasesTheirListeningPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	f := &forwarders{portsFile: filepath.Join(t.TempDir(), "ports.json"), consolePort: ln.Addr().(*net.TCPAddr).Port, tokens: onetime.New(time.Now)}
	target := service(t, "proxied")
	served, err := f.sync([]plugin.Plugin{proxied("svc", target)})
	if err != nil {
		t.Fatal(err)
	}
	url := served["svc"].URL
	if got := answer(t, url); got != "proxied" {
		t.Fatalf("%s answers %q, want proxied", url, got)
	}
	f.close()
	if got := answer(t, url); got != "" {
		t.Fatalf("after close, %s still answers %q", url, got)
	}
}

var (
	consoleListenLog   = regexp.MustCompile(`hostdeck: (http://127\.0\.0\.1:\d+)`)
	forwarderListenLog = regexp.MustCompile(`svc: (http://127\.0\.0\.1:\d+)/ -> `)
)

func TestGracefulExitClosesForwarderPorts(t *testing.T) {
	if os.Getenv("HOSTDECK_TEST_GRACEFUL_CHILD") == "1" {
		log.SetFlags(0)
		dir := os.Getenv("HOSTDECK_TEST_GRACEFUL_DIR")
		if err := run([]string{"--addr", "127.0.0.1:0", "--dir", dir}); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}

	dir := t.TempDir()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "backend")
	}))
	t.Cleanup(backend.Close)
	pluginDir := filepath.Join(dir, "plugins", "svc")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema: 1\nname: Service\npage:\n  proxy: " + backend.URL + "/\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestGracefulExitClosesForwarderPorts$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"HOSTDECK_TEST_GRACEFUL_CHILD=1",
		"HOSTDECK_TEST_GRACEFUL_DIR="+dir,
	)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
		}
	})

	var consoleURL, forwardURL string
	sc := bufio.NewScanner(stderr)
	deadline := time.Now().Add(10 * time.Second)
	for consoleURL == "" || forwardURL == "" {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for console and forwarder listen logs")
		}
		if !sc.Scan() {
			t.Fatalf("child exited before logging listen addresses: %v", sc.Err())
		}
		line := sc.Text()
		if m := consoleListenLog.FindStringSubmatch(line); m != nil {
			consoleURL = m[1]
		}
		if m := forwarderListenLog.FindStringSubmatch(line); m != nil {
			forwardURL = m[1] + "/"
		}
	}
	if got := answer(t, forwardURL); got != "backend" {
		t.Fatalf("before signal, forwarder answers %q, want backend", got)
	}

	// Hold an incomplete console connection so Shutdown waits on it. Only an
	// explicit forwarders close in the stopping path frees the proxy port
	// before that wait ends; defer alone would leave it listening.
	host := strings.TrimPrefix(consoleURL, "http://")
	held, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	released := false
	stopWait := time.Now().Add(2 * time.Second)
	for time.Now().Before(stopWait) {
		if answer(t, forwardURL) == "" {
			released = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !released {
		t.Fatal("forwarder port still listening after SIGTERM while console Shutdown is held open")
	}

	_ = held.Close()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("child exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not exit after SIGTERM")
	}
}
