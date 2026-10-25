package e2e

import (
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestTheLayoutSavedNoticeDisappearsAndTheNextChangeCanStillBeUndone(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)),
		chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .move-down`)), chromedp.WaitVisible(chromedp.CSS(`#layout-note`)))
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#layout-note').hidden && document.querySelector('#layout-undo').getClientRects().length === 0`, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatalf("the saved notice and undo should leave the page after eight idle seconds: %v", err)
	}
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="b"] .more-trigger`)), chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="b"] .move-down`)), chromedp.WaitVisible(chromedp.CSS(`#layout-note`)))
	savedLayout(t, dir, "sidebar", []string{"a", "b", "c"})
	do(t, ctx, chromedp.Focus(chromedp.CSS(`#layout-undo`)), chromedp.KeyEvent(kb.Enter))
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
	do(t, ctx, chromedp.WaitNotVisible(chromedp.CSS(`#layout-note`)))
}

func TestTheLayoutSavedNoticeWaitsWhileUndoHasFocusOrThePointerIsOverIt(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)))
	pauseTime(t, ctx)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .move-down`)), chromedp.WaitVisible(chromedp.CSS(`#layout-note`)), chromedp.Focus(chromedp.CSS(`#layout-undo`)))
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
	advanceTime(t, ctx, 9*time.Second)
	focused, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!document.querySelector('#layout-note').hidden && document.activeElement.id === 'layout-undo'`))
	if err != nil || !focused {
		t.Fatalf("the timeout must not remove a focused undo button: %v %v", focused, err)
	}
	point, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](`(() => { const r = document.querySelector('#layout-note').getBoundingClientRect(); return [r.x + r.width/2, r.y + r.height/2]; })()`))
	if err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, point[0], point[1]), chromedp.Focus(chromedp.CSS(`#refresh`)))
	advanceTime(t, ctx, 9*time.Second)
	hovered, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!document.querySelector('#layout-note').hidden && document.querySelector('#layout-note').matches(':hover')`))
	if err != nil || !hovered {
		t.Fatalf("the timeout must wait while the pointer is over the notice: %v %v", hovered, err)
	}
	do(t, ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, 20, 500))
	advanceTime(t, ctx, 7999*time.Millisecond)
	visible, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!document.querySelector('#layout-note').hidden`))
	if err != nil || !visible {
		t.Fatalf("leaving must start a fresh eight-second countdown: %v %v", visible, err)
	}
	advanceTime(t, ctx, time.Millisecond)
	hidden, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('#layout-note').hidden`))
	if err != nil || !hidden {
		t.Fatalf("the notice should disappear after eight idle seconds: %v %v", hidden, err)
	}
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
}
