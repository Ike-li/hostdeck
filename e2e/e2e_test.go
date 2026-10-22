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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

var binary string

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "fake-plugin" {
		fakePlugin(os.Args[2:])
		return
	}
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

// storageService answers like a web UI that keeps data in the browser. The
// first visit to its start page in a frame fills every kind of storage and
// moves deeper in, as a user would; later visits, and the console's status
// probes, go straight to the state page, which shows what the browser holds
// for the service. name keeps its cookie apart from other services', since
// cookies are not kept per port.
func storageService(t *testing.T, name string) string {
	t.Helper()
	var visited atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/start.html", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Fetch-Dest") != "iframe" || visited.Swap(true) {
			http.Redirect(w, r, "/state.html", http.StatusFound)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: name + "_session", Value: "kept", Path: "/", SameSite: http.SameSiteLaxMode})
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, fillPage)
	})
	mux.HandleFunc("/state.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, statePage, name)
	})
	mux.HandleFunc("/cached.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Cache-Control", "max-age=3600")
		io.WriteString(w, "// cached")
	})
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		io.WriteString(w, "self.addEventListener('fetch', () => {});")
	})
	svc := httptest.NewServer(mux)
	t.Cleanup(svc.Close)
	return svc.URL
}

const fillPage = `<!doctype html><meta charset="utf-8"><p>filling</p>
<script>
(async () => {
  localStorage.setItem('k', 'v');
  await new Promise((res, rej) => {
    const r = indexedDB.open('db', 1);
    r.onupgradeneeded = () => r.result.createObjectStore('s');
    r.onsuccess = () => { r.result.close(); res(); };
    r.onerror = rej;
  });
  await (await caches.open('c')).put('/x', new Response('x'));
  await fetch('/cached.js');
  await navigator.serviceWorker.register('/sw.js');
  location.assign('/state.html?deeper=1');
})();
</script>`

const statePage = `<!doctype html><meta charset="utf-8"><pre id="state"></pre>
<script>
addEventListener('pagehide', () => localStorage.setItem('left', 'written'));
(async () => {
  const state = {
    page: location.search,
    nonce: Math.random(),
    localStorage: localStorage.getItem('k'),
    writtenWhileLeaving: localStorage.getItem('left'),
    indexedDB: (await indexedDB.databases()).some(d => d.name === 'db'),
    cacheStorage: (await caches.keys()).length,
    serviceWorkers: (await navigator.serviceWorker.getRegistrations()).length,
    httpCache: await fetch('/cached.js', {cache: 'only-if-cached', mode: 'same-origin'}).then(r => r.ok, () => false),
    cookie: document.cookie.includes('%s_session=kept'),
  };
  document.getElementById('state').textContent = JSON.stringify(state);
  parent.postMessage({state}, '*');
})();
</script>`

const storagePlugin = `schema: 1
name: %s
page:
  proxy: %s/start.html
`

type siteState struct {
	Page                string  `json:"page"`
	Nonce               float64 `json:"nonce"`
	LocalStorage        *string `json:"localStorage"`
	WrittenWhileLeaving *string `json:"writtenWhileLeaving"`
	IndexedDB           bool    `json:"indexedDB"`
	CacheStorage        int     `json:"cacheStorage"`
	ServiceWorkers      int     `json:"serviceWorkers"`
	HTTPCache           bool    `json:"httpCache"`
	Cookie              bool    `json:"cookie"`
}

func (s siteState) full() bool {
	return s.LocalStorage != nil && s.IndexedDB && s.CacheStorage == 1 && s.ServiceWorkers == 1 && s.HTTPCache && s.Cookie
}

// collectStates has the console page keep every state that a service frame
// reports, by the service's origin.
func collectStates(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := chromedp.Run(ctx, chromedp.Evaluate[any](`
		window.reported = [];
		addEventListener('message', (e) => { if (e.data && e.data.state) reported.push({origin: e.origin, state: e.data.state}); });
	`))
	if err != nil {
		t.Fatal(err)
	}
}

// serviceState waits until the service at origin reports a state that ok
// accepts, and returns it.
func serviceState(t *testing.T, ctx context.Context, origin string, ok func(siteState) bool) siteState {
	t.Helper()
	var last siteState
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		states, err := chromedp.Run(ctx, chromedp.Evaluate[[]siteState](fmt.Sprintf(`reported.filter(r => r.origin === %q).map(r => r.state)`, origin)))
		if err == nil && len(states) > 0 {
			if last = states[len(states)-1]; ok(last) {
				return last
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s never reported the state the test waits for; last %+v", origin, last)
	return last
}

func originOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Scheme + "://" + u.Host
}

// doubleClick clicks twice the way a person's double-click does: a click,
// then a second one that the browser counts as the second of two.
func doubleClick(t *testing.T, ctx context.Context, css string) {
	t.Helper()
	nodes, err := chromedp.Run(ctx, chromedp.Nodes(chromedp.CSS(css)))
	if err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.MouseClickNode(nodes[0]), chromedp.MouseClickNode(nodes[0], chromedp.ClickCount(2)))
}

func TestClearingCacheEmptiesOneServiceAndKeepsCookies(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "b", fmt.Sprintf(storagePlugin, "B", storageService(t, "b")))
	writePlugin(t, dir, "c", fmt.Sprintf(storagePlugin, "C", storageService(t, "c")))
	console := startConsole(t, dir)
	b, c := originOf(t, sidebarURL(t, console, "b")), originOf(t, sidebarURL(t, console, "c"))
	ctx := openBrowser(t, "en-US,en;q=0.9")

	// Picking a language stores it in the console's own storage.
	do(t, ctx, chromedp.Navigate(console), chromedp.Click(chromedp.CSS(`button[data-lang="en"]`)))
	collectStates(t, ctx)
	do(t, ctx, chromedp.Click(chromedp.CSS(`.nav-item[data-id="c"]`)))
	serviceState(t, ctx, c, siteState.full)
	do(t, ctx,
		chromedp.Click(chromedp.CSS(`.nav-item[data-id="b"]`)),
		chromedp.WaitVisible(chromedp.Search(`//span[@id="service-name" and text()="B"]`)),
	)
	before := serviceState(t, ctx, b, func(s siteState) bool { return s.full() && s.Page == "?deeper=1" })

	// A double-click is one gesture: its first click arms the button and its
	// second does not confirm.
	doubleClick(t, ctx, "#service-clear")
	time.Sleep(1500 * time.Millisecond)
	if s := serviceState(t, ctx, b, func(siteState) bool { return true }); s.Nonce != before.Nonce {
		t.Fatalf("a double-click cleared the service: before %+v, now %+v", before, s)
	}
	if label := text(t, ctx, "#service-clear"); label != "Click again to clear" {
		t.Errorf("after the first click the button says %q, want it waiting for a second click", label)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS("#service-clear")))
	after := serviceState(t, ctx, b, func(s siteState) bool { return s.Nonce != before.Nonce })
	want := siteState{Page: "?deeper=1", Nonce: after.Nonce, Cookie: true}
	if after != want {
		t.Errorf("after clearing, the service reports %+v; want it back on the page the user was on, with nothing kept but its cookie", after)
	}

	lang, err := chromedp.Run(ctx, chromedp.Evaluate[string](`localStorage.getItem('hostdeck.lang')`))
	if err != nil || lang != "en" {
		t.Errorf("the console's own storage holds %q (%v), want the language kept", lang, err)
	}
	// The console routes on hashchange, which comes after the click returns.
	do(t, ctx,
		chromedp.Click(chromedp.CSS(`.nav-item[data-id="c"]`)),
		chromedp.WaitVisible(chromedp.Search(`//span[@id="service-name" and text()="C"]`)),
		chromedp.Click(chromedp.CSS("#service-reload")),
	)
	if s := serviceState(t, ctx, c, func(s siteState) bool { return s.Page == "" }); !s.full() {
		t.Errorf("another service lost its data: %+v", s)
	}
}

