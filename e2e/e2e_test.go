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
	if row := text(t, ctx, card+" .row-name"); row != "已用" {
		t.Errorf("row %q, want 已用", row)
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
