package e2e

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestNavigationHasCompactRowsAndKeyboardAccessibleMoreActions(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 860), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-row[data-id="a"]`)))
	const row = `#nav .nav-row[data-id="a"]`
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const row = document.querySelector('#nav .nav-row[data-id="a"]');
 const trigger = row.querySelector('.more-trigger');
 return document.querySelector('.sidebar').getBoundingClientRect().width === 240 && row.getBoundingClientRect().height <= 52 && !!trigger && trigger.getBoundingClientRect().height >= 44 && row.querySelector('.move-down').getClientRects().length === 0;
 })()`))
	if err != nil || !got {
		t.Fatalf("want a 240px sidebar, compact rows, a visible 44px more button and hidden actions: %v %v", got, err)
	}
	do(t, ctx, chromedp.Focus(chromedp.CSS(row+` .more-trigger`)), chromedp.KeyEvent(kb.Enter), chromedp.WaitVisible(chromedp.CSS(row+` .move-down`)), chromedp.KeyEvent(kb.Tab))
	focused, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.matches('#nav .nav-row[data-id="a"] .move-down')`))
	if err != nil || !focused {
		t.Fatalf("Tab should reach the first enabled action: %v %v", focused, err)
	}
	do(t, ctx, chromedp.KeyEvent(kb.Escape))
	focused, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.matches('#nav .nav-row[data-id="a"] .more-trigger') && document.activeElement.getAttribute('aria-expanded') === 'false'`))
	if err != nil || !focused {
		t.Fatalf("Escape should close the panel and restore focus: %v %v", focused, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(row+` .more-trigger`)), chromedp.Click(chromedp.CSS(row+` .move-down`)))
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
}

func TestTheNarrowNavigationKeepsFocusInsideAndRestoresItWhenClosed(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(520, 700), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#menu`)))
	check := func(expression string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](expression))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", expression, got, err)
		}
	}
	check(`document.querySelector('.side-menu').inert`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#menu`)))
	check(`document.querySelector('main').inert && !document.querySelector('.side-menu').inert && !document.querySelector('#nav-backdrop').hidden`)
	do(t, ctx, chromedp.Focus(chromedp.CSS(`[data-lang="zh-CN"]`)), chromedp.KeyEvent(kb.Tab))
	check(`document.activeElement.matches('.brand')`)
	do(t, ctx, chromedp.KeyEvent(kb.Escape))
	check(`document.querySelector('.side-menu').inert && !document.querySelector('main').inert && document.activeElement.id === 'menu'`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#menu`)), chromedp.Click(chromedp.CSS(`#nav-backdrop`)))
	check(`document.querySelector('#menu').getAttribute('aria-expanded') === 'false'`)
	do(t, ctx, chromedp.EmulateViewport(1280, 860))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('.side-menu').inert && !document.querySelector('main').inert`)); err != nil {
		t.Fatal(err)
	}
}