func TestTheDiskExampleShowsItsStatusAndRows(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, "plugins", "disk"), os.DirFS(filepath.Join("..", "examples", "disk"))); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	const card = `.card[data-id="disk"]`
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(card+" .card-value")))
	if value, label := text(t, ctx, card+" .card-value"), text(t, ctx, card+" .card-label"); !strings.HasSuffix(value, " GB") || !strings.HasPrefix(label, "可用，共") {
		t.Errorf("card %q %q, want the free space in GB and a Chinese label", value, label)
	}
	if pill := text(t, ctx, card+" .pill"); !slices.Contains([]string{"正常", "注意", "异常"}, pill) {
		t.Errorf("status says %q, want it in Chinese", pill)
	}
	if row := text(t, ctx, card+" .row-value"); !strings.HasSuffix(row, "%") {
		t.Errorf("first row says %q, want how much of the home folder's disk is used", row)
	}
}

// fakePlugin is the test binary playing a plugin's commands, for hostdeck to
// run: "detail" prints one item with a restart action, and "action" records
// its arguments in $HOSTDECK_E2E_RAN and prints a message.
func fakePlugin(args []string) {
	switch args[0] {
	case "detail":
		fmt.Print(`{"items": [{"id": "svc", "name": {"en": "Service", "zh-CN": "服务"}, "status": "ok",
			"fields": [{"name": "PID", "value": 4242}], "log": "started",
			"actions": [{"id": "restart", "label": {"en": "Restart", "zh-CN": "重启"}}]}]}`)
	case "action":
		os.WriteFile(os.Getenv("HOSTDECK_E2E_RAN"), []byte(strings.Join(args[1:], " ")), 0o644)
		fmt.Print(`{"message": {"en": "Restarted", "zh-CN": "已重启"}}`)
	}
}

const fakeDetailPlugin = `schema: 1
name: {en: Services, zh-CN: 服务列表}
detail:
  exec: {command: ['%[1]s', fake-plugin, detail]}
actions:
  exec: {command: ['%[1]s', fake-plugin, action]}
`

func TestAnActionRunsOnTheSecondClickAndNotOnADoubleClick(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	t.Setenv("HOSTDECK_E2E_RAN", ran)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writePlugin(t, dir, "svc", fmt.Sprintf(fakeDetailPlugin, exe))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	const button = ".detail-item button.action"
	do(t, ctx,
		chromedp.Navigate(url),
		chromedp.Click(chromedp.CSS(`.nav-item[data-id="svc"]`)),
		chromedp.WaitVisible(chromedp.CSS(button)),
	)
	if name, pid := text(t, ctx, ".detail-item .detail-name"), text(t, ctx, ".detail-item .detail-fields dd"); name != "服务" || pid != "4242" {
		t.Errorf("item %q with PID %q, want 服务 and 4242", name, pid)
	}

	doubleClick(t, ctx, button)
	time.Sleep(time.Second)
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a double-click ran the action")
	}
	if label := text(t, ctx, button); label != "确认重启？" {
		t.Errorf("after the first click the button says %q, want it asking to confirm", label)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(button)), chromedp.WaitVisible(chromedp.CSS(".detail-note")))
	if note := text(t, ctx, ".detail-note"); note != "已重启" {
		t.Errorf("note %q, want the plugin's message", note)
	}
	if got, _ := os.ReadFile(ran); string(got) != "restart svc" {
		t.Errorf("the action command got %q, want restart svc", got)
	}
}

// The overview waits 5 s for its cards; this one takes longer.
func TestASlowCardSaysItIsLoadingAndThenShowsItsValue(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(6 * time.Second)
		io.WriteString(w, `{"v": "late"}`)
	}))
	t.Cleanup(svc.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "slow", fmt.Sprintf("schema: 1\nname: Slow\ncard:\n  http: {url: '%s'}\n  value: v\n", svc.URL))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	const card = `.card[data-id="slow"]`
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(card+" .card-pending")))
	if got := text(t, ctx, card+" .card-pending"); got != "读取中…" {
		t.Errorf("while loading: %q", got)
	}
	do(t, ctx, chromedp.WaitVisible(chromedp.CSS(card+" .card-value")))
	if got := text(t, ctx, card+" .card-value"); got != "late" {
		t.Errorf("value %q, want late", got)
	}
}

// A row without thresholds of its own has not been judged, so it does not
// take the colour of its card.
func TestARowWithoutThresholdsDoesNotTakeItsCardsColour(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"waiting": 72, "workers": 3}`)
	}))
	t.Cleanup(svc.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "queue", fmt.Sprintf(`schema: 1
name: Queue
card:
  http: {url: '%s'}
  value: waiting
  warn: {above: 50}
  rows:
    - {name: Judged, value: waiting, warn: {above: 50}}
    - {name: Plain, value: workers}
`, svc.URL))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="queue"] .row`)))
	dots, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](
		`[...document.querySelectorAll('.card[data-id="queue"] .row-dot')].map(d => getComputedStyle(d).backgroundColor)`))
	if err != nil {
		t.Fatal(err)
	}
	if len(dots) != 2 || dots[0] == dots[1] {
		t.Errorf("row dots %q: the row without thresholds has the colour of the yellow one", dots)
	}
}

// A clearing the service's port refuses, here because the token was tampered
// with, says that it failed instead of looking done.
func TestARefusedClearingSaysItFailed(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "b", fmt.Sprintf(storagePlugin, "B", storageService(t, "b")))
	console := startConsole(t, dir)
	ctx := openBrowser(t, "en-US,en;q=0.9")

	do(t, ctx,
		chromedp.Navigate(console+"#/b"),
		chromedp.WaitVisible(chromedp.Search(`//span[@id="service-name" and text()="B"]`)),
	)
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`
		const realFetch = window.fetch;
		window.fetch = async (url, opts) => {
			const resp = await realFetch(url, opts);
			if (!String(url).startsWith('/api/clear/')) return resp;
			const { url: clearURL } = await resp.json();
			return new Response(JSON.stringify({ url: clearURL.replace(/token=[^&]*/, 'token=forged') }));
		};
	`)); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS("#service-clear")))
	time.Sleep(200 * time.Millisecond)
	do(t, ctx, chromedp.Click(chromedp.CSS("#service-clear")))
	note, err := chromedp.Run(ctx, chromedp.Poll[string](
		`(() => { const n = document.getElementById('clear-note'); return !n.hidden && n.textContent.startsWith('Could not') && n.textContent; })()`,
		chromedp.WithPollingTimeout(10*time.Second),
	))
	if err != nil {
		t.Fatalf("no failure shown after a refused clearing: %v; note %q", err, text(t, ctx, "#clear-note"))
	}
	t.Logf("note: %s", note)
}

