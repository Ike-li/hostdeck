package e2e

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// Headings and strong words are set in the console's semibold, 600, on the
// overview, on the plugins page with its help open and in the editor. None
// falls back to the browser's bold, which is heavier than anything the
// console draws but its brand mark.
func TestHeadingsAndStrongWordsKeepToTheConsolesSemibold(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "proxy", fmt.Sprintf(proxyPlugin, databaseService(t)))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	type weights struct {
		Seen int
		Off  []string
	}
	check := func(view string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[weights](`(() => {
 const shown = [...document.querySelectorAll('h1, h2, h3, h4, strong, b')].filter(e => e.getClientRects().length);
 const off = shown.filter(e => getComputedStyle(e).fontWeight !== '600')
   .map(e => e.tagName.toLowerCase() + (e.id ? '#' + e.id : '') + (e.className ? '.' + e.className : '') + ' ' + getComputedStyle(e).fontWeight);
 return {seen: shown.length, off};
 })()`))
		if err != nil {
			t.Fatal(err)
		}
		if got.Seen < 2 || len(got.Off) > 0 {
			t.Errorf("%s: %d headings and strong words shown, these are not 600: %v", view, got.Seen, got.Off)
		}
	}

	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.add-card strong`)))
	check("the overview")
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"),
		chromedp.Click(chromedp.CSS(`#connection-help > summary`)),
		chromedp.WaitVisible(chromedp.CSS(`#help-pages-title`)))
	check("the plugins page with its help open")
	do(t, ctx, chromedp.Click(chromedp.CSS(`#catalog .catalog-item[data-id="proxy"] .page-edit-open`)),
		chromedp.WaitVisible(chromedp.CSS(`#editor-title`)))
	check("the editor")
}
