package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// notifyRecord is the test binary playing a notification command: it appends
// what it was told to file, one line per run.
func notifyRecord(file string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		os.Exit(1)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s->%s\n", os.Getenv("HOSTDECK_ID"), os.Getenv("HOSTDECK_FROM"), os.Getenv("HOSTDECK_TO"))
}

// downService is a plugin whose card is read every second from a service that
// does not answer, and the notifications.json that watches it.
func downService(t *testing.T, dir string) (notifications []byte, runs string) {
	t.Helper()
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(svc.Close)
	writePlugin(t, dir, "svc", fmt.Sprintf("schema: 1\nname: Service\ncard:\n  interval: 1s\n  http:\n    url: %s\n  value: state\n", svc.URL))
	runs = filepath.Join(dir, "runs.txt")
	notifications, err := json.Marshal(map[string]any{"watch": []string{"svc"}, "command": []string{os.Args[0], "notify-record", runs}})
	if err != nil {
		t.Fatal(err)
	}
	return notifications, runs
}

func waitForRun(t *testing.T, runs, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		got, _ := os.ReadFile(runs)
		if strings.TrimSpace(string(got)) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the notification command was told %q within 20s, want %q", got, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestAWatchedCardThatStopsAnsweringRunsTheNotificationCommandWithNoPageOpen(t *testing.T) {
	dir := t.TempDir()
	notifications, runs := downService(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), notifications, 0o644); err != nil {
		t.Fatal(err)
	}
	startConsole(t, dir)
	waitForRun(t, runs, "svc ->unreadable")
}

func TestNotificationsWrittenWhileTheConsoleRunsAreReadByPluginReload(t *testing.T) {
	dir := t.TempDir()
	notifications, runs := downService(t, dir)
	url := startConsole(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), notifications, 0o644); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url+"/api/plugins/reload", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", url)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reload status %d", response.StatusCode)
	}
	waitForRun(t, runs, "svc ->unreadable")
}

// watchedService is a plugin whose card is read every second from a service
// the test turns up or down, watched by notifications.json with command.
func watchedService(t *testing.T, dir string, watch []string, command ...string) *atomic.Value {
	t.Helper()
	var state atomic.Value
	state.Store("up")
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"state": %q}`, state.Load())
	}))
	t.Cleanup(svc.Close)
	writePlugin(t, dir, "svc", fmt.Sprintf("schema: 1\nname: Service\ncard:\n  interval: 1s\n  http:\n    url: %s\n  value: state\n  error: {equals: down}\n", svc.URL))
	data, err := json.Marshal(map[string]any{"watch": watch, "command": command})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return &state
}

// waitForNotifications waits until the console's notification status says want.
func waitForNotifications(t *testing.T, url, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var got []byte
		if resp, err := http.Get(url + "/api/notifications"); err == nil {
			got, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		if strings.Contains(string(got), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the notification status said %s within 20s, want %s in it", got, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitLong(t *testing.T, ctx context.Context, css, want string) {
	t.Helper()
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector(%q)?.textContent.includes(%q)`, css, want), chromedp.WithPollingInterval(200*time.Millisecond), chromedp.WithPollingTimeout(30*time.Second))); err != nil {
		t.Fatalf("%s never said %q; it says %q", css, want, text(t, ctx, css))
	}
}

func TestAWatchedCardSaysWhenItWasCheckedAndThatTheCommandFailedWithoutAReload(t *testing.T) {
	dir := t.TempDir()
	state := watchedService(t, dir, []string{"svc"}, os.Args[0], "notify-fail")
	url := startConsole(t, dir)
	waitForNotifications(t, url, `"checked"`)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="svc"]`)))
	waitText(t, ctx, `.card[data-id="svc"] .card-watch`, "Watching · checked at")

	state.Store("down")
	waitLong(t, ctx, `.card[data-id="svc"] .card-watch-failed`, "no notifier here")

	zh := openBrowser(t, "zh-CN")
	do(t, zh, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="svc"]`)))
	waitText(t, zh, `.card[data-id="svc"] .card-watch`, "关注中")
	waitText(t, zh, `.card[data-id="svc"] .card-watch-failed`, "推送命令")
}

