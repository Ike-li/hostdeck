package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const groupedDetail = `{"groups":[{"id":"running","label":{"en":"Running","zh-CN":"运行中"}},{"id":"idle","label":{"en":"Not running","zh-CN":"未运行"}},{"id":"attention","label":{"en":"Needs attention","zh-CN":"需要处理"}}],"items":[{"id":"zeta","name":"Zeta service","status":"ok","group":"running"},{"id":"alpha","name":"Alpha service","status":"ok","group":"idle"},{"id":"failed","name":"Failed service 中文","status":"error","group":"attention","actions":[{"id":"start","label":{"en":"Run","zh-CN":"运行"}}]}]}`

func TestDetailSearchAndGroupsKeepTheirStateAcrossReadingsAndLanguages(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	t.Setenv("HOSTDECK_E2E_RAN", ran)
	writePlugin(t, dir, "grouped", fmt.Sprintf("schema: 1\nname: Jobs\ndetail:\n  exec: {command: [%q, fake-plugin, detail-json, '%s']}\nactions:\n  exec: {command: [%q, fake-plugin, action]}\n", exe, groupedDetail, exe))
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			os.Remove(ran)
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/grouped"))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!!document.querySelector('.detail-search input')`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-search input`)), chromedp.SendKeys(chromedp.CSS(`.detail-search input`), " SERVICE "))
			check := func(js string) {
				t.Helper()
				got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
				if err != nil || !got {
					t.Fatalf("%s: %v %v", js, got, err)
				}
			}
			check(`document.activeElement.matches('.detail-search input') && document.querySelectorAll('.detail-item').length === 3`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-groups [data-group="attention"]`)))
			check(`document.querySelectorAll('.detail-item').length === 1 && document.querySelector('.detail-name').textContent.includes('Failed')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-view').getAttribute('aria-busy') === 'false'`)); err != nil {
				t.Fatal(err)
			}
			check(`document.querySelector('.detail-search input').value === ' SERVICE ' && document.querySelector('[data-group="attention"]').getAttribute('aria-pressed') === 'true' && document.querySelectorAll('.detail-item').length === 1`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-item .action`)), chromedp.Click(chromedp.CSS(`.detail-item .action`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!!document.querySelector('.detail-note:not([hidden])') && !document.querySelector('.detail-item .action').disabled`, chromedp.WithPollingTimeout(6*time.Second))); err != nil {
				t.Fatal(err)
			}
			check(`document.querySelector('.detail-search input').value === ' SERVICE ' && document.querySelector('[data-group="attention"]').getAttribute('aria-pressed') === 'true' && document.querySelectorAll('.detail-item').length === 1`)
			if got, err := os.ReadFile(ran); err != nil || string(got) != "start failed" {
				t.Fatalf("action event: %q %v", got, err)
			}
			next := "zh-CN"
			if lang == "zh-CN" {
				next = "en"
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`[data-lang="`+next+`"]`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.lang === '`+next+`'`)); err != nil {
				t.Fatal(err)
			}
			check(`document.querySelector('.detail-search input').value === ' SERVICE ' && document.querySelectorAll('.detail-item').length === 1`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-search .detail-clear`)), chromedp.Click(chromedp.CSS(`.detail-groups [data-group="all"]`)))
			check(`document.querySelectorAll('.detail-item').length === 3 && document.querySelector('.detail-name').textContent.includes('Failed')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-search input`)), chromedp.SendKeys(chromedp.CSS(`.detail-search input`), "中文"))
			check(`document.querySelectorAll('.detail-item').length === 1 && document.querySelector('[data-group="running"]').textContent.includes('(0)')`)
			do(t, ctx, chromedp.SendKeys(chromedp.CSS(`.detail-search input`), "absent"))
			check(`document.querySelectorAll('.detail-item').length === 0 && !!document.querySelector('.detail-list .empty') && document.querySelector('#live-region').textContent.includes('0')`)
		})
	}
}
