package e2e

import (
	"context"
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
	if n := evalString(t, ctx, `String(document.querySelectorAll('#service .btn.action').length)`); n != "0" {
		t.Fatalf("%s action buttons", n)
	}
	for _, lang := range []struct{ id, empty, up string }{
		{"en", "No items.", " · up "},
		{"zh-CN", "没有条目。", "已运行"},
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