// A first run has no plugins: the overview points to the plugins page, which
// adds a built-in plugin on a second click and puts it to use.
func TestThePluginsPageAddsABuiltInPluginOnTheSecondClick(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS("#empty")))
	if got := text(t, ctx, "#empty"); !strings.Contains(got, "「插件」") {
		t.Errorf("empty overview %q does not point to the plugins page", got)
	}

	const disk = `.catalog-item[data-id="disk"]`
	do(t, ctx, chromedp.Click(chromedp.CSS(`.nav-item[href="#/.plugins"]`)), chromedp.WaitVisible(chromedp.CSS(disk+" .add")))
	if got := text(t, ctx, disk+" .catalog-description"); !strings.Contains(got, "不用改就能用") {
		t.Errorf("disk is described as %q", got)
	}

	added := filepath.Join(dir, "plugins", "disk", "plugin.yaml")
	doubleClick(t, ctx, disk+" .add")
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(added); err == nil {
		t.Fatal("a double-click added the plugin")
	}

	// The double-click's first click armed the button; wait until it gives up.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector(%q).textContent === '添加'`, disk+" .add"))); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(disk+" .add")))
	if got := text(t, ctx, disk+" .add"); got != "再点一次添加" {
		t.Errorf("after one click the button says %q", got)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(disk+" .add")), chromedp.WaitVisible(chromedp.CSS(disk+" .catalog-state")))
	if _, err := os.Stat(added); err != nil {
		t.Fatalf("not added: %v", err)
	}
	if got := text(t, ctx, "#catalog-note"); !strings.Contains(got, filepath.Join(dir, "plugins", "disk")) || strings.Contains(got, "重启") {
		t.Errorf("note %q, want where it was added and no restart", got)
	}
	if got := text(t, ctx, disk+" .catalog-state"); got != "使用中" {
		t.Errorf("disk now shows %q, want it in use", got)
	}

	// In use at once: in the sidebar, and on the overview with its card.
	do(t, ctx, chromedp.WaitVisible(chromedp.CSS(`.nav-item[data-id="disk"]`)))
	do(t, ctx, chromedp.Click(chromedp.CSS(`.nav-item[href="#/"]`)), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="disk"]`)))
}

// A plugin is disabled on the plugins page by a second click, which renames
// its folder and stops it, and enabled again the same way.
func TestThePluginsPageDisablesAndEnablesAPlugin(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "site", "schema: 1\nname: Site\npage: {link: 'http://127.0.0.1:9/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	const site = `#catalog-installed .catalog-item[data-id="site"]`
	button := site + " button"
	waitLabel := func(want string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector(%q)?.textContent === %q`, button, want))); err != nil {
			t.Fatalf("waiting for the button to say %q: %v; it says %q", want, err, text(t, ctx, button))
		}
	}
	enabled, disabled := filepath.Join(dir, "plugins", "site"), filepath.Join(dir, "plugins", ".site")

	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(button)))
	if got := text(t, ctx, site+" .catalog-state"); got != "使用中" {
		t.Errorf("site shows %q, want it in use", got)
	}
	doubleClick(t, ctx, button)
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(enabled); err != nil {
		t.Fatal("a double-click disabled the plugin")
	}
	waitLabel("停用")

	do(t, ctx, chromedp.Click(chromedp.CSS(button)))
	waitLabel("再点一次停用")
	do(t, ctx, chromedp.Click(chromedp.CSS(button)))
	waitLabel("启用")
	if _, err := os.Stat(filepath.Join(disabled, "plugin.yaml")); err != nil {
		t.Fatalf("not disabled: %v", err)
	}
	if note, state := text(t, ctx, "#catalog-note"), text(t, ctx, site+" .catalog-state"); strings.Contains(note+state, "重启") || !strings.Contains(state, ".site") {
		t.Errorf("note %q, state %q; want the folder's new name and no restart", note, state)
	}
	gone(t, ctx, `.nav-item[data-id="site"]`)

	do(t, ctx, chromedp.Click(chromedp.CSS(button)))
	waitLabel("再点一次启用")
	do(t, ctx, chromedp.Click(chromedp.CSS(button)))
	waitLabel("停用")
	if _, err := os.Stat(filepath.Join(enabled, "plugin.yaml")); err != nil {
		t.Fatalf("not enabled again: %v", err)
	}
	do(t, ctx, chromedp.WaitVisible(chromedp.CSS(`.nav-item[data-id="site"]`)))
}

// gone waits until nothing matches selector.
func gone(t *testing.T, ctx context.Context, selector string) {
	t.Helper()
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`!document.querySelector(%q)`, selector))); err != nil {
		t.Fatalf("%s is still there: %v", selector, err)
	}
}

// threeLinks writes plugins a, b and c, each a sidebar link with a card.
func threeLinks(t *testing.T, dir string) {
	t.Helper()
	svc := databaseService(t)
	for _, id := range []string{"a", "b", "c"} {
		writePlugin(t, dir, id, fmt.Sprintf("schema: 1\nname: %s\npage: {link: 'https://%s.example/'}\ncard:\n  http: {url: '%s'}\n  value: database\n", strings.ToUpper(id), id, svc))
	}
}

// drag drops the element at from onto the one at to, as the browser reports
// a drag made with the mouse.
func drag(t *testing.T, ctx context.Context, from, to string) {
	t.Helper()
	_, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf(`(() => {
		const from = document.querySelector(%q), to = document.querySelector(%q), dt = new DataTransfer();
		const fire = (el, type) => el.dispatchEvent(new DragEvent(type, {bubbles: true, cancelable: true, dataTransfer: dt}));
		fire(from, 'dragstart'); fire(to, 'dragenter'); fire(to, 'dragover'); fire(to, 'drop'); fire(from, 'dragend');
	})()`, from, to)))
	if err != nil {
		t.Fatal(err)
	}
}

func idsOf(t *testing.T, ctx context.Context, css string) []string {
	t.Helper()
	got, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](fmt.Sprintf(`[...document.querySelectorAll(%q)].map(e => e.dataset.id)`, css)))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// savedLayout waits for layout.json in dir to hold want under key.
func savedLayout(t *testing.T, dir, key string, want []string) {
	t.Helper()
	var got map[string][]string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		data, err := os.ReadFile(filepath.Join(dir, "layout.json"))
		got = nil
		if err == nil && json.Unmarshal(data, &got) == nil && slices.Equal(got[key], want) {
			return
		}
	}
	t.Fatalf("layout.json has %s %v, want %v", key, got[key], want)
}

func TestSidebarEntriesAreDraggedIntoAnOrderThatLasts(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	const entry = `#nav .nav-item[data-id]`
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(entry)))
	drag(t, ctx, entry+`[data-id="c"]`, entry+`[data-id="a"]`)
	if got, want := idsOf(t, ctx, entry), []string{"c", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("after dragging: %v, want %v", got, want)
	}
	savedLayout(t, dir, "sidebar", []string{"c", "a", "b"})

	do(t, ctx, chromedp.Reload(), chromedp.WaitVisible(chromedp.CSS(entry)), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="c"]`)))
	if got, want := idsOf(t, ctx, entry), []string{"c", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("after reloading: %v, want %v", got, want)
	}
	if got, want := idsOf(t, ctx, ".card"), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("cards %v, want them in their own order %v", got, want)
	}
}

func TestCardsAreDraggedAndWidenedAndStaySo(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	const card = ".card"
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="c"] .card-value`)))
	drag(t, ctx, `.card[data-id="c"]`, `.card[data-id="a"]`)
	if got, want := idsOf(t, ctx, card), []string{"c", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("after dragging: %v, want %v", got, want)
	}
	savedLayout(t, dir, "cards", []string{"c", "a", "b"})

	// Widening is undone as easily, so one click does it.
	if label := text(t, ctx, `.card[data-id="b"] .card-width`); label != "加宽" {
		t.Errorf("width button says %q, want 加宽", label)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="b"] .card-width`)))
	savedLayout(t, dir, "wide", []string{"b"})

	do(t, ctx, chromedp.Reload(), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="b"] .card-value`)))
	if got, want := idsOf(t, ctx, card), []string{"c", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("after reloading: %v, want %v", got, want)
	}
	widths, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](`['a', 'b'].map(id => document.querySelector('.card[data-id="' + id + '"]').getBoundingClientRect().width)`))
	if err != nil {
		t.Fatal(err)
	}
	if widths[1] < 1.8*widths[0] {
		t.Errorf("widths of a and b: %v, want b about twice as wide", widths)
	}
	if label := text(t, ctx, `.card[data-id="b"] .card-width`); label != "恢复" {
		t.Errorf("width button of a wide card says %q, want 恢复", label)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="b"] .card-width`)))
	savedLayout(t, dir, "wide", nil)
}

// A web page on this machine is embedded from the plugins page by its address,
// or by dropping its address onto the console, and opens at once.
func TestAWebPageIsEmbeddedByItsAddressOrByDroppingIt(t *testing.T) {
	dir := t.TempDir()
	svc := framingRefusingService(t)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	submit := func(address, name string) {
		t.Helper()
		js := fmt.Sprintf(`document.querySelector('#page-address').value = %q; document.querySelector('#page-name').value = %q; document.querySelector('#page-form button').click()`, address, name)
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](js)); err != nil {
			t.Fatal(err)
		}
	}
	opened := func(id string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`location.hash === %q`, "#/"+id))); err != nil {
			t.Fatalf("waiting for #/%s: %v; at %q", id, err, text(t, ctx, "#catalog-note"))
		}
		if got, ok := tryFrameText(ctx, "#frames iframe:not([hidden])", "#svc", 15*time.Second); !ok || got != "Hello from the service" {
			t.Errorf("%s shows %q, %v; want the service's page", id, got, ok)
		}
	}

	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS("#page-address")))
	submit("8.8.8.8", "")
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#catalog-note').textContent.includes('局域网')`)); err != nil {
		t.Errorf("an address on the internet: %v; the note says %q", err, text(t, ctx, "#catalog-note"))
	}

	id := strings.NewReplacer(".", "-", ":", "-").Replace(strings.TrimPrefix(svc, "http://"))
	submit(svc, "服务")
	opened(id)
	if got := text(t, ctx, fmt.Sprintf(`.nav-item[data-id=%q] .nav-name`, id)); got != "服务" {
		t.Errorf("the sidebar names it %q, want 服务", got)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`.nav-item[href="#/"]`)), chromedp.WaitVisible(chromedp.CSS("#overview")))
	_, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf(`(() => {
		const dt = new DataTransfer();
		dt.setData('text/uri-list', %q);
		for (const type of ['dragenter', 'dragover', 'drop']) document.querySelector('#cards').dispatchEvent(new DragEvent(type, {bubbles: true, cancelable: true, dataTransfer: dt}));
	})()`, svc+"/")))
	if err != nil {
		t.Fatal(err)
	}
	opened(id + "-2")
}

