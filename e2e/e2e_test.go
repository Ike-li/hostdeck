// Package e2e runs the hostdeck binary and drives a real browser against it.
//
// Without a browser these tests skip. HOSTDECK_E2E=1, which CI sets, turns a
// missing browser into a failure; HOSTDECK_BROWSER picks the browser's path.
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hostdeck-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "hostdeck")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "github.com/Ike-li/hostdeck")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	code := 1
	if err := build.Run(); err == nil {
		code = m.Run()
	}
	os.RemoveAll(dir)
	os.Exit(code)
}

var consoleURL = regexp.MustCompile(`^hostdeck: (http://127\.0\.0\.1:\d+)`)

// startConsole runs hostdeck on a free port with dir as its folder and returns
// the console's URL. What hostdeck logged is shown if the test fails.
func startConsole(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command(binary, "--addr", "127.0.0.1:0", "--dir", dir)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var logged strings.Builder
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			mu.Lock()
			t.Logf("hostdeck logged:\n%s", logged.String())
			mu.Unlock()
		}
	})
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			mu.Lock()
			logged.WriteString(sc.Text() + "\n")
			mu.Unlock()
			if m := consoleURL.FindStringSubmatch(sc.Text()); m != nil {
				select {
				case found <- m[1]:
				default:
				}
			}
		}
		close(found)
	}()
	select {
	case url, ok := <-found:
		if !ok {
			t.Fatal("hostdeck exited without saying where it listens")
		}
		return url
	case <-time.After(10 * time.Second):
		t.Fatal("hostdeck did not say where it listens within 10s")
	}
	return ""
}

// writePlugin puts a plugin.yaml under <dir>/plugins/<id>/.
func writePlugin(t *testing.T, dir, id, manifest string) {
	t.Helper()
	pluginDir := filepath.Join(dir, "plugins", id)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// openBrowser starts a headless browser whose preferred languages are langs
// and returns a context that bounds the test's steps.
func openBrowser(t *testing.T, langs string) context.Context {
	t.Helper()
	opts := slices.Clone(chromedp.DefaultExecAllocatorOptions[:])
	if path := os.Getenv("HOSTDECK_BROWSER"); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	t.Cleanup(func() {
		cancelCtx()
		cancelAlloc()
	})
	if err := chromedp.Do(ctx); err != nil {
		if os.Getenv("HOSTDECK_E2E") == "1" {
			t.Fatalf("starting the browser: %v", err)
		}
		t.Skipf("no browser to drive (%v); HOSTDECK_E2E=1 makes this a failure", err)
	}
	version, err := chromedp.CallBrowser(ctx, browser.GetVersion, cdp.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = chromedp.Call(ctx, emulation.SetUserAgentOverride, emulation.SetUserAgentOverrideParams{
		UserAgent:      version.UserAgent,
		AcceptLanguage: langs,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelSteps := context.WithTimeout(ctx, time.Minute)
	t.Cleanup(cancelSteps)
	return ctx
}

func do(t *testing.T, ctx context.Context, steps ...chromedp.Action[chromedp.Void]) {
	t.Helper()
	if err := chromedp.Do(ctx, steps...); err != nil {
		t.Fatal(err)
	}
}

func text(t *testing.T, ctx context.Context, css string) string {
	t.Helper()
	s, err := chromedp.Run(ctx, chromedp.Text(chromedp.CSS(css)))
	if err != nil {
		t.Fatalf("text of %s: %v", css, err)
	}
	return s
}

// databaseService answers like a health endpoint that reports its database.
func databaseService(t *testing.T) string {
	t.Helper()
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"database": "ok"}`)
	}))
	t.Cleanup(svc.Close)
	return svc.URL
}

const demoPlugin = `schema: 1
name: Demo
icon: D
card:
  http:
    url: %s
  value: database
  label: {en: Database, zh-CN: 数据库}
`

func TestTheOverviewSpeaksTheBrowsersLanguageAndSwitches(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "demo", fmt.Sprintf(demoPlugin, databaseService(t)))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(".card-value")))
	title, value, label := text(t, ctx, "h1"), text(t, ctx, ".card-value"), text(t, ctx, ".card-label")
	if title != "总览" || value != "ok" || label != "数据库" {
		t.Errorf("in Chinese: title %q, card %q %q; want 总览, ok 数据库", title, value, label)
	}

	do(t, ctx,
		chromedp.Click(chromedp.CSS(`button[data-lang="en"]`)),
		chromedp.WaitVisible(chromedp.Search(`//h1[text()="Overview"]`)),
	)
	if label := text(t, ctx, ".card-label"); label != "Database" {
		t.Errorf("after switching to English: label %q, want Database", label)
	}

	do(t, ctx, chromedp.Reload(), chromedp.WaitVisible(chromedp.CSS(".card-value")))
	if title := text(t, ctx, "h1"); title != "Overview" {
		t.Errorf("after reloading: title %q, want the chosen language kept", title)
	}
}

func TestABrowserInEnglishGetsEnglish(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "demo", fmt.Sprintf(demoPlugin, databaseService(t)))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en-US,en;q=0.9")

	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(".card-value")))
	if title, label := text(t, ctx, "h1"), text(t, ctx, ".card-label"); title != "Overview" || label != "Database" {
		t.Errorf("title %q, label %q; want Overview, Database", title, label)
	}
}

