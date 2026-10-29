package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestTheProcessesPluginInstallsSearchesAndRefreshesWithoutProcessActions(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	cliCommand(t, 0, "plugin", "install", "--id", "processes", "--url", url)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {const card=document.querySelector('#cards [data-id="processes"]');return card && Number(card.querySelector('.card-value')?.textContent)>0})()`, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Navigate(url+"#/processes"))
			wait := func() {
				t.Helper()
				if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-view')?.getAttribute('aria-busy')==='false' && !!document.querySelector('.detail-search input')`, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
					t.Fatal(err)
				}
			}
			wait()
			if got := evalString(t, ctx, `document.querySelector('#service .detail-item .action') ? 'action' : ''`); got != "" {
				t.Fatal("read-only process list offers an action")
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-groups [data-group="hostdeck"]`)))
			if got := evalString(t, ctx, `String(document.querySelectorAll('#service .detail-item').length)`); got != "1" {
				t.Fatalf("console group has %s items", got)
			}
			name := evalString(t, ctx, `document.querySelector('.detail-item .detail-name').textContent`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-search input`)), chromedp.SendKeys(chromedp.CSS(`.detail-search input`), name))
			if got := evalString(t, ctx, `String(document.querySelectorAll('#service .detail-item').length)`); got != "1" {
				t.Fatalf("name search has %s items", got)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-search .detail-clear`)))
			pid := evalString(t, ctx, `document.querySelector('.detail-item').dataset.item`)
			if pid == "" {
				t.Fatal("console row has no PID")
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-search input`)), chromedp.SendKeys(chromedp.CSS(`.detail-search input`), pid))
			if got := evalString(t, ctx, `String(document.querySelectorAll('#service .detail-item').length)`); got != "1" {
				t.Fatalf("PID search has %s items", got)
			}
			before := evalString(t, ctx, `String(performance.getEntriesByType('resource').filter(e=>e.name.includes('/api/detail/processes')).length)`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`performance.getEntriesByType('resource').filter(e=>e.name.includes('/api/detail/processes')).length > `+before, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
				t.Fatal(err)
			}
			wait()
			if got := evalString(t, ctx, `document.querySelector('.detail-search input').value`); got != pid {
				t.Fatalf("refresh lost search %q", got)
			}
			for _, theme := range []string{"light", "dark"} {
				if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: theme}}}); err != nil {
					t.Fatal(err)
				}
				for _, width := range []int{1280, 520} {
					do(t, ctx, chromedp.EmulateViewport(int64(width), 800))
					overflow, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.documentElement.scrollWidth > innerWidth || [...document.querySelectorAll('#service .detail-item')].some(n=>n.scrollWidth>n.clientWidth)`))
					if err != nil || overflow {
						t.Fatalf("%s %s %d overflow=%v err=%v", lang, theme, width, overflow, err)
					}
					body := evalString(t, ctx, `document.querySelector('#service').innerText`)
					newFields := []string{"Executable path", "Parent process", "Started", "Uptime", "CPU time", "Threads"}
					if lang == "zh-CN" {
						newFields = []string{"可执行文件路径", "父进程名称", "启动时间", "运行时长", "累计 CPU 时间", "线程数"}
					}
					for _, word := range append([]string{"PID", "CPU", "MiB", "UTC"}, newFields...) {
						if !strings.Contains(body, word) {
							t.Fatalf("missing %s: %s", word, body)
						}
					}
				}
			}
			do(t, ctx, chromedp.SendKeys(chromedp.CSS(`.detail-search input`), "no-such-process-"+fmt.Sprint(time.Now().UnixNano())))
			if got := evalString(t, ctx, `String(document.querySelectorAll('#service .detail-item').length)`); got != "0" {
				t.Fatalf("unmatched search has %s rows", got)
			}
		})
	}
}
