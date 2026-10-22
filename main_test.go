package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
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
