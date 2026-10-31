package e2e

import (
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// The narrow drawer slides over the page instead of jumping, and it stops
// there on its own.
//
// What the browser computes is what is asserted, rather than the frames in
// between: a headless browser is free to keep its animation clock still, and a
// test that waits for a moving pixel asks for a frame the runner may never
// draw. The two tests together say the whole thing - one duration when motion
// is allowed, none when it is not, and the drawer ending in the same place.
func TestTheDrawerSlidesAndStopsWhenMotionIsAllowed(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	// The page is asked to allow motion, so the assertion below cannot pass
	// because the runner happens to prefer none.
	if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
		Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}},
	}); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.EmulateViewport(520, 700), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#menu`)), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="a"]`)))

	check := func(js string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", js, got, err)
		}
	}
	check(`matchMedia('(prefers-reduced-motion: reduce)').matches === false`)

	do(t, ctx, chromedp.Click(chromedp.CSS(`#menu`)))
	// The drawer carries the duration only once the menu has opened it, which
	// is also the only moment it matters.
	check(`(() => { const s = getComputedStyle(document.querySelector('.side-menu'));
 return s.transitionProperty === 'transform' && s.transitionDuration === '0.2s'; })()`)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`document.querySelector('#nav').getBoundingClientRect().right > 1`,
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
	// The initial route closes the drawer after asynchronous startup reads.
	// A visible menu alone can precede that route; wait for its first reading
	// before clicking, keeping the no-wait assertion after the click intact.
	do(t, ctx, chromedp.EmulateViewport(520, 700), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#menu`)), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="a"]`)))

	check := func(js string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", js, got, err)
		}
	}
	check(`matchMedia('(prefers-reduced-motion: reduce)').matches === true`)
	check(`getComputedStyle(document.querySelector('.side-menu')).transitionDuration === '0s'`)

	do(t, ctx, chromedp.Click(chromedp.CSS(`#menu`)))
	// No waiting at all: with less motion the list is over the page already.
	check(`document.querySelector('#nav').getBoundingClientRect().right > 1`)
	// Nor does it arrive late and stay: the drawer is where it belongs, and
	// a quarter of a second later it has not moved on.
	time.Sleep(250 * time.Millisecond)
	check(`document.querySelector('#nav').getBoundingClientRect().right > 1`)
}
