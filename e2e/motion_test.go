package e2e

import (
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// The narrow drawer slides over the page instead of jumping, and it stops
// there on its own.
func TestTheDrawerSlidesAndStopsWhenMotionIsAllowed(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(520, 700), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#menu`)))

	// A transition is an event, not a frame to catch, so the page records it.
	if got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 window.__moved = [];
 document.querySelector('.side-menu').addEventListener('transitionrun', (e) => window.__moved.push(e.propertyName));
 return true; })()`)); err != nil || !got {
		t.Fatalf("watching the drawer: %v %v", got, err)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`#menu`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`window.__moved.includes('transform')`,
		chromedp.WithPollingTimeout(2*time.Second))); err != nil {
		t.Fatalf("the drawer should slide the list in, not jump: %v", err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`document.querySelector('#nav').getBoundingClientRect().right > 1 && document.getAnimations().every(a => a.playState !== 'running')`,
		chromedp.WithPollingTimeout(2*time.Second))); err != nil {
		t.Fatalf("the drawer should stop over the page: %v", err)
	}
}

// Someone who asked their browser for less motion gets the drawer where it
// belongs at once, with nothing to wait for.
func TestTheDrawerArrivesAtOnceWhenTheBrowserAsksForLessMotion(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	// Asked before the first navigation, and the page is made to confirm it,
	// so a change in how the override applies cannot make what follows pass
	// for the wrong reason.
	if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
		Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
	}); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.EmulateViewport(520, 700), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#menu`)))

	check := func(js string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", js, got, err)
		}
	}
	check(`matchMedia('(prefers-reduced-motion: reduce)').matches === true`)
	check(`getComputedStyle(document.querySelector('.side-menu')).transitionDuration === '0s'`)

	if got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 window.__moved = [];
 document.querySelector('.side-menu').addEventListener('transitionrun', (e) => window.__moved.push(e.propertyName));
 return true; })()`)); err != nil || !got {
		t.Fatalf("watching the drawer: %v %v", got, err)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`#menu`)))
	// No waiting at all: with less motion the list is over the page already.
	check(`document.querySelector('#nav').getBoundingClientRect().right > 1`)
	time.Sleep(400 * time.Millisecond)
	check(`window.__moved.length === 0`)
}
