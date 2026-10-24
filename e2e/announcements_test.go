package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// What the console did last is said once, and with the urgency it deserves:
// a result is a status, a failure is an alert.
func TestEveryResultIsAnnouncedOnceWithTheUrgencyItDeserves(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "site", "schema: 1\nname: Site\npage: {link: 'http://127.0.0.1:9/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	const button = `#catalog-installed .catalog-item[data-id="site"] .catalog-toggle`

	// Disable sits in the row's More actions panel, as it does for a reader.
	do(t, ctx,
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item[data-id="site"] .more-trigger`)),
		chromedp.Click(chromedp.CSS(`#catalog-installed .catalog-item[data-id="site"] .more-trigger`)),
		chromedp.WaitVisible(chromedp.CSS(button)),
	)
	// Disable needs two separate clicks: the first only arms it.
	do(t, ctx, chromedp.Click(chromedp.CSS(button)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`document.querySelector('#catalog .catalog-toggle')?.textContent === 'Click again to disable'`,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("the first click did not arm the button: %v; it says %q", err, text(t, ctx, "#catalog .catalog-toggle"))
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(button)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const note = document.querySelector('#catalog-note');
 return !note.hidden && note.textContent.trim().length > 0 && note.getAttribute('role') === 'status';
})()`, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatalf("a result that went well was not said as a status: %v; the note says %q", err, text(t, ctx, "#catalog-note"))
	}

	// An address that is not on this machine is refused by the console, and
	// the note becomes an alert without leaving the page.
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => {
 document.querySelector('#page-address').value = 'https://example.com/notes';
 document.querySelector('#page-form button').click();
})()`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const note = document.querySelector('#catalog-note');
 return !note.hidden && note.textContent.includes('Only pages on this machine') && note.getAttribute('role') === 'alert';
})()`, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatalf("a failure was not said as an alert: %v; the note says %q", err, text(t, ctx, "#catalog-note"))
	}
}

// The overview is rebuilt every two seconds while a card is still being read.
// What it says about itself must not be said again each time.
func TestTheCardCountIsAnnouncedOnceAndNotOnEveryRebuild(t *testing.T) {
	var healthy atomic.Bool
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"database":"ready"}`)
	}))
	defer svc.Close()
	dir := t.TempDir()
	writePlugin(t, dir, "demo", fmt.Sprintf(demoPlugin, svc.URL))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card-error`)))
	// The first announcement is what the reader is told; what follows is what
	// the rebuilds must not add to.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`document.querySelector('#live-region').textContent.trim().length > 0`,
		chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatalf("the overview never said how many cards it has: %v", err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => {
 const region = document.getElementById('live-region');
 window.__live = 0;
 new MutationObserver((records) => { window.__live += records.length; })
   .observe(region, { childList: true, characterData: true, subtree: true });
})()`)); err != nil {
		t.Fatal(err)
	}

	// Three rebuilds at least: a card that failed is asked again every 2 s.
	time.Sleep(7 * time.Second)
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const region = document.getElementById('live-region');
 const box = region.getBoundingClientRect();
 return window.__live === 0
   && region.textContent === 'Cards on the overview: 1.'
   && !document.querySelector('#cards').contains(region)
   && getComputedStyle(region).position === 'absolute'
   && box.width <= 1.5 && box.height <= 1.5
   && document.documentElement.scrollWidth <= innerWidth;
})()`))
	if err != nil || !got {
		t.Fatalf("the overview said itself again while the grid was rebuilt: %v %v; it says %q, %d more times",
			err, got, text(t, ctx, "#live-region"), liveWrites(t, ctx))
	}
}

func liveWrites(t *testing.T, ctx context.Context) int {
	t.Helper()
	n, err := chromedp.Run(ctx, chromedp.Evaluate[int](`window.__live`))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A long plugins page answers a search by saying how many entries matched,
// so the filter is not silent for someone who cannot see the list shrink.
func TestASearchAnnouncesHowManyEntriesMatch(t *testing.T) {
	dir := t.TempDir()
	for i := range 9 {
		name := fmt.Sprintf("Widget %d", i)
		if i == 0 {
			name = "Target"
		}
		writePlugin(t, dir, fmt.Sprintf("n%d", i), fmt.Sprintf("schema: 1\nname: %s\npage: {link: 'https://n%d.example/'}\n", name, i))
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-search`)))

	search := func(term, want string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf(`(() => {
 const s = document.querySelector('#catalog-search');
 s.value = %q;
 s.dispatchEvent(new Event('input', { bubbles: true }));
})()`, term))); err != nil {
			t.Fatal(err)
		}
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(
			`document.querySelector('#live-region').textContent === %q`, want),
			chromedp.WithPollingTimeout(5*time.Second))); err != nil {
			t.Fatalf("searching %q did not say %q: %v; the page says %q", term, want, err, text(t, ctx, "#live-region"))
		}
	}
	search("Target", "Matches: 1.")
	search("Widget", "Matches: 8.")

	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.querySelector('#catalog-no-results').getAttribute('role') === 'status'`))
	if err != nil || !got {
		t.Fatalf("no results is said politely: %v %v", got, err)
	}
}
