package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestEveryAddedConnectionAppearsInBothWorkspaceLists(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "web", "schema: 1\nname: Web\npage: {link: 'https://example.test/'}\n")
	writePlugin(t, dir, ".off", "schema: 1\nname: Off\npage: {link: 'https://off.test/'}\n")
	writePlugin(t, dir, "hidden", "schema: 1\nname: Hidden\npage: {link: 'https://hidden.test/'}\n")
	if err := os.WriteFile(filepath.Join(dir, "layout.json"), []byte(`{"hidden":["hidden"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-available .catalog-item[data-id="ollama"] .btn`)))
	const add = `#catalog-available .catalog-item[data-id="ollama"] .btn`
	do(t, ctx, chromedp.Click(chromedp.CSS(add)), chromedp.Click(chromedp.CSS(add)), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-item[data-id="ollama"]`)))
	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav-overview`)), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="ollama"]`)), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="web"] .card-destination`)))
	if got := text(t, ctx, `#cards .card[data-id="web"]`); !strings.Contains(got, "新标签") || !strings.Contains(got, "https://example.test/") {
		t.Fatalf("page shortcut says %q", got)
	}
	a := attrs(t, ctx, `#cards .card[data-id="web"] .card-name a`)
	if a["href"] != "https://example.test/" || a["target"] != "_blank" {
		t.Fatalf("page shortcut link: %#v", a)
	}
	for _, id := range []string{"hidden", "off"} {
		gone(t, ctx, fmt.Sprintf(`#nav [data-id="%s"], #cards .card[data-id="%s"]`, id, id))
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-item[data-id="ollama"]`)), chromedp.WaitVisible(chromedp.CSS(`#service .monitor-view .card`)))
	if got := text(t, ctx, `#service-name`); got != "Ollama" {
		t.Fatalf("opened %q", got)
	}
	do(t, ctx, chromedp.Reload(), chromedp.WaitVisible(chromedp.CSS(`#service .monitor-view .card`)))
}

func TestASidebarMonitorRefreshesItsReadingAndFitsANarrowWindow(t *testing.T) {
	var hits atomic.Int32
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fmt.Sprintf(`{"v":%d}`, hits.Add(1)))
	}))
	defer svc.Close()
	dir := t.TempDir()
	writePlugin(t, dir, "monitor", fmt.Sprintf("schema: 1\nname: Monitor\ncard:\n  http: {url: %q}\n  value: v\n  label: {en: readings, zh-CN: 次读取}\n", svc.URL))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/monitor"), chromedp.WaitVisible(chromedp.CSS(`#service .card-value`)))
	before := text(t, ctx, `#service .card-value`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#service .card-value')?.textContent !== `+fmt.Sprintf("%q", before))); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`[data-lang="zh-CN"]`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#service .card-label')?.textContent === '次读取'`)); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.EmulateViewport(520, 700))
	fits, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => { const card=document.querySelector('#service .card'); return document.documentElement.scrollWidth <= innerWidth && card.getBoundingClientRect().right <= innerWidth && !card.querySelector('a,button') && document.querySelector('#service-open').hidden && document.querySelector('#service-more').hidden; })()`))
	if err != nil || !fits {
		t.Fatalf("monitor overflow or unrelated controls: %v %v", fits, err)
	}
}

func TestEnabledConnectionsAreTheFirstManagementSection(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "web", "schema: 1\nname: Web\npage: {link: 'https://example.test/'}\n")
	ctx := openBrowser(t, "zh-CN")
	url := startConsole(t, dir)
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item`)))
	for _, lang := range []string{"zh-CN", "en"} {
		do(t, ctx, chromedp.Click(chromedp.CSS(`[data-lang="`+lang+`"]`)))
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.lang === `+fmt.Sprintf("%q", lang))); err != nil {
			t.Fatal(err)
		}
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {const headings=[...document.querySelectorAll('#catalog h2')];const enabled=document.querySelector('#catalog-installed [data-group="use"] h2');return !!enabled && headings[0]===enabled && enabled.textContent === (document.documentElement.lang==='en'?'Enabled':'已启用') && !document.querySelector('[data-i18n="catalog.installed"]') && !!(enabled.compareDocumentPosition(document.querySelector('#catalog-add')) & Node.DOCUMENT_POSITION_FOLLOWING);})()`))
		if err != nil || !got {
			t.Fatalf("enabled connections are not first in %s: %v %v", lang, got, err)
		}
	}
}

func TestManagementKeepsAnEmptyEnabledSectionBeforeAdding(t *testing.T) {
	url := startConsole(t, t.TempDir())
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitReady(chromedp.CSS(`#catalog-installed [data-group="use"]`)))
	visible, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {const enabled=document.querySelector('#catalog-installed [data-group="use"]');return enabled.checkVisibility() && enabled.textContent.includes('还没有已启用') && document.querySelector('#catalog h2') === enabled.querySelector('h2');})()`))
	if err != nil || !visible {
		t.Fatalf("empty Enabled section disappeared: %v %v", visible, err)
	}
}
