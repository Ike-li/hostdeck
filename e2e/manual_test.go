package e2e

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestServicesStartedByHandShowWhereTheyCameFrom(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	const add = `#catalog-available .catalog-item[data-id="manual"] .btn`
	do(t, ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(add)),
		chromedp.Click(chromedp.CSS(add)),
		chromedp.Click(chromedp.CSS(add)),
		chromedp.WaitVisible(chromedp.CSS(`#nav .nav-item[data-id="manual"]`)),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="manual"]`)),
	)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const card = document.querySelector('#cards .card[data-id="manual"]');
		if (!card || card.querySelector('.card-pending')) return false;
		return !!(card.querySelector('.card-label') || card.querySelector('.card-error'));
	})()`, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
		t.Fatal(err)
	}
	if errText := evalString(t, ctx, `document.querySelector('#cards .card[data-id="manual"] .card-error')?.textContent || ''`); errText != "" {
		t.Fatalf("card error: %s", errText)
	}

	for _, lang := range []struct{ id, empty, started, up string }{
		{"zh-CN", "没有手动启动的服务", "关掉启动它的终端", "已运行"},
		{"en", "No services started by hand", "", " · up "},
	} {
		do(t, ctx, chromedp.EmulateViewport(1280, 800))
		// A resize back from the drawer leaves the sidebar inert until the
		// matchMedia listener runs. Click only once the button is the element
		// under the pointer.
		clickOnScreen(t, ctx, `button[data-lang="`+lang.id+`"]`)
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
			`document.documentElement.lang === '`+lang.id+`'`,
			chromedp.WithPollingTimeout(15*time.Second),
		)); err != nil {
			t.Fatalf("language %s: %v; page says %s", lang.id, err, evalString(t, ctx, `document.documentElement.lang + '\n' + (document.querySelector('#cards .card[data-id="manual"]')?.innerText || '')`))
		}
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
			`!!document.querySelector('#cards .card[data-id="manual"] .card-label')`,
			chromedp.WithPollingTimeout(25*time.Second),
		)); err != nil {
			t.Fatalf("card has no label: %v; %s", err, evalString(t, ctx, `document.querySelector('#cards .card[data-id="manual"]')?.innerText || ''`))
		}
		label := text(t, ctx, `#cards .card[data-id="manual"] .card-label`)
		rows := evalStrings(t, ctx, `[...document.querySelectorAll('#cards .card[data-id="manual"] .row-value')].map(n => n.textContent)`)
		for _, row := range rows {
			if strings.Contains(row, `{"en"`) {
				t.Fatalf("%s row %q still has raw localized text", lang.id, row)
			}
		}
		if lang.id == "zh-CN" && label == lang.empty {
			if len(rows) != 0 {
				t.Fatalf("empty card has rows %q", rows)
			}
		} else if lang.id == "zh-CN" && (!strings.Contains(label, lang.started) || len(rows) == 0 || !allHave(rows, lang.up)) {
			t.Fatalf("label %q rows %q", label, rows)
		} else if lang.id == "en" && label != lang.empty && !allHave(rows, lang.up) {
			t.Fatalf("label %q rows %q", label, rows)
		}
		for _, width := range []int64{1280, 520} {
			do(t, ctx, chromedp.EmulateViewport(width, 800))
			if !evalBool(t, ctx, `document.documentElement.scrollWidth <= innerWidth`) {
				t.Fatalf("%s overview overflows at %d", lang.id, width)
			}
		}
	}

	do(t, ctx, chromedp.EmulateViewport(1280, 800))
	clickOnScreen(t, ctx, `#nav .nav-item[data-id="manual"]`)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const view = document.querySelector('#service .detail-view');
		if (!view || view.getAttribute('aria-busy') === 'true') return false;
		return !!view.querySelector('.detail-item, .empty, [role="alert"]');
	})()`, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
		t.Fatalf("detail did not open: %v; hash %s; %s", err, evalString(t, ctx, `location.hash`), evalString(t, ctx, `(document.querySelector('#service')?.innerText || '').slice(0, 400)`))
	}
	if alert := evalString(t, ctx, `document.querySelector('#service [role="alert"]')?.textContent || ''`); alert != "" {
		t.Fatalf("detail says %q", alert)
	}
	for _, lang := range []struct{ id, empty, up, dir, cmd string }{
		{"en", "No items.", " · up ", "Directory", "Command"},
		{"zh-CN", "没有条目。", "已运行", "目录", "命令"},
	} {
		do(t, ctx, chromedp.EmulateViewport(1280, 800))
		clickOnScreen(t, ctx, `button[data-lang="`+lang.id+`"]`)
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.lang === '`+lang.id+`'`, chromedp.WithPollingTimeout(15*time.Second))); err != nil {
			t.Fatalf("detail language %s: %v; page says %s", lang.id, err, evalString(t, ctx, `document.documentElement.lang`))
		}
		body := text(t, ctx, `#service .detail-view`)
		if strings.Contains(body, `{"en"`) {
			t.Fatalf("%s detail shows raw localized text: %s", lang.id, body)
		}
		if !strings.Contains(body, lang.empty) && !strings.Contains(body, lang.up) {
			t.Fatalf("%s detail %q", lang.id, body)
		}
		if strings.Contains(body, lang.up) && (!strings.Contains(body, lang.dir) || !strings.Contains(body, lang.cmd)) {
			t.Fatalf("%s detail has no directory or command: %s", lang.id, body)
		}
		for _, width := range []int64{1280, 520} {
			do(t, ctx, chromedp.EmulateViewport(width, 800))
			if !evalBool(t, ctx, `document.documentElement.scrollWidth <= innerWidth`) {
				t.Fatalf("%s detail overflows at %d", lang.id, width)
			}
		}
	}
}