// A plugin that does not load is named from its manifest. A missing variable
// is named on the overview and on the plugins page, and the file path is not
// part of that sentence. A green dot means the address answered.
func TestAFailedPluginSaysItsNameAndWhichVariableIsMissing(t *testing.T) {
	if _, ok := os.LookupEnv("HOSTDECK_TEST_ABSENT"); ok {
		t.Fatal("HOSTDECK_TEST_ABSENT is set; this test needs it unset")
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(up.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "bad", "schema: 1\nname: {en: Broken, zh-CN: 坏了}\npage: {lnk: 'http://127.0.0.1:1/'}\n")
	writePlugin(t, dir, "sync", `schema: 1
name: Syncthing
card:
  http:
    url: http://127.0.0.1:9/rest
    headers: {X-API-Key: "${env:HOSTDECK_TEST_ABSENT}"}
  value: n
`)
	writePlugin(t, dir, "up", fmt.Sprintf("schema: 1\nname: Up\npage: {proxy: %q}\n", up.URL+"/"))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="sync"] .card-error`)))
	if got := text(t, ctx, `.card[data-id="bad"] .card-name`); got != "坏了" {
		t.Errorf("broken card is named %q, want the name from its manifest", got)
	}
	sentence := text(t, ctx, `.card[data-id="sync"] .card-error`)
	if !strings.Contains(sentence, "HOSTDECK_TEST_ABSENT") || strings.Contains(sentence, dir) {
		t.Errorf("overview says %q, want the variable and not the folder", sentence)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="sync"] .error-file summary`)))
	if got := text(t, ctx, `.card[data-id="sync"] .error-file p`); !strings.Contains(got, filepath.Join(dir, "plugins", "sync", "plugin.yaml")) {
		t.Errorf("file disclosure %q, want the manifest path", got)
	}

	const sync = `#catalog-installed .catalog-item[data-id="sync"]`
	do(t, ctx, chromedp.Click(chromedp.CSS(`.nav-item[href="#/.plugins"]`)), chromedp.WaitVisible(chromedp.CSS(sync+" .catalog-state")))
	if got := text(t, ctx, sync+" .catalog-state"); !strings.Contains(got, "HOSTDECK_TEST_ABSENT") || strings.Contains(got, dir) {
		t.Errorf("plugins page says %q, want the variable and not the folder", got)
	}

	do(t, ctx, chromedp.WaitVisible(chromedp.CSS(`.nav-item[data-id="up"] .dot[data-status="up"]`)))
	title, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.querySelector('.nav-item[data-id="up"] .dot').title`))
	if err != nil {
		t.Fatal(err)
	}
	if title != "这个地址有应答" {
		t.Errorf("dot title %q, want it to say the address answered", title)
	}
}

// The big number stays on one line beside a normal label, a long label stays
// inside the card, and the widen button does not take a place in the row.
// On a screen with no hover, the button is visible.
func TestACardKeepsItsNumberTogetherAndInsideTheCard(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/free":
			io.WriteString(w, `{"free":"209 GB"}`)
		default:
			io.WriteString(w, `{"n":"123456789012345678901234567890 GB"}`)
		}
	}))
	t.Cleanup(svc.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "disk", fmt.Sprintf(`schema: 1
name: {en: Disk space, zh-CN: 磁盘空间}
card:
  http: {url: %q}
  value: free
  label: {en: "free of 460 GB", zh-CN: "可用，共 460 GB"}
`, svc.URL+"/free"))
	writePlugin(t, dir, "long", fmt.Sprintf(`schema: 1
name: {en: Long, zh-CN: 很长}
card:
  http: {url: %q}
  value: n
  label: {en: "a very long label that sits beside the number", zh-CN: "一个很长的说明写在数字旁边"}