func TestAWatchedCardSaysWhichWayOfTellingFailedAndOnceBothToldTheSameSaysItOnce(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs.txt")
	command := []string{os.Args[0], "notify-record", runs}
	state := watchedService(t, dir, []string{"svc"}, command...)
	var answer atomic.Int32
	answer.Store(http.StatusServiceUnavailable)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(answer.Load()))
		io.WriteString(w, "busy")
	}))
	t.Cleanup(hook.Close)
	data, err := json.Marshal(map[string]any{"watch": []string{"svc"}, "command": command, "webhook": map[string]any{"url": hook.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	waitForNotifications(t, url, `"checked"`)
	ctx := openBrowser(t, "en")
	svc := `.card[data-id="svc"]`
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(svc)))

	state.Store("down")
	waitLong(t, ctx, svc+` .card-watch-webhook-failed`, "The webhook failed at")
	waitText(t, ctx, svc+` .card-watch-webhook-failed`, "503 Service Unavailable: busy")
	waitText(t, ctx, svc+` .card-watch`, "command told Problem at")
	if evalBool(t, ctx, `!!document.querySelector('.card[data-id="svc"] .card-watch-failed')`) {
		t.Fatal("the command that succeeded shows as failed")
	}

	zh := openBrowser(t, "zh-CN")
	do(t, zh, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(svc)))
	waitText(t, zh, svc+` .card-watch-webhook-failed`, "Webhook 在")

	answer.Store(http.StatusOK)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('.card[data-id="svc"] .card-watch-webhook-failed')`, chromedp.WithPollingInterval(200*time.Millisecond), chromedp.WithPollingTimeout(30*time.Second))); err != nil {
		t.Fatalf("the webhook's failure stayed after it answered; the card says %q", text(t, ctx, svc+` .card-watch`))
	}
	waitText(t, ctx, svc+` .card-watch`, " · told Problem at")
	if got := text(t, ctx, svc+` .card-watch`); strings.Contains(got, "command") || strings.Contains(got, "webhook") {
		t.Fatalf("once both told the same the card says %q, want it said once", got)
	}
}

func TestAboveTheCardsTheOverviewSaysWhatIsWatchedButNotThereAndWhyNotificationsJSONWasRefused(t *testing.T) {
	dir := t.TempDir()
	watchedService(t, dir, []string{"svc", "gone"}, os.Args[0], "notify-record", filepath.Join(dir, "runs.txt"))
	url := startConsole(t, dir)
	waitForNotifications(t, url, `"gone"`)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="svc"]`)))
	waitText(t, ctx, `#watch-missing`, "gone")

	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), []byte(`{"watch": ["svc"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url+"/api/plugins/reload", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", url)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	do(t, ctx, chromedp.Reload(), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="svc"]`)))
	waitText(t, ctx, `#watch-error`, "a command")
}

func readNotifications(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "notifications.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTheWatchToggleOnACardWritesOnlyTheWatchListAndIsRefusedWithoutACommand(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs.txt")
	watchedService(t, dir, []string{}, os.Args[0], "notify-record", runs)
	writePlugin(t, dir, "notes", "schema: 1\nname: Notes\npage:\n  link: https://example.com/\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	svc := `.card[data-id="svc"]`
	toggle := func() {
		do(t, ctx, chromedp.Click(chromedp.CSS(svc+` .more-trigger`)), chromedp.Click(chromedp.CSS(svc+` .watch-toggle`)))
	}
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(svc)), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="notes"]`)))
	if evalBool(t, ctx, `!!document.querySelector('.card[data-id="notes"] .watch-toggle')`) {
		t.Fatal("a card that only opens a link offers Watch")
	}

	toggle()
	waitText(t, ctx, svc+` .card-watch`, "Watching")
	var cfg struct {
		Watch   []string `json:"watch"`
		Command []string `json:"command"`
	}
	if err := json.Unmarshal([]byte(readNotifications(t, dir)), &cfg); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Watch, ",") != "svc" || len(cfg.Command) != 3 || cfg.Command[2] != runs {
		t.Fatalf("notifications.json became %+v, want svc watched and the command kept", cfg)
	}

	toggle()
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('.card[data-id="svc"] .card-watch')`, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatal("the watch line stayed after Stop watching")
	}
	if got := readNotifications(t, dir); strings.Contains(got, `"svc"`) {
		t.Fatalf("notifications.json still watches svc: %s", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), []byte(`{"watch": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	toggle()
	waitText(t, ctx, svc+` .card-note`, "command")
	if got := readNotifications(t, dir); got != `{"watch": []}` {
		t.Fatalf("a refused watch rewrote notifications.json as %s", got)
	}

	response, err := http.Post(url+"/api/notifications/watch", "application/json", strings.NewReader(`{"id": "svc", "watch": true}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden || readNotifications(t, dir) != `{"watch": []}` {
		t.Fatalf("a request without the console's Origin answered %d and left %s", response.StatusCode, readNotifications(t, dir))
	}

	zh := openBrowser(t, "zh-CN")
	do(t, zh, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(svc)), chromedp.Click(chromedp.CSS(svc+` .more-trigger`)))
	waitText(t, zh, svc+` .watch-toggle`, "关注")
}
