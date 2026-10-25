package e2e

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestTheOverviewAndThePluginsPageFillAWideWindow(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		writePlugin(t, dir, id, fmt.Sprintf("schema: 1\nname: %s\npage: {link: 'https://%s.example/'}\n", id, id))
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	for _, page := range []struct{ hash, section, content string }{
		{"", "#overview", "#cards"},
		{"#/.plugins", "#catalog", "#catalog-installed .catalog-entry"},
	} {
		do(t, ctx, chromedp.Navigate(url+page.hash), chromedp.WaitVisible(chromedp.CSS(page.content)))
		for _, width := range []int64{1280, 1965, 2964} {
			do(t, ctx, chromedp.EmulateViewport(width, 900))
			// The space right of the heading and the content matches the page's
			// own margin on the left, wherever the window ends.
			got, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](fmt.Sprintf(`(() => {
 const main = document.querySelector('main').getBoundingClientRect(), right = main.left + document.querySelector('main').clientWidth;
 return [%q, %q].map(s => document.querySelector(s).getBoundingClientRect()).flatMap(r => [r.left - main.left, right - r.right]);
 })()`, page.section+" .overview-head", page.content)))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < len(got); i += 2 {
				if left, right := got[i], got[i+1]; right-left > 1 || left-right > 1 {
					t.Errorf("%s at %dpx: %.0fpx on the left but %.0fpx on the right", page.section, width, left, right)
				}
			}
		}
	}
}