`, svc.URL+"/n"))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	do(t, ctx,
		chromedp.EmulateViewport(1280, 860),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="disk"] .card-value`)),
	)

	fits := func(id string) (valueLines float64, labelInside bool, btnPos, btnOpacity string) {
		t.Helper()
		raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](fmt.Sprintf(`(() => {
			const card = document.querySelector('.card[data-id=%q]');
			const v = card.querySelector('.card-value');
			const label = card.querySelector('.card-label');
			const btn = card.querySelector('.card-width');
			const line = parseFloat(getComputedStyle(v).lineHeight) || parseFloat(getComputedStyle(v).fontSize) * 1.2;
			const cr = card.getBoundingClientRect();
			const lr = label.getBoundingClientRect();
			return JSON.stringify({
				lines: v.getBoundingClientRect().height / line,
				inside: lr.right <= cr.right + 1 && lr.left >= cr.left - 1 && lr.bottom <= cr.bottom + 1,
				pos: getComputedStyle(btn).position,
				opacity: getComputedStyle(btn).opacity
			});
		})()`, id)))
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Lines   float64 `json:"lines"`
			Inside  bool    `json:"inside"`
			Pos     string  `json:"pos"`
			Opacity string  `json:"opacity"`
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		return got.Lines, got.Inside, got.Pos, got.Opacity
	}

	if lines, inside, pos, opacity := fits("disk"); lines > 1.4 || !inside || pos != "absolute" || opacity != "0" {
		t.Errorf("disk at 1280: lines %.2f inside %v button %s opacity %s; want one line, label inside, absolute button, hidden", lines, inside, pos, opacity)
	}
	if _, inside, _, _ := fits("long"); !inside {
		t.Errorf("the long label leaves its card at 1280")
	}

	do(t, ctx, chromedp.EmulateViewport(520, 860))
	if lines, inside, _, _ := fits("disk"); lines > 1.4 || !inside {
		t.Errorf("disk at 520: lines %.2f inside %v; want one line, label inside", lines, inside)
	}

	// A headless page is not focused, so a script focus sets activeElement and
	// still leaves :focus unmatched. Focus emulation is what a real window does.
	if _, err := chromedp.Call(ctx, emulation.SetFocusEmulationEnabled, emulation.SetFocusEmulationEnabledParams{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Focus(chromedp.CSS(`.card[data-id="disk"] .card-width`)))
	raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const btn = document.querySelector('.card[data-id="disk"] .card-width');
		return JSON.stringify({
			hasFocus: document.hasFocus(),
			active: document.activeElement === btn,
			focus: btn.matches(':focus'),
			opacity: getComputedStyle(btn).opacity
		});
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"opacity":"1"`) {
		t.Errorf("after focus: %s", raw)
	}

	// Focus leaves the button. A touch screen has no hover, so the button stays
	// visible. setEmulatedMedia does not change hover; touch emulation does.
	do(t, ctx, chromedp.Focus(chromedp.CSS("a.brand")))
	if _, err := chromedp.Call(ctx, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{
		Enabled:        true,
		MaxTouchPoints: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, pos, opacity := fits("disk"); pos != "absolute" || opacity != "1" {
		t.Errorf("widen button on a touch screen is %s with opacity %s, want absolute and 1", pos, opacity)
	}
}

// Below 640px the page starts under a short bar and the menu opens over it.
// However many plugins there are, Plugins and the language control stay at the
// ends of the sidebar, and Overview lines up with a plugin's name.
func TestANarrowWindowStartsUnderABarAndTheMenuEndsStayPut(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 24; i++ {
		id := fmt.Sprintf("p%02d", i)
		writePlugin(t, dir, id, fmt.Sprintf("schema: 1\nname: %s\npage: {link: 'https://%s.example/'}\n", id, id))
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	type box struct {
		Top     float64 `json:"top"`
		Bottom  float64 `json:"bottom"`
		Left    float64 `json:"left"`
		Right   float64 `json:"right"`
		Display string  `json:"display"`
		Missing bool    `json:"missing"`
	}
	type layout struct {
		VH          float64 `json:"vh"`
		H1          box     `json:"h1"`
		Menu        box     `json:"menu"`
		Overview    box     `json:"overview"`
		Plugin      box     `json:"plugin"`
		Catalog     box     `json:"catalog"`
		CatalogName box     `json:"catalogName"`
		Lang        box     `json:"lang"`
		Nav         box     `json:"nav"`
		NavScroll   float64 `json:"navScroll"`
	}
	measure := func() layout {
		t.Helper()
		raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
			const box = (sel) => {
				const e = document.querySelector(sel);
				if (!e) return {missing: true};
				const r = e.getBoundingClientRect();
				return {top: r.top, bottom: r.bottom, left: r.left, right: r.right, display: getComputedStyle(e).display};
			};
			const nav = document.querySelector('#nav');
			return JSON.stringify({
				vh: innerHeight,
				h1: box('h1'),
				menu: box('#menu'),
				overview: box('.nav-item[href="#/"] .nav-name'),
				plugin: box('.nav-item[data-id] .nav-name'),
				catalog: box('.nav-item[href="#/.plugins"]'),
				catalogName: box('.nav-item[href="#/.plugins"] .nav-name'),
				lang: box('.lang'),
				nav: box('#nav'),
				navScroll: nav ? nav.scrollHeight - nav.clientHeight : -1
			});
		})()`))
		if err != nil {
			t.Fatal(err)
		}
		var got layout
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	seen := func(b box) bool {
		return !b.Missing && b.Display != "none" && b.Bottom > 0 && b.Top < 4000 && b.Right > b.Left
	}
	inView := func(b box, vh float64) bool {
		return seen(b) && b.Top >= -1 && b.Bottom <= vh+1
	}

	do(t, ctx,
		chromedp.EmulateViewport(1280, 640),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`.nav-item[data-id="p01"]`)),
	)
	wide := measure()
	if wide.Menu.Missing || wide.Menu.Display != "none" {
		t.Errorf("wide menu: missing %v display %s, want a button that is not shown", wide.Menu.Missing, wide.Menu.Display)
	}
	if wide.Overview.Missing || wide.Plugin.Missing || abs(wide.Overview.Left-wide.Plugin.Left) > 1 || abs(wide.CatalogName.Left-wide.Plugin.Left) > 1 {
		t.Errorf("names at 1280: overview %v plugin %v plugins-page %v, want the same left edge", wide.Overview, wide.Plugin, wide.CatalogName)
	}
	if !inView(wide.Catalog, wide.VH) || !inView(wide.Lang, wide.VH) || wide.NavScroll < 20 {
		t.Errorf("sidebar ends at 1280: plugins-page %v lang %v middle scroll %.0f vh %.0f, want both ends in view and the middle scrolling", wide.Catalog, wide.Lang, wide.NavScroll, wide.VH)
	}

	do(t, ctx, chromedp.EmulateViewport(520, 700))
	closed := measure()
	if closed.H1.Missing || closed.H1.Top < 40 || closed.H1.Top > 96 {
		t.Errorf("overview heading at 520: %+v, want it just under a bar about 48px tall", closed.H1)
	}
	if !inView(closed.Menu, closed.VH) || closed.Menu.Bottom > 48 {
		t.Errorf("menu button at 520: %+v, want it inside the top bar", closed.Menu)
	}
	if closed.Nav.Right > 1 {
		t.Errorf("closed menu still shows the list: right %.0f", closed.Nav.Right)
	}
	if closed.Menu.Missing {
		t.Fatal("no menu button to open")
	}

	do(t, ctx, chromedp.Click(chromedp.CSS("#menu")))
	open := measure()
	if open.Nav.Right <= 1 || open.H1.Top < 40 || open.H1.Top > 96 {
		t.Errorf("open menu at 520: list %+v heading %+v, want the list over the page and the heading still under the bar", open.Nav, open.H1)
	}
	if !inView(open.Catalog, open.VH) || !inView(open.Lang, open.VH) || open.NavScroll < 20 {
		t.Errorf("open menu ends: plugins-page %v lang %v middle scroll %.0f, want both ends in view and the middle scrolling", open.Catalog, open.Lang, open.NavScroll)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`.nav-item[href="#/.plugins"]`)), chromedp.WaitVisible(chromedp.CSS("#catalog h1")))
	if after := measure(); after.Nav.Right > 1 {
		t.Errorf("the menu stays open over the plugins page: list right %.0f", after.Nav.Right)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// A card that failed is read again while the overview stays open, and it says
// when it was read. Refresh asks a card again before its interval.
func TestAFailedCardIsReadAgainWhileTheOverviewStaysOpen(t *testing.T) {
	var up atomic.Bool
	var hits atomic.Int32
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if !up.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, `{"v":"%d"}`, n)
	}))
	t.Cleanup(svc.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "health", fmt.Sprintf(`schema: 1
name: {en: Health, zh-CN: 健康}
card:
  http: {url: %q}
  value: v
  interval: 1h
`, svc.URL))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	do(t, ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="health"] .card-error`)),
	)
	up.Store(true)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.card[data-id="health"] .card-value')?.textContent === '2' || document.querySelector('.card[data-id="health"] .card-value')?.textContent > '1'`)); err != nil {
		t.Fatal(err)
	}
	// The value above may be "2" from the failed attempt counting. Accept any
	// non-empty value once the error is gone, then require it to hold.
	value := text(t, ctx, `.card[data-id="health"] .card-value`)
	if value == "" {
		t.Fatal("the card recovered without a value")
	}
	updated := text(t, ctx, `.card[data-id="health"] .card-updated`)
	if !strings.Contains(updated, "读取于") {
		t.Errorf("updated line %q, want it to say when the card was read", updated)
	}
	held := hits.Load()
	time.Sleep(2500 * time.Millisecond)
	if hits.Load() != held || text(t, ctx, `.card[data-id="health"] .card-value`) != value {
		t.Errorf("a card that succeeded was read again: value %s hits %d, was %s after %d", text(t, ctx, `.card[data-id="health"] .card-value`), hits.Load(), value, held)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS("#refresh")))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector('.card[data-id="health"] .card-value')?.textContent !== %q`, value))); err != nil {
		t.Fatal(err)
	}
}

// An overview card opens its plugin's page. The plugins page says what each
// plugin provides, and opens one that is in the sidebar.
func TestAnOverviewCardOpensThatPluginsPageAndThePluginsPageSaysWhatItOffers(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/health") {
			io.WriteString(w, `{"v":"ok"}`)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html><html><body><h1 id="mark">Embedded</h1></body></html>`)
	}))
	t.Cleanup(svc.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "board", fmt.Sprintf(`schema: 1
name: {en: Board, zh-CN: 看板}
page: {proxy: %q}
card:
  http: {url: %q}
  value: v
`, svc.URL+"/", svc.URL+"/health"))
	writePlugin(t, dir, "list", fmt.Sprintf(`schema: 1
name: {en: List, zh-CN: 清单}
card:
  http: {url: %q}
  value: v
detail:
  exec: {command: [%q, fake-plugin, detail]}
actions:
  exec: {command: [%q, fake-plugin, action]}
`, svc.URL+"/health", exe, exe))
	writePlugin(t, dir, "only", fmt.Sprintf(`schema: 1
name: {en: Only, zh-CN: 仅卡片}
card:
  http: {url: %q}
  value: v
`, svc.URL+"/health"))
	writePlugin(t, dir, "out", fmt.Sprintf(`schema: 1
name: {en: Out, zh-CN: 外链}
page: {link: 'https://example.test/'}
card:
  http: {url: %q}
  value: v
`, svc.URL+"/health"))
	writePlugin(t, dir, ".off", "schema: 1\nname: {en: Off, zh-CN: 停用}\npage: {link: 'https://off.example/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	count := func(css string) int {
		t.Helper()
		n, err := chromedp.Run(ctx, chromedp.Evaluate[int](fmt.Sprintf(`document.querySelectorAll(%q).length`, css)))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	do(t, ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="board"] .card-value`)),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="list"] .card-value`)),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="only"] .card-value`)),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="out"] .card-value`)),
	)
	if n := count(`.card[data-id="board"] .card-name a`); n != 1 {
		t.Fatalf("the board card has %d links, want one that opens its page", n)
	}

	do(t, ctx,
		chromedp.Click(chromedp.CSS(`.card[data-id="board"] .card-value`)),
		chromedp.WaitVisible(chromedp.CSS("#service-name")),
	)
	if name := text(t, ctx, "#service-name"); name != "看板" {
		t.Errorf("opened %q, want the board's page", name)
	}
	if got, ok := tryFrameText(ctx, "#frames iframe", "#mark", 15*time.Second); !ok || got != "Embedded" {
		t.Errorf("embedded page %q, %v; want the service", got, ok)
	}

	do(t, ctx,
		chromedp.Click(chromedp.CSS("#nav-overview")),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="list"] .card-name a`)),
		chromedp.Click(chromedp.CSS(`.card[data-id="list"] .card-name a`)),
		chromedp.WaitVisible(chromedp.CSS(".detail-item")),
	)
	if name, item := text(t, ctx, "#service-name"), text(t, ctx, ".detail-name"); name != "清单" || item != "服务" {
		t.Errorf("opened %q item %q, want the list's detail page", name, item)
	}

	do(t, ctx,
		chromedp.Click(chromedp.CSS("#nav-overview")),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="only"] .card-value`)),
	)
	if n := count(`.card[data-id="only"] .card-name a`); n != 0 {
		t.Errorf("a card with no page has %d links, want none", n)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="only"] .card-value`)))
	if title := text(t, ctx, "h1"); title != "总览" {
		t.Errorf("clicking a card with no page left the overview for %q", title)
	}
	link := attrs(t, ctx, `.card[data-id="out"] .card-name a`)
	if link["href"] != "https://example.test/" || link["target"] != "_blank" {
		t.Errorf("out card link %#v, want the external page in a new tab", link)
	}

	do(t, ctx,
		chromedp.Click(chromedp.CSS(`.nav-item[href="#/.plugins"]`)),
		chromedp.WaitVisible(chromedp.CSS(`.catalog-item[data-id="list"] .catalog-provides`)),
	)
	says := func(id string) string {
		t.Helper()
		return text(t, ctx, fmt.Sprintf(`.catalog-item[data-id="%s"] .catalog-provides`, id))
	}
	if got := says("list"); !strings.Contains(got, "卡片") || !strings.Contains(got, "详情") || !strings.Contains(got, "操作") || strings.Contains(got, "侧栏页面") {
		t.Errorf("list provides %q, want a card, a detail page and actions", got)
	}
	if got := says("board"); !strings.Contains(got, "侧栏页面") || !strings.Contains(got, "卡片") || strings.Contains(got, "详情") {
		t.Errorf("board provides %q, want a sidebar page and a card", got)
	}
	if got := says("only"); got != "卡片" {
		t.Errorf("only provides %q, want just a card", got)
	}
	if got := says("off"); got != "侧栏页面" {
		t.Errorf("disabled plugin provides %q, want a sidebar page", got)
	}
	if got := says("out"); !strings.Contains(got, "侧栏页面") || !strings.Contains(got, "卡片") {
		t.Errorf("out provides %q, want a sidebar page and a card", got)
	}
	if n := count(`.catalog-item[data-id="only"] .catalog-open`) + count(`.catalog-item[data-id="off"] .catalog-open`); n != 0 {
		t.Errorf("a card-only or disabled plugin has an Open control")
	}
	open := attrs(t, ctx, `.catalog-item[data-id="out"] .catalog-open`)
	if open["href"] != "https://example.test/" || open["target"] != "_blank" {
		t.Errorf("out's Open is %#v, want the external page in a new tab", open)
	}
	do(t, ctx,
		chromedp.Click(chromedp.CSS(`.catalog-item[data-id="list"] .catalog-open`)),
		chromedp.WaitVisible(chromedp.CSS(".detail-item")),
	)
	if name := text(t, ctx, "#service-name"); name != "清单" {
		t.Errorf("Open went to %q, want the list", name)
	}
}

// The plugins page orders plugins by the name it shows, not by the folder.
func TestThePluginsPageListsPluginsByName(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "a", "schema: 1\nname: {en: Beta, zh-CN: 乙}\npage: {link: 'https://b.example/'}\n")
	writePlugin(t, dir, "b", "schema: 1\nname: {en: Alpha, zh-CN: 甲}\npage: {link: 'https://a.example/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	do(t, ctx,
		chromedp.Navigate(url),
		chromedp.Click(chromedp.CSS(`.nav-item[href="#/.plugins"]`)),
		chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item`)),
	)
	if got := idsOf(t, ctx, "#catalog-installed .catalog-item"); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("plugins page order %v, want 甲 then 乙", got)
	}
	if shown, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => { const s = document.querySelector('#catalog-search'); return !!s && !s.hidden; })()`)); err != nil || shown {
		t.Errorf("a short plugins page shows a search box (err %v)", err)
	}
}

// Installed plugins are grouped into what is in use, what is disabled and
// what failed to load. Each group is ordered by name.
func TestThePluginsPageGroupsWhatIsInUseDisabledAndBroken(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "a", "schema: 1\nname: {en: Beta, zh-CN: 乙}\npage: {link: 'https://b.example/'}\n")
	writePlugin(t, dir, "b", "schema: 1\nname: {en: Alpha, zh-CN: 甲}\npage: {link: 'https://a.example/'}\n")
	writePlugin(t, dir, ".off", "schema: 1\nname: {en: Off, zh-CN: 停用}\npage: {link: 'https://off.example/'}\n")
	writePlugin(t, dir, "bad", "this is: [not yaml\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	do(t, ctx,
		chromedp.Navigate(url),
		chromedp.Click(chromedp.CSS(`.nav-item[href="#/.plugins"]`)),
		chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item`)),
	)
	group := func(name string) []string {
		t.Helper()
		return idsOf(t, ctx, `#catalog-installed [data-group="`+name+`"] .catalog-item`)
	}
	if got := group("use"); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("in use %v, want 甲 then 乙", got)
	}
	if got := group("disabled"); !slices.Equal(got, []string{"off"}) {
		t.Fatalf("disabled %v, want off", got)
	}
	if got := group("error"); !slices.Equal(got, []string{"bad"}) {
		t.Fatalf("broken %v, want bad", got)
	}
	if use, disabled, broken := text(t, ctx, `[data-group="use"] h3`), text(t, ctx, `[data-group="disabled"] h3`), text(t, ctx, `[data-group="error"] h3`); use != "使用中" || disabled != "已停用" || broken != "有问题" {
		t.Errorf("group headings %q %q %q", use, disabled, broken)
	}
}

