package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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

func TestOverviewActionsStayInTheirPanelWhileAReadingArrives(t *testing.T) {
	var healthy atomic.Bool
	var reads atomic.Int32
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
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
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const card = document.querySelector('#cards .card');
 const head = card.querySelector('.card-head');
 const more = card.querySelector('.more-trigger');
 return !!more && more.getBoundingClientRect().height >= 44 && getComputedStyle(head).paddingTop === '0px' && card.querySelector('.card-width').getClientRects().length === 0;
 })()`))
	if err != nil || !got {
		t.Fatalf("want a compact card with visible more and hidden width action: %v %v", got, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#cards .more-trigger`)), chromedp.Focus(chromedp.CSS(`#cards .hide`)))
	before := reads.Load()
	healthy.Store(true)
	for deadline := time.Now().Add(8 * time.Second); reads.Load() <= before && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	if reads.Load() <= before {
		t.Fatal("the overview did not retry the failed card")
	}
	// Waiting for the HTTP response to settle must not replace the focused action.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#cards').getAttribute('aria-busy') === 'false'`)); err != nil {
		t.Fatal(err)
	}
	got, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.matches('#cards .hide') && document.querySelector('#cards .more-trigger').getAttribute('aria-expanded') === 'true'`))
	if err != nil || !got {
		t.Fatalf("a new reading interrupted the open action panel: %v %v", got, err)
	}
	do(t, ctx, chromedp.KeyEvent(kb.Escape), chromedp.Focus(chromedp.CSS(`#refresh`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#cards .card-value')?.textContent === 'ready'`)); err != nil {
		t.Fatal(err)
	}
}

func TestAnEmptyOverviewOffersADirectLinkToPlugins(t *testing.T) {
	url := startConsole(t, t.TempDir())
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#empty`)))
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!!document.querySelector('#empty-add[href="#/.plugins"]') && !document.querySelector('#empty-add').hidden`))
	if err != nil || !got {
		t.Fatalf("empty overview needs an actionable plugin link: %v %v", got, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#empty-add`)), chromedp.WaitVisible(chromedp.CSS(`#catalog h1`)))
}

func TestAFailedLayoutSaveRestoresTheVisibleCards(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="c"]`)))
	if err := os.Mkdir(filepath.Join(dir, "layout.json"), 0755); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#cards .card[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`#cards .card[data-id="a"] .hide`)), chromedp.WaitVisible(chromedp.CSS(`#banner`)))
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!!document.querySelector('#cards .card[data-id="a"]') && !!document.querySelector('#nav .nav-item[data-id="a"]') && document.querySelector('#layout-note').hidden`))
	if err != nil || !got {
		t.Fatalf("a refused save should restore the visible layout and remove undo: %v %v", got, err)
	}
}

func TestPluginManagementLabelsItsFormsAndConfirmsInsideMoreActions(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "site", "schema: 1\nname: Site\npage: {link: 'https://example.test/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item`)))
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`['page-kind','page-address','page-name'].every(id => document.getElementById(id).labels.length > 0) && !!document.querySelector('#catalog-add-open') && document.querySelector('#catalog-installed .catalog-toggle')?.getClientRects().length === 0`))
	if err != nil || !got {
		t.Fatalf("want labelled fields, an add shortcut and a hidden disable action: %v %v", got, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#catalog-add-open`)))
	got, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.id === 'page-address'`))
	if err != nil || !got {
		t.Fatalf("the add shortcut must focus the address: %v %v", got, err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`document.querySelector('#page-kind').value = 'link'; document.querySelector('#page-kind').dispatchEvent(new Event('change', {bubbles: true}))`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#page-hint').textContent.includes('new tab')`)); err != nil {
		t.Fatal(err)
	}
	const site = `#catalog-installed .catalog-item[data-id="site"]`
	do(t, ctx, chromedp.Click(chromedp.CSS(site+` .more-trigger`)), chromedp.Click(chromedp.CSS(site+` .catalog-toggle`)))
	got, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('#catalog-installed .more-trigger').getAttribute('aria-expanded') === 'true' && document.querySelector('#catalog-installed .catalog-toggle').textContent.includes('again')`))
	if err != nil || !got {
		state, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`JSON.stringify({expanded:document.querySelector('#catalog-installed .more-trigger').getAttribute('aria-expanded'),text:document.querySelector('#catalog-installed .catalog-toggle').textContent})`))
		t.Fatalf("the first disable click should keep the panel open for confirmation: %v %v %s", got, err, state)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "site")); err != nil {
		t.Fatalf("one click disabled the plugin: %v", err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(site+` .catalog-toggle`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#catalog-installed .catalog-state')?.dataset.state === 'disabled'`)); err != nil {
		t.Fatal(err)
	}
}

func TestTheServiceToolbarKeepsThePageWhenItsMorePanelCloses(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "b", fmt.Sprintf(storagePlugin, "B", storageService(t, "b")))
	url := startConsole(t, dir)
	origin := originOf(t, sidebarURL(t, url, "b"))
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/b"), chromedp.WaitVisible(chromedp.CSS(`#service-name`)))
	collectStates(t, ctx)
	before := serviceState(t, ctx, origin, siteState.full)
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('#service-clear').getClientRects().length === 0 && !!document.querySelector('#service-more .more-trigger') && document.querySelector('#service-reload').getBoundingClientRect().height >= 44`))
	if err != nil || !got {
		t.Fatalf("want a visible refresh control and cache clearing inside More: %v %v", got, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#service-more .more-trigger`)), chromedp.Click(chromedp.CSS(`#service-clear`)))
	got, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('#service-more .more-trigger').getAttribute('aria-expanded') === 'true' && !document.querySelector('#clear-note').hidden`))
	if err != nil || !got {
		t.Fatalf("the first clear click should show its explanation and keep the panel open: %v %v", got, err)
	}
	do(t, ctx, chromedp.KeyEvent(kb.Escape), chromedp.Click(chromedp.CSS(`[data-lang="zh-CN"]`)))
	after := serviceState(t, ctx, origin, siteState.full)
	if after.Nonce != before.Nonce || after.Page != before.Page {
		t.Fatalf("toolbar or language interaction reloaded the embedded page: before %+v after %+v", before, after)
	}
	do(t, ctx, chromedp.EmulateViewport(520, 700))
	got, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.documentElement.scrollWidth <= innerWidth && document.querySelector('#service-reload').getBoundingClientRect().right <= innerWidth`))
	if err != nil || !got {
		t.Fatalf("the narrow toolbar should fit: %v %v", got, err)
	}
}
