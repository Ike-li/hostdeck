package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestCompactDetailsKeepStateAndActionsTogetherWithoutChangingOtherDetails(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	item := `"items":[{"id":"long","name":"A very long background job name 中文 keeps wrapping inside its own row","status":"error","value":"not running · exit 78","fields":[{"name":{"en":"Command","zh-CN":"命令"},"value":"` + strings.Repeat("/a-very-long-path", 30) + `"}],"log":"` + strings.Repeat("log line\\n", 40) + `","actions":[{"id":"start","label":{"en":"Run","zh-CN":"运行"}}]}]`
	for id, printed := range map[string]string{"compact": `{"presentation":"compact",` + item + `}`, "ordinary": `{` + item + `}`} {
		writePlugin(t, dir, id, fmt.Sprintf("schema: 1\nname: %s\ndetail:\n  exec: {command: [%q, fake-plugin, detail-json, '%s']}\nactions:\n  exec: {command: [%q, fake-plugin, action]}\n", id, exe, printed, exe))
	}
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/compact"), chromedp.WaitVisible(chromedp.CSS(`.detail-item`)))
			for _, width := range []int64{1280, 768, 520} {
				for _, theme := range []string{"light", "dark"} {
					do(t, ctx, chromedp.EmulateViewport(width, 800))
					if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: theme}}}); err != nil {
						t.Fatal(err)
					}
					ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
    const v=document.querySelector('.detail-view'), row=v.querySelector('.detail-item'), head=row.querySelector('.detail-head'), action=row.querySelector('.action'), log=row.querySelector('.detail-log');
    return head.contains(action) && head.contains(row.querySelector('.detail-status')) && getComputedStyle(row).paddingTop==='16px' && action.getBoundingClientRect().height>=44 && !log.open && v.scrollWidth<=v.clientWidth+1 && document.documentElement.scrollWidth<=innerWidth;
   })()`))
					if err != nil || !ok {
						t.Fatalf("%s %d %s: compact row/action/overflow %v %v", lang, width, theme, ok, err)
					}
				}
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-log summary`)))
			ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`getComputedStyle(document.querySelector('.detail-log pre')).maxHeight==='240px'`))
			if err != nil || !ok {
				t.Fatalf("bounded log: %v %v", ok, err)
			}
			do(t, ctx, chromedp.Navigate(url+"#/ordinary"), chromedp.WaitVisible(chromedp.CSS(`.detail-view:not([hidden]) .detail-item`)))
			ok, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {const v=document.querySelector('.detail-view:not([hidden])'); return !v.querySelector('.detail-head .action') && getComputedStyle(v.querySelector('.detail-value')).fontSize==='24px'})()`))
			if err != nil || !ok {
				t.Fatalf("ordinary layout changed: %v %v", ok, err)
			}
		})
	}
}