// Cards in one row keep their own height. A short card is not stretched to
// the tall one beside it.
func TestPluginCardsLineUpAtTheTop(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "short", "schema: 1\nname: {en: Short, zh-CN: 短}\npage: {link: 'https://short.example/'}\n")
	writePlugin(t, dir, "tall", "schema: 1\nname: {en: Tall, zh-CN: 高}\ndescription: {en: A long description, zh-CN: 这是一段故意写得很长的说明，用来把这张卡片撑高，好让旁边那张短卡片不必跟着变高。它再说一句，确保高度真的不一样。}\npage: {link: 'https://tall.example/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	do(t, ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url),
		chromedp.Click(chromedp.CSS(`.nav-item[href="#/.plugins"]`)),
		chromedp.WaitVisible(chromedp.CSS(`.catalog-item[data-id="tall"] .catalog-description`)),
	)
	heights, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](`['short', 'tall'].map(id => document.querySelector('.catalog-item[data-id="' + id + '"]').getBoundingClientRect().height)`))
	if err != nil {
		t.Fatal(err)
	}
	if !(heights[0]+24 < heights[1]) {
		t.Errorf("card heights short %.0f tall %.0f, want the short card to stay short", heights[0], heights[1])
	}
}

// Disabling a plugin says so on that plugin's card.
func TestTheResultSitsOnThePluginJustChanged(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "keep", "schema: 1\nname: Keep\npage: {link: 'https://keep.example/'}\n")
	writePlugin(t, dir, "site", "schema: 1\nname: Site\npage: {link: 'https://site.example/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	const button = `.catalog-item[data-id="site"] button`

	do(t, ctx,
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(button)),
		chromedp.Click(chromedp.CSS(button)),
	)
	do(t, ctx, chromedp.Click(chromedp.CSS(button)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.catalog-item[data-id="site"] .catalog-state')?.textContent.includes('.site')`)); err != nil {
		t.Fatal(err)
	}
	onCard, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.catalog-item[data-id="site"]').contains(document.querySelector('#catalog-note')) && !document.querySelector('.catalog-item[data-id="keep"]').contains(document.querySelector('#catalog-note'))`))
	if err != nil {
		t.Fatal(err)
	}
	if !onCard {
		t.Errorf("the note is not on Site's card: %q", text(t, ctx, "#catalog-note"))
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(button)), chromedp.Click(chromedp.CSS(button)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.catalog-item[data-id="site"]')?.contains(document.querySelector('#catalog-note')) && document.querySelector('#catalog-note')?.textContent.includes('已启用')`)); err != nil {
		t.Fatal(err)
	}
}

