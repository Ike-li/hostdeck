package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestADetailWithoutAnItemNameShowsAnErrorInsteadOfAnEmptyHeading(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for id, item := range map[string]string{
		"missing": `{"id":"blank"}`,
		"empty":   `{"id":"blank","name":""}`,
	} {
		printed := `{"items":[{"id":"valid","name":"Valid"},` + item + `]}`
		writePlugin(t, dir, id, fmt.Sprintf("schema: 1\nname: Services\ndetail:\n  exec: {command: [%q, fake-plugin, detail-json, '%s']}\n", exe, printed))
	}
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			for _, id := range []string{"missing", "empty"} {
				const alert = `.detail-view:not([hidden]) .detail-list [role="alert"]`
				do(t, ctx, chromedp.Navigate(url+"#/"+id), chromedp.WaitVisible(chromedp.CSS(alert)))
				want := `The command printed something hostdeck cannot read: item "blank" has no name`
				if lang == "zh-CN" {
					want = `命令的输出 hostdeck 读不了：item "blank" has no name`
				}
				if got := text(t, ctx, alert); got != want {
					t.Errorf("%s: error %q, want %q", id, got, want)
				}
				clear, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelectorAll('.detail-item, .detail-name, .detail-actions').length === 0`))
				if err != nil || !clear {
					t.Fatalf("%s: invalid detail still shows items or actions: %v", id, err)
				}
			}
		})
	}
}
