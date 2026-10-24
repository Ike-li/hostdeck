package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A console with nothing in it says what to do next, both where the plugins
// would be listed and where the cards would be, and says it in the language
// the browser asked for.
func TestAnEmptySidebarAndAnEmptyOverviewEachSayWhatToDoNext(t *testing.T) {
	url := startConsole(t, t.TempDir())
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#empty`)))

	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const nav = document.querySelector('#nav-empty'), empty = document.querySelector('#empty');
 return !!nav && nav.checkVisibility() && nav.classList.contains('empty') && nav.textContent.trim().length > 0
   && document.querySelectorAll('#nav .nav-row').length === 0
   && empty.checkVisibility() && empty.classList.contains('empty') && empty.textContent.trim().length > 0
   && document.documentElement.scrollWidth <= innerWidth;
})()`))
	if err != nil || !got {
		t.Fatalf("an empty console should say what to do next in both lists: %v %v", got, err)
	}

	english := strings.TrimSpace(text(t, ctx, "#nav-empty"))
	do(t, ctx, chromedp.Click(chromedp.CSS(`[data-lang="zh-CN"]`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`document.documentElement.lang === 'zh-CN'`,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("the language never switched: %v", err)
	}
	if got := strings.TrimSpace(text(t, ctx, "#nav-empty")); got == "" || got == english {
		t.Errorf("the sidebar's empty state stayed %q in Chinese", got)
	}
}

// The overview is read once it is asked for. When that first reading never
// arrives, the overview still says so, instead of showing nothing at all.
func TestAFirstReadingThatNeverArrivesLeavesTheOverviewWithAnExplanation(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	// The plugins page first: the cards are not read until the overview is
	// asked for, so the reading below is the first one.
	do(t, ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(`#catalog h1`)),
	)
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`
		const realFetch = window.fetch;
		window.fetch = (url, opts) => String(url).startsWith('/api/cards')
			? Promise.reject(new Error('the console is unreachable'))
			: realFetch(url, opts);
	`)); err != nil {
		t.Fatal(err)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav-overview`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const empty = document.querySelector('#empty');
 return empty.checkVisibility() && empty.textContent.trim().length > 0
   && !empty.textContent.includes('No cards yet')
   && document.querySelector('#cards').getAttribute('aria-busy') === 'false'
   && document.querySelectorAll('#cards .card').length === 0;
})()`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("a first reading that failed left the overview with nothing to read: %v; the overview says %q", err, text(t, ctx, "#empty"))
	}
}