// Adding a page and the built-in plugins are one block, after the plugins
// already installed.
func TestTheEmbedFormAndTheBuiltInPluginsAreOneBlock(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	do(t, ctx,
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS("#page-form")),
		chromedp.WaitVisible(chromedp.CSS("#catalog-available .catalog-item")),
	)
	ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const block = document.querySelector('#page-form').parentElement;
		const installed = document.querySelector('#catalog-installed');
		return block.id === 'catalog-add' && block.contains(document.querySelector('#catalog-available')) && !block.contains(installed) && !!(installed.compareDocumentPosition(block) & Node.DOCUMENT_POSITION_FOLLOWING);
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("the embed form and the built-in plugins are not one block after the installed plugins")
	}
}

// A long plugins page can be searched by the name it shows. A short one has
// no search box.
func TestThePluginsPageSearchesWhenThereAreMany(t *testing.T) {
	dir := t.TempDir()
	for i := range 9 {
		id := fmt.Sprintf("n%d", i)
		name := "插件" + fmt.Sprint(i)
		if i == 0 {
			name = "目标"
		}
		writePlugin(t, dir, id, fmt.Sprintf("schema: 1\nname: %s\npage: {link: 'https://%s.example/'}\n", name, id))
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	do(t, ctx,
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item`)),
	)
	shown, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => { const s = document.querySelector('#catalog-search'); return !!s && !s.hidden; })()`))
	if err != nil {
		t.Fatal(err)
	}
	if !shown {
		t.Fatal("a long plugins page has no search box")
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => { const s = document.querySelector('#catalog-search'); s.value = '目标'; s.dispatchEvent(new Event('input', {bubbles: true})); })()`)); err != nil {
		t.Fatal(err)
	}
	if got := idsOf(t, ctx, `#catalog .catalog-item:not([hidden])`); !slices.Equal(got, []string{"n0"}) {
		t.Errorf("search shows %v, want only 目标", got)
	}
}

// Sidebar entries and overview cards move up and down from buttons the
// keyboard can reach. The move is saved at once, and one undo puts it back.
// Dragging is left as it is.
func TestSidebarEntriesAndCardsMoveUpAndDownAndTheMoveCanBeUndone(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	const entry = `#nav .nav-item[data-id]`
	do(t, ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(entry+`[data-id="c"]`)),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="c"]`)),
	)

	kind, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const row = (id) => document.querySelector('#nav .nav-row[data-id="' + id + '"]');
		const up = row('a')?.querySelector('.move-up');
		const downA = row('a')?.querySelector('.move-down');
		const downC = row('c')?.querySelector('.move-down');
		if (!up || !downA || !downC) return 'missing';
		if (up.tagName !== 'BUTTON' || up.type !== 'button' || up.closest('a')) return 'inside-link';
		if (!up.disabled || !downC.disabled || downA.disabled) return 'ends';
		if (up.tabIndex < 0 || downA.tabIndex < 0) return 'not-tabbable';
		return 'ok ' + downA.textContent;
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if kind != "ok 下移" {
		t.Fatalf("sidebar move buttons: %s", kind)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .move-down`)))
	if got, want := idsOf(t, ctx, entry), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Errorf("after moving a down: %v, want %v", got, want)
	}
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
	if got, want := idsOf(t, ctx, ".card"), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("cards %v, want their own order %v", got, want)
	}

	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const note = document.querySelector('#layout-note');
		const undo = document.querySelector('#layout-undo');
		return !!note && !note.hidden && note.textContent.includes('布局已保存') && undo && undo.textContent.includes('撤销');
	})()`)); err != nil {
		t.Fatalf("no undo for the sidebar move: %v", err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS("#layout-undo")))
	if got, want := idsOf(t, ctx, entry), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("after undo: %v, want %v", got, want)
	}
	savedLayout(t, dir, "sidebar", []string{"a", "b", "c"})

	do(t, ctx, chromedp.WaitVisible(chromedp.CSS(`.card[data-id="c"] .move-up`)))
	cardEnds, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const up = document.querySelector('.card[data-id="a"] .move-up');
		const down = document.querySelector('.card[data-id="c"] .move-down');
		const mid = document.querySelector('.card[data-id="b"] .move-up');
		if (!up || !down || !mid) return 'missing';
		if (up.tagName !== 'BUTTON' || mid.closest('a')) return 'inside-link';
		if (!up.disabled || !down.disabled || mid.disabled) return 'ends';
		return 'ok ' + mid.textContent;
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if cardEnds != "ok 上移" {
		t.Fatalf("card move buttons: %s", cardEnds)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="c"] .move-up`)))
	if got, want := idsOf(t, ctx, ".card"), []string{"a", "c", "b"}; !slices.Equal(got, want) {
		t.Errorf("after moving c up: %v, want %v", got, want)
	}
	savedLayout(t, dir, "cards", []string{"a", "c", "b"})
	if got, want := idsOf(t, ctx, entry), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("sidebar %v, want it left as %v", got, want)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS("#layout-undo")))
	if got, want := idsOf(t, ctx, ".card"), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("after undoing the card move: %v, want %v", got, want)
	}
	savedLayout(t, dir, "cards", []string{"a", "b", "c"})
}