const servicePage = `<!doctype html><title>Service</title><h1 id="svc">Hello from the service</h1>`

// framingRefusingService answers like a web UI that forbids every page to frame it.
func framingRefusingService(t *testing.T) string {
	t.Helper()
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, servicePage)
	}))
	t.Cleanup(svc.Close)
	return svc.URL
}

// page serves html on a port of its own.
func page(t *testing.T, html string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, html)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const proxyPlugin = `schema: 1
name: Service
icon: S
page:
  proxy: %s/
`

// tryFrameText waits up to wait for the element css inside the iframe frameCSS
// and returns its text. A frame that is still navigating is tried again.
func tryFrameText(ctx context.Context, frameCSS, css string, wait time.Duration) (string, bool) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		nodes, err := chromedp.Run(ctx, chromedp.Nodes(chromedp.CSS(frameCSS)))
		if err != nil || len(nodes) == 0 {
			continue
		}
		text, err := chromedp.Run(ctx, chromedp.Poll[string](
			fmt.Sprintf("document.querySelector(%q)?.textContent", css),
			chromedp.WithPollingInFrame(nodes[0]),
			chromedp.WithPollingTimeout(time.Second),
		))
		if err == nil {
			return text, true
		}
	}
	return "", false
}

func attrs(t *testing.T, ctx context.Context, css string) map[string]string {
	t.Helper()
	a, err := chromedp.Run(ctx, chromedp.Attributes(chromedp.CSS(css)))
	if err != nil {
		t.Fatalf("attributes of %s: %v", css, err)
	}
	return a
}

// sidebarURL asks the console where the sidebar entry id opens.
func sidebarURL(t *testing.T, console, id string) string {
	t.Helper()
	resp, err := http.Get(console + "/api/plugins")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var entries []struct{ ID, URL string }
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ID == id {
			return e.URL
		}
	}
	t.Fatalf("no sidebar entry %s", id)
	return ""
}

func TestAServiceThatForbidsFramingOpensInsideTheConsole(t *testing.T) {
	dir := t.TempDir()
	svc := framingRefusingService(t)
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, svc))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en-US,en;q=0.9")

	do(t, ctx,
		chromedp.Navigate(url),
		chromedp.Click(chromedp.CSS(`.nav-item[data-id="svc"]`)),
		chromedp.WaitVisible(chromedp.CSS("#frames iframe")),
	)
	if got, ok := tryFrameText(ctx, "#frames iframe", "#svc", 15*time.Second); !ok || got != "Hello from the service" {
		t.Errorf("inside the console: %q, %v; want the service's page", got, ok)
	}
	src, open := attrs(t, ctx, "#frames iframe")["src"], attrs(t, ctx, "#service-open")["href"]
	if src != open || !strings.HasPrefix(src, "http://127.0.0.1:") || src == svc+"/" {
		t.Errorf("frame %q, new tab %q; want both on the service's own forwarding port, not %s", src, open, svc)
	}
}

func TestOnlyTheConsoleMayFrameAnEmbeddedService(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, framingRefusingService(t)))
	served := sidebarURL(t, startConsole(t, dir), "svc")
	// Another page on 127.0.0.1 frames the service and, to show that its
	// frames can be read at all, a page that allows framing.
	other := page(t, fmt.Sprintf(`<iframe id="served" src="%s"></iframe><iframe id="plain" src="%s"></iframe>`, served, page(t, servicePage)))
	ctx := openBrowser(t, "en-US,en;q=0.9")

	do(t, ctx, chromedp.Navigate(other))
	if _, ok := tryFrameText(ctx, "#plain", "#svc", 15*time.Second); !ok {
		t.Fatal("could not read a frame that allows framing, so the check below means nothing")
	}
	if got, ok := tryFrameText(ctx, "#served", "#svc", 3*time.Second); ok {
		t.Errorf("another page on 127.0.0.1 framed the service and saw %q", got)
	}
}

func TestAMalformedAddressShowsTheOverview(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, framingRefusingService(t)))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en-US,en;q=0.9")

	for _, hash := range []string{"#/%E0%A4%A", "#/no-such-plugin"} {
		do(t, ctx,
			chromedp.Navigate("about:blank"),
			chromedp.Navigate(url+"/"+hash),
			chromedp.WaitReady(chromedp.CSS(`.nav-item[data-id="svc"]`)),
		)
		if title := text(t, ctx, "#overview h1"); title != "Overview" {
			t.Errorf("%s: overview title %q, want Overview", hash, title)
		}
		if _, hidden := attrs(t, ctx, "#service")["hidden"]; !hidden {
			t.Errorf("%s: the service view is showing", hash)
		}
	}
}
