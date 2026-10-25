package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestResetLayoutIsOfferedForSavedChangesAndDisappearsWhenTheyAreCleared(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			dir := t.TempDir()
			threeLinks(t, dir)
			url := startConsole(t, dir)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(url)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			tag := regexp.MustCompile(`<button\b[^>]*\bid="layout-reset"[^>]*>`).Find(body)
			if !regexp.MustCompile(`\shidden(?:\s|=|>)`).Match(tag) {
				t.Fatalf("reset must start hidden before scripts load: %s", tag)
			}
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="a"] .card-value`)))
			label := "Reset layout"
			if lang == "zh-CN" {
				label = "重置布局"
			}
			check := func(visible bool) {
				t.Helper()
				if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`(() => {
 const reset=document.querySelector('#layout-reset');
 return reset.hidden === %t && reset.checkVisibility() === %t && reset.textContent === %q;
})()`, !visible, visible, label), chromedp.WithPollingTimeout(5*time.Second))); err != nil {
					t.Fatalf("reset visible=%v label=%q: %v", visible, label, err)
				}
			}
			reset := func() {
				t.Helper()
				do(t, ctx, chromedp.Focus(chromedp.CSS(`#layout-reset`)), chromedp.KeyEvent(kb.Enter))
				check(false)
				focused, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.id === 'refresh'`))
				if err != nil || !focused {
					t.Fatalf("reset should leave focus on Refresh: %v %v", focused, err)
				}
			}
			check(false)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .move-down`)))
			savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
			check(true)
			focused, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.matches('#nav .nav-row[data-id="a"] .more-trigger')`))
			if err != nil || !focused {
				t.Fatalf("save acknowledgement moved keyboard focus: %v %v", focused, err)
			}
			reset()
			savedLayout(t, dir, "sidebar", nil)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#layout-undo`)))
			check(true)
			savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
			reset()

			width := func() {
				t.Helper()
				do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`.card[data-id="a"] .card-width`)))
			}
			width()
			check(true)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#layout-undo`)))
			check(false)
			width()
			check(true)
			width()
			check(false)

			do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="c"] .more-trigger`)), chromedp.Click(chromedp.CSS(`.card[data-id="c"] .move-up`)))
			check(true)
			for _, viewport := range []int64{1280, 768, 520} {
				do(t, ctx, chromedp.EmulateViewport(viewport, 800))
				fits, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => { const r=document.querySelector('#layout-reset').getBoundingClientRect(); return r.width>0 && r.left>=0 && r.right<=innerWidth && document.documentElement.scrollWidth<=innerWidth; })()`))
				if err != nil || !fits {
					t.Fatalf("reset should fit %dpx in %s: %v %v", viewport, lang, fits, err)
				}
			}
			reset()
			do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`.card[data-id="a"] .hide`)))
			check(true)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#layout-undo`)))
			check(false)
		})
	}
}

func TestEveryStoredLayoutPreferenceOffersResetAfterReload(t *testing.T) {
	for _, field := range []string{"sidebar", "cards", "wide", "hidden"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			threeLinks(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "layout.json"), []byte(fmt.Sprintf(`{%q:["a"]}`, field)), 0600); err != nil {
				t.Fatal(err)
			}
			url := startConsole(t, dir)
			ctx := openBrowser(t, "en")
			do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#layout-reset`)))
			if got := text(t, ctx, `#layout-reset`); got != "Reset layout" {
				t.Fatalf("reset says %q", got)
			}
		})
	}
}

func TestARefusedLayoutSaveDoesNotOfferResetForAnUnsavedChange(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="a"] .card-value`)))
	if err := os.Mkdir(filepath.Join(dir, "layout.json"), 0700); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`.card[data-id="a"] .card-width`)), chromedp.WaitVisible(chromedp.CSS(`#banner`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#layout-reset').hidden && !document.querySelector('.card[data-id="a"]').classList.contains('wide') && document.querySelector('#layout-note').hidden`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("failed save should restore default controls: %v", err)
	}
}
