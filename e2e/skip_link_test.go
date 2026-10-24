package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Someone who reads the console with a keyboard reaches its content from the
// first Tab, and gets there without the address moving under them.
func TestTheFirstTabReachesALinkThatJumpsToTheContentWithoutMovingTheAddress(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card`)))
	do(t, ctx, chromedp.KeyEvent(kb.Tab))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`document.activeElement === document.querySelector('.skip-link')`,
		chromedp.WithPollingTimeout(2*time.Second))); err != nil {
		t.Fatalf("the first Tab did not reach the skip link: %v; focus is on %q", err, focused(t, ctx))
	}

	// It is a link to the content, it is the first thing on the page, and it
	// comes into view when focused without taking room when it is not.
	// It slides in, so the box is only where it belongs once the transition
	// has run; everything else about it is true at once.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const link = document.querySelector('.skip-link');
 const r = link.getBoundingClientRect();
 return link.tagName === 'A' && link.getAttribute('href') === '#main'
   && document.body.firstElementChild === link
   && getComputedStyle(link).position === 'fixed'
   && link.textContent.trim().length > 0
   && r.top >= 0 && r.bottom <= innerHeight && r.left >= 0 && r.right <= innerWidth
   && document.documentElement.scrollWidth <= innerWidth;
})()`, chromedp.WithPollingTimeout(2*time.Second))); err != nil {
		t.Fatalf("the skip link is not a visible link to the content: %v", err)
	}

	do(t, ctx, chromedp.KeyEvent(kb.Enter))
	// #main is not an address: read as one, the route would leave the page the
	// reader is on and show the overview.
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 return document.activeElement === document.querySelector('main')
   && location.hash !== '#main'
   && !document.querySelector('#overview').hidden
   && document.querySelector('#service').hidden;
})()`))
	if err != nil || !got {
		t.Fatalf("activating the link did not put the keyboard on the content: %v %v; hash %q", got, err, hash(t, ctx))
	}

	// A narrow window keeps the overview where it was: the link is out of the
	// flow, so it cannot push the heading down.
	do(t, ctx, chromedp.EmulateViewport(520, 700))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const top = document.querySelector('#overview h1').getBoundingClientRect().top;
 return top >= 40 && top <= 96;
})()`, chromedp.WithPollingTimeout(2*time.Second))); err != nil {
		t.Fatalf("the skip link pushed the heading out of the top bar: %v", err)
	}
}

func focused(t *testing.T, ctx context.Context) string {
	t.Helper()
	s, err := chromedp.Run(ctx, chromedp.Evaluate[string](
		`document.activeElement ? document.activeElement.tagName + (document.activeElement.id ? '#' + document.activeElement.id : '') : 'none'`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func hash(t *testing.T, ctx context.Context) string {
	t.Helper()
	s, err := chromedp.Run(ctx, chromedp.Evaluate[string](`location.hash`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