// clickOnScreen clicks sel once it is the element under the pointer. A sidebar
// control stays inert for a turn after the window leaves the drawer width.
func clickOnScreen(t *testing.T, ctx context.Context, sel string) {
	t.Helper()
	wait := `(() => {
		const el = document.querySelector(` + strconv.Quote(sel) + `);
		if (!el || el.closest('[inert]')) return false;
		const r = el.getBoundingClientRect();
		if (r.width < 1 || r.height < 1 || r.bottom <= 0 || r.right <= 0 || r.top >= innerHeight || r.left >= innerWidth) return false;
		const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
		return !!hit && (hit === el || el.contains(hit));
	})()`
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](wait, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("%s is not on screen: %v", sel, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(sel)))
}

func evalString(t *testing.T, ctx context.Context, js string) string {
	t.Helper()
	s, err := chromedp.Run(ctx, chromedp.Evaluate[string](js))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func evalStrings(t *testing.T, ctx context.Context, js string) []string {
	t.Helper()
	rows, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](js))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func evalBool(t *testing.T, ctx context.Context, js string) bool {
	t.Helper()
	ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// manualListen is the re-executed test binary. It listens on 127.0.0.1:0 and
// writes "addr\npid\n" so the test can stop only that process.
func manualListen(args []string) {
	if len(args) < 3 {
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	body := ln.Addr().String() + "\n" + strconv.Itoa(os.Getpid()) + "\n"
	if err := os.WriteFile(args[2], []byte(body), 0o644); err != nil {
		os.Exit(1)
	}
	_, _ = ln.Accept()
}

func TestStopOnlyTheServiceThisTestStarted(t *testing.T) {
	pid, port := listenForManualStop(t)
	dir := t.TempDir()
	url := startConsole(t, dir)
	cliCommand(t, 0, "plugin", "install", "--id", "manual", "--url", url)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(url+"#/manual"))
	mark := `(() => {
		const view = document.querySelector('#service .detail-view');
		if (!view || view.getAttribute('aria-busy') === 'true') return '';
		const wantPort = ` + strconv.Quote(port) + `;
		const wantPID = ` + strconv.Quote(strconv.Itoa(pid)) + `;
		const field = (item, name) => {
			const dts = [...item.querySelectorAll('dt')];
			const dds = [...item.querySelectorAll('dd')];
			const i = dts.findIndex(dt => dt.textContent.trim() === name);
			return i >= 0 && dds[i] ? dds[i].textContent.trim() : '';
		};
		const item = [...document.querySelectorAll('#service .detail-item')].find(el => field(el, 'Ports').split(',').map(s => s.trim()).includes(wantPort));
		if (!item) return '';
		if (field(item, 'PID') !== wantPID) return 'pid:' + field(item, 'PID');
		const directory = field(item, 'Directory');
		const command = field(item, 'Command');
		if (!directory || directory.startsWith('Could not') || !(directory.startsWith('/') || /^[A-Za-z]:\\/.test(directory))) return 'dir:' + directory;
		if (!command || command.startsWith('Could not')) return 'cmd:' + command;
		const btn = [...item.querySelectorAll('.btn.action')].find(b => b.textContent.trim() === 'Stop');
		if (!btn) return 'no-stop';
		btn.id = 'manual-stop-under-test';
		btn.scrollIntoView({block: 'center'});
		return 'ready';
	})()`
	deadline := time.Now().Add(20 * time.Second)
	var state string
	for time.Now().Before(deadline) {
		got, err := chromedp.Run(ctx, chromedp.Evaluate[string](mark))
		if err == nil && got == "ready" {
			state = got
			break
		}
		if err == nil {
			state = got
		}
		reload, reloadErr := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => { const b = document.querySelector('#service-reload'); if (!b || b.disabled || b.hidden) return false; const r = b.getBoundingClientRect(); return r.width > 1 && r.bottom > 0 && r.top < innerHeight; })()`))
		if reloadErr == nil && reload {
			clickOnScreen(t, ctx, `#service-reload`)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if state != "ready" {
		t.Fatalf("listener pid %d port %s was not ready (%q); page: %s", pid, port, state, evalString(t, ctx, `(document.querySelector('#service')?.innerText || '').slice(0, 800)`))
	}
	clickOnScreen(t, ctx, `#manual-stop-under-test`)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#manual-stop-under-test')?.classList.contains('armed')`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("stop did not arm: %v; %s", err, evalString(t, ctx, `document.querySelector('#manual-stop-under-test')?.textContent || ''`))
	}
	time.Sleep(700 * time.Millisecond)
	clickOnScreen(t, ctx, `#manual-stop-under-test`)
	gone := `(() => {
		const view = document.querySelector('#service .detail-view');
		if (!view || view.getAttribute('aria-busy') === 'true') return false;
		const wantPort = ` + strconv.Quote(port) + `;
		const wantPID = ` + strconv.Quote(strconv.Itoa(pid)) + `;
		return ![...document.querySelectorAll('#service .detail-item')].some(el => {
			const dts = [...el.querySelectorAll('dt')];
			const dds = [...el.querySelectorAll('dd')];
			const at = (name) => { const i = dts.findIndex(dt => dt.textContent.trim() === name); return i >= 0 && dds[i] ? dds[i].textContent.trim() : ''; };
			return at('PID') === wantPID || at('Ports').split(',').map(s => s.trim()).includes(wantPort);
		});
	})()`
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](gone, chromedp.WithPollingTimeout(20*time.Second))); err != nil {
		t.Fatalf("row remained: %v; note %q; page %s", err, evalString(t, ctx, `document.querySelector('#service .detail-note')?.textContent || ''`), evalString(t, ctx, `(document.querySelector('#service')?.innerText || '').slice(0, 800)`))
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !processGone(pid) {
		time.Sleep(50 * time.Millisecond)
	}
	if !processGone(pid) {
		ps, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,stat=,command=").CombinedOutput()
		conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 200*time.Millisecond)
		if conn != nil {
			conn.Close()
		}
		t.Fatalf("pid %d is still running after the row disappeared; listening %v; ps %q; note %q", pid, dialErr == nil, strings.TrimSpace(string(ps)), evalString(t, ctx, `document.querySelector('#service .detail-note')?.textContent || ''`))
	}
}

func listenForManualStop(t *testing.T) (int, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("console integration test")
	}
	file := filepath.Join(t.TempDir(), "addr")
	cmd := startManualShell(t, file)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	t.Cleanup(func() {
		if pid > 1 {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
		stopManualShell(cmd)
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(file)
		if err == nil && strings.Count(string(b), "\n") >= 2 {
			text = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if text == "" {
		t.Fatal("listener did not start")
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	n, err := strconv.Atoi(lines[1])
	if err != nil || n <= 1 {
		t.Fatalf("pid %q", lines[1])
	}
	_, port, err := net.SplitHostPort(lines[0])
	if err != nil || port == "" {
		t.Fatalf("addr %q", lines[0])
	}
	pid = n
	return pid, port
}

func allHave(rows []string, part string) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if !strings.Contains(row, part) {
			return false
		}
	}
	return true
}
