package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestListeningPortsPluginShowsCountAndDetailRows(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	const add = `#catalog-available .catalog-item[data-id="listeners"] .btn`
	do(t, ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(add)),
		chromedp.Click(chromedp.CSS(add)),
		chromedp.Click(chromedp.CSS(add)),
		chromedp.WaitVisible(chromedp.CSS(`#nav .nav-item[data-id="listeners"]`)),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="listeners"]`)),
	)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const card = document.querySelector('#cards .card[data-id="listeners"]');
		if (!card || card.querySelector('.card-pending')) return false;
		return !!(card.querySelector('.card-value') || card.querySelector('.card-error'));
	})()`, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
		t.Fatal(err)
	}
	errText := evalString(t, ctx, `document.querySelector('#cards .card[data-id="listeners"] .card-error')?.textContent || ''`)
	value := evalString(t, ctx, `document.querySelector('#cards .card[data-id="listeners"] .card-value')?.textContent || ''`)
	if errText != "" && value != "" {
		t.Fatalf("card shows both error %q and value %q", errText, value)
	}
	if errText == "" && value == "" {
		t.Fatal("card has neither a count nor a bilingual error")
	}
	if errText != "" {
		if !strings.Contains(errText, "listening") && !strings.Contains(errText, "监听") {
			t.Fatalf("error wording %q", errText)
		}
		return
	}

	clickOnScreen(t, ctx, `#nav .nav-item[data-id="listeners"]`)
	do(t, ctx, chromedp.WaitVisible(chromedp.CSS(`#view .detail-item, #view .detail-search, #view .empty`)))
	// A machine with any TCP listener should render searchable rows.
	hasSearch := evalString(t, ctx, `document.querySelector('#view .detail-search') ? 'yes' : ''`)
	rows := evalStrings(t, ctx, `[...document.querySelectorAll('#view .detail-item .detail-name')].map(n => n.textContent)`)
	if hasSearch == "" && len(rows) == 0 {
		t.Fatal("detail has neither search nor rows")
	}
	body := evalString(t, ctx, `document.querySelector('#view')?.innerText || ''`)
	if !strings.Contains(body, "Port") && !strings.Contains(body, "端口") && len(rows) > 0 {
		// Compact fields use dt/dd; Port label should appear when a row is expanded/visible.
		if !strings.Contains(body, "127.0.0.1") && !strings.Contains(body, "0.0.0.0") && !strings.Contains(body, "::") {
			t.Fatalf("detail missing bind address markers: %s", body)
		}
	}
}