// Hiding takes a plugin off the sidebar and the overview without uninstalling
// it. Its place is kept, Show puts it back, and Reset clears the layout.
// Each of these is one click and one undo.
func TestHidingAPluginRemovesItUntilItIsShownOrReset(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")

	const entry = `#nav .nav-item[data-id]`
	do(t, ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(entry+`[data-id="c"]`)),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="c"] .card-value`)),
	)

	kind, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const hide = document.querySelector('#nav .nav-row[data-id="a"] .hide');
		const reset = document.querySelector('#layout-reset');
		if (!hide || !reset) return 'missing';
		if (hide.tagName !== 'BUTTON' || hide.type !== 'button' || hide.closest('a')) return 'inside-link';
		if (reset.tagName !== 'BUTTON' || reset.type !== 'button') return 'reset';
		return 'ok ' + hide.textContent + ' ' + reset.textContent;
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if kind != "ok 隐藏 重置" {
		t.Fatalf("hide and reset: %s", kind)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .move-down`)))
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})

	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .hide`)))
	gone(t, ctx, `#nav .nav-item[data-id="a"]`)
	gone(t, ctx, `.card[data-id="a"]`)
	if got, want := idsOf(t, ctx, entry), []string{"b", "c"}; !slices.Equal(got, want) {
		t.Errorf("sidebar after hiding a: %v, want %v", got, want)
	}
	if got, want := idsOf(t, ctx, ".card"), []string{"b", "c"}; !slices.Equal(got, want) {
		t.Errorf("cards after hiding a: %v, want %v", got, want)
	}
	savedLayout(t, dir, "hidden", []string{"a"})
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})

	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const note = document.querySelector('#layout-note');
		const undo = document.querySelector('#layout-undo');
		return !!note && !note.hidden && note.textContent.includes('布局已保存') && undo && undo.textContent.includes('撤销');
	})()`)); err != nil {
		t.Fatalf("no undo for hiding: %v", err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS("#layout-undo")))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!!document.querySelector('#nav .nav-item[data-id="a"]') && !!document.querySelector('.card[data-id="a"]')`)); err != nil {
		t.Fatalf("undo did not bring a back: %v", err)
	}
	if got, want := idsOf(t, ctx, entry), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Errorf("sidebar after undoing the hide: %v, want %v", got, want)
	}
	savedLayout(t, dir, "hidden", nil)

	do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="b"] .hide`)))
	gone(t, ctx, `#nav .nav-item[data-id="b"]`)
	gone(t, ctx, `.card[data-id="b"]`)
	savedLayout(t, dir, "hidden", []string{"b"})
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})

	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog .catalog-item[data-id="b"] .show`)))
	if label := text(t, ctx, `#catalog .catalog-item[data-id="b"] .show`); label != "显示" {
		t.Errorf("show button says %q, want 显示", label)
	}
	showKind, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const show = document.querySelector('#catalog .catalog-item[data-id="b"] .show');
		if (!show) return 'missing';
		if (show.tagName !== 'BUTTON' || show.closest('a')) return 'inside-link';
		return 'ok';
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if showKind != "ok" {
		t.Fatalf("show button: %s", showKind)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#catalog .catalog-item[data-id="b"] .show`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!!document.querySelector('#nav .nav-item[data-id="b"]')`)); err != nil {
		t.Fatalf("show did not bring b back: %v", err)
	}
	savedLayout(t, dir, "hidden", nil)

	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="b"] .card-value`)))
	if got, want := idsOf(t, ctx, entry), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Errorf("sidebar after showing b: %v, want its place kept %v", got, want)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS("#layout-reset")))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`JSON.stringify([...document.querySelectorAll('#nav .nav-item[data-id]')].map(e => e.dataset.id)) === '["a","b","c"]'`)); err != nil {
		t.Fatalf("reset did not restore folder order: %v; sidebar %v", err, idsOf(t, ctx, entry))
	}
	savedLayout(t, dir, "sidebar", nil)
	savedLayout(t, dir, "hidden", nil)

	do(t, ctx, chromedp.Click(chromedp.CSS("#layout-undo")))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`JSON.stringify([...document.querySelectorAll('#nav .nav-item[data-id]')].map(e => e.dataset.id)) === '["b","a","c"]'`)); err != nil {
		t.Fatalf("undo of reset: %v; sidebar %v", err, idsOf(t, ctx, entry))
	}
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
}

// The plugins page edits a page's name and address in one save. A refused
// address stays in the form, and the form has no field for a command.
func TestThePluginsPageEditsAPageAndKeepsWhatWasTypedWhenItFails(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "meter", `schema: 1
name: {en: Meter, zh-CN: 仪表}
icon: M
page: {proxy: 'http://127.0.0.1:9/old'}
card:
  exec:
    command: [echo, secret-command]
`)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN,zh;q=0.9")
	const item = `#catalog .catalog-item[data-id="meter"]`
	do(t, ctx,
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.Click(chromedp.CSS(item+` .page-edit summary`)),
		chromedp.WaitVisible(chromedp.CSS(item+` input[name="address"]`)),
	)
	got, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const form = document.querySelector('#catalog .catalog-item[data-id="meter"] form');
		const names = [...form.querySelectorAll('[name]')].map((el) => el.name);
		if (names.some((n) => /command|exec|action/.test(n))) return 'command ' + names.join(',');
		const v = (n) => form.elements[n].value;
		return [v('name'), v('icon'), v('address'), v('kind')].join('|');
	})()`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "仪表|M|http://127.0.0.1:9/old|proxy" {
		t.Fatalf("the form shows %q, want the current name, icon and embed address", got)
	}

	set := func(js string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](js)); err != nil {
			t.Fatal(err)
		}
	}
	set(`(() => {
		const form = document.querySelector('#catalog .catalog-item[data-id="meter"] form');
		form.elements.address.value = 'http://8.8.8.8/';
		form.querySelector('button').click();
	})()`)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(document.querySelector('#catalog .catalog-item[data-id="meter"] #catalog-note')?.textContent || '').includes('局域网')`)); err != nil {
		t.Fatalf("a public address: %v; the card says %q", err, text(t, ctx, item))
	}
	kept, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.querySelector('#catalog .catalog-item[data-id="meter"] input[name="address"]').value`))
	if err != nil {
		t.Fatal(err)
	}
	if kept != "http://8.8.8.8/" || text(t, ctx, item+` .card-name`) != "仪表" {
		t.Fatalf("after the refusal the form has %q and the card is %q; want the typed address kept and the old name", kept, text(t, ctx, item+` .card-name`))
	}

	set(`(() => {
		const form = document.querySelector('#catalog .catalog-item[data-id="meter"] form');
		form.elements.name.value = '量表';
		form.elements.address.value = 'http://127.0.0.1:9/now';
		form.querySelector('button').click();
	})()`)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#catalog .catalog-item[data-id="meter"] .card-name')?.textContent === '量表' && document.querySelector('#nav .nav-item[data-id="meter"] .nav-name')?.textContent === '量表'`)); err != nil {
		t.Fatalf("after saving: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "plugins", "meter", "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	file := string(data)
	if !strings.Contains(file, "量表") || !strings.Contains(file, "http://127.0.0.1:9/now") || strings.Contains(file, "8.8.8.8") || !strings.Contains(file, "secret-command") {
		t.Fatalf("plugin.yaml is:\n%s", file)
	}
}
