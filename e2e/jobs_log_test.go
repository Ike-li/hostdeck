package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestAJobLogScrollsAndARunSaysWhatTheJobIsDoing(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writePlugin(t, dir, "jobs", fmt.Sprintf(`schema: 1
name: {en: Background jobs, zh-CN: 后台任务}
detail:
  exec: {command: [%q, fake-plugin, jobs-log]}
actions:
  exec: {command: [%q, fake-plugin, jobs-log-action]}
`, exe, exe))
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/jobs"), chromedp.WaitVisible(chromedp.CSS(`.detail-item`)))
			collapsed, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
				const log = document.querySelector('.detail-log');
				return !!log && !log.open && !log.querySelector('pre').textContent.includes('END');
			})()`))
			if err != nil || !collapsed {
				t.Fatalf("log starts open or already filled: %v %v", collapsed, err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-log summary`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-log pre')?.textContent.includes('END')`)); err != nil {
				t.Fatal(err)
			}
			for _, width := range []int64{1280, 520} {
				do(t, ctx, chromedp.EmulateViewport(width, 800))
				ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
					const pre = document.querySelector('.detail-log pre');
					return !!pre && getComputedStyle(pre).maxHeight === '480px' && pre.scrollHeight > pre.clientHeight + 1;
				})()`))
				if err != nil || !ok {
					t.Fatalf("%s %d: log does not scroll inside 480px: %v %v", lang, width, ok, err)
				}
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-log summary`)))
			const button = `.detail-item button.action`
			do(t, ctx, chromedp.Click(chromedp.CSS(button)))
			wantConfirm := "Confirm Run?"
			wantNote := "Ran. Not running, exit 78."
			if lang == "zh-CN" {
				wantConfirm = "确认运行？"
				wantNote = "已运行。未在运行，退出码 78。"
			}
			if got := text(t, ctx, button); got != wantConfirm {
				t.Fatalf("button %q, want %q", got, wantConfirm)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(button)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`(() => {
				const note = document.querySelector('.detail-note');
				const log = document.querySelector('.detail-log');
				return note && !note.hidden && note.textContent === %q && log && log.open && log.querySelector('pre')?.textContent.includes('END');
			})()`, wantNote))); err != nil {
				t.Fatal(err)
			}
		})
	}
}
