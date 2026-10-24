package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Every way into a page is inside a landmark, and the one that is open says
// so, so that a reader who jumps between landmarks knows where they are.
func TestEveryNavigationEntryIsInALandmarkAndSaysWhichPageIsOpen(t *testing.T) {
	dir := t.TempDir()
	// A card plugin: a link's entry opens a new tab, and the console stays
	// where it is.
	writePlugin(t, dir, "demo", fmt.Sprintf(demoPlugin, databaseService(t)))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card`)))

	check := func(what, js string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", what, got, err)
		}
	}
	// The overview, Pages & plugins and every sidebar entry are all named, and
	// all three pages name themselves from the heading they carry.
	check("the sidebar and its list are landmarks with names", `(() => {
 const menu = document.querySelector('.side-menu'), aside = document.querySelector('aside.sidebar');
 return menu.tagName === 'NAV' && (menu.getAttribute('aria-label') || '').trim().length > 0
   && (aside.getAttribute('aria-label') || '').trim().length > 0
   && document.querySelectorAll('main').length === 1;
})()`)
	// A page that is not open has no name to give yet, but it still points at
	// the heading that will name it.
	check("every page is named by its own heading", `(() => {
 return ['overview', 'catalog', 'service'].every((id) => {
   const section = document.getElementById(id);
   const named = document.getElementById(section.getAttribute('aria-labelledby') || '');
   return !!named && (section.hidden || named.textContent.trim().length > 0);
 });
})()`)
	check("the overview is the open page", `(() => {
 return document.querySelector('#nav-overview').getAttribute('aria-current') === 'page'
   && !document.querySelector('#nav-catalog').hasAttribute('aria-current')
   && [...document.querySelectorAll('#nav .nav-item')].every((a) => !a.hasAttribute('aria-current'));
})()`)

	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="demo"] .nav-item`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 return document.querySelector('#nav .nav-row[data-id="demo"] .nav-item').getAttribute('aria-current') === 'page'
   && !document.querySelector('#nav-overview').hasAttribute('aria-current');
})()`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("a plugin does not say it is the open page: %v", err)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav-catalog`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 return document.querySelector('#nav-catalog').getAttribute('aria-current') === 'page'
   && !document.querySelector('#nav-overview').hasAttribute('aria-current');
})()`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("the plugins page does not say it is the open page: %v", err)
	}
}

// The headings step one level at a time, and the service view names itself
// with a heading rather than with a span pretending to be one.
func TestTheHeadingLadderRunsFromOneH1WithoutSkippingALevel(t *testing.T) {
	dir := t.TempDir()
	// A card plugin, so that the console opens a page of its own for it:
	// a link opens in a new tab instead.
	writePlugin(t, dir, "demo", fmt.Sprintf(demoPlugin, databaseService(t)))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")

	do(t, ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item`)),
	)
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const levels = [...document.querySelectorAll('#catalog h1, #catalog h2, #catalog h3, #catalog h4')]
   .filter((e) => e.checkVisibility())
   .map((e) => Number(e.tagName[1]));
 for (let i = 1; i < levels.length; i++) {
   if (levels[i] > levels[i - 1] + 1) return false;
 }
 return levels[0] === 1 && levels.filter((l) => l === 1).length === 1
   && document.querySelector('#catalog-installed .catalog-item .card-name').tagName === 'H3'
   && document.querySelector('#catalog-available .catalog-item .card-name').tagName === 'H4';
})()`))
	if err != nil || !got {
		t.Fatalf("the plugins page does not step down one heading level at a time: %v %v", got, err)
	}

	do(t, ctx, chromedp.Navigate(url+"#/demo"), chromedp.WaitVisible(chromedp.CSS(`#service-name`)))
	got, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 return document.querySelector('#service-name').tagName === 'H1'
   && document.querySelector('#service').getAttribute('aria-labelledby') === 'service-name'
   && document.querySelectorAll('#service h1').length === 1;
})()`))
	if err != nil || !got {
		t.Fatalf("the service view does not name itself with a heading: %v %v", got, err)
	}
}

// A panel, an external link and a sample someone copies all say what they are.
func TestPopoversExternalLinksAndCodeSamplesSayWhatTheyAre(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(`#help-plugin-example`)),
	)
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const trigger = document.querySelector('#nav .nav-row .more-trigger');
 const sample = document.querySelector('#help-plugin-example');
 const external = document.querySelector('#nav .nav-item[data-id="a"]');
 return trigger.getAttribute('aria-haspopup') === 'true'
   && !!document.getElementById(trigger.getAttribute('aria-controls'))
   && (sample.getAttribute('aria-label') || '').trim().length > 0
   && external.getAttribute('target') === '_blank'
   && /new tab/i.test(external.getAttribute('aria-label') || '');
})()`))
	if err != nil || !got {
		t.Fatalf("a panel, an outside link or a sample is unnamed: %v %v", got, err)
	}

	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)))
	got, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const panel = document.querySelector('.action-panel[popover]');
 return !!panel && panel.getAttribute('role') === 'group'
   && (panel.getAttribute('aria-label') || '').trim().length > 0;
})()`))
	if err != nil || !got {
		t.Fatalf("an open action panel has no role or name: %v %v", got, err)
	}
}
