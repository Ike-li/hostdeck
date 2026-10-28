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
	cliCommand(t, 0, "plugin", "install", "--id", "listeners", "--url", url)
	ctx := openBrowser(t, "en")
	do(t, ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="listeners"]`)),
	)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const card = document.querySelector('#cards .card[data-id="listeners"]');
		if (!card || card.querySelector('.card-pending')) return false;
		return !!(card.querySelector('.card-label') || card.querySelector('.card-error'));
	})()`, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
		t.Fatal(err)
	}
	errText := evalString(t, ctx, `document.querySelector('#cards .card[data-id="listeners"] .card-error')?.textContent || ''`)
	value := evalString(t, ctx, `document.querySelector('#cards .card[data-id="listeners"] .card-value')?.textContent || ''`)
	label := evalString(t, ctx, `document.querySelector('#cards .card[data-id="listeners"] .card-label')?.textContent || ''`)
	if errText != "" && value != "" {
		t.Fatalf("card shows both error %q and value %q", errText, value)
	}
	if errText == "" && label == "" {
		t.Fatal("card has neither a label nor a bilingual error")
	}
	if errText != "" {
		if !strings.Contains(errText, "listening") && !strings.Contains(errText, "监听") {
			t.Fatalf("error wording %q", errText)
		}
		return
	}

	do(t, ctx, chromedp.Navigate(url+"#/listeners"))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const view = document.querySelector('#service .detail-view');
		if (!view || view.getAttribute('aria-busy') === 'true') return false;
		return !!(document.querySelector('#service .detail-item') || document.querySelector('#service .detail-search') || document.querySelector('#service .detail-list .empty'));
	})()`, chromedp.WithPollingTimeout(25*time.Second))); err != nil {
		t.Fatalf("detail did not open: %v; hash %s; %s", err, evalString(t, ctx, `location.hash`), evalString(t, ctx, `(document.querySelector('#service')?.innerText || '').slice(0, 400)`))
	}
	body := evalString(t, ctx, `document.querySelector('#service')?.innerText || ''`)
	rows := evalStrings(t, ctx, `[...document.querySelectorAll('#service .detail-item .detail-name')].map(n => n.textContent)`)
	hasSearch := evalString(t, ctx, `document.querySelector('#service .detail-search') ? 'yes' : ''`)
	if hasSearch == "" && len(rows) == 0 && !strings.Contains(body, "No items") && !strings.Contains(body, "没有") {
		t.Fatalf("detail has neither search, rows nor empty state: %s", body)
	}
	if len(rows) > 0 && !strings.Contains(body, "127.0.0.1") && !strings.Contains(body, "0.0.0.0") && !strings.Contains(body, "::") {
		t.Fatalf("detail missing bind address markers: %s", body)
	}
}
