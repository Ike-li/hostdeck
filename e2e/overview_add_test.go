package e2e

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestTheOverviewHasAnAddCardAfterPluginsAndItOpensTheAddArea(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="c"]`)))
	for _, width := range []int64{1280, 768, 520} {
		do(t, ctx, chromedp.EmulateViewport(width, 800))
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const add = document.querySelector('#cards > #empty-add');
 if (!add || !add.checkVisibility()) return false;
 const r = add.getBoundingClientRect();
 return add === add.parentElement.lastElementChild && add.tagName === 'A' && add.getAttribute('href') === '#/.plugins' && !add.draggable && !add.dataset.id && add.textContent.includes('+') && add.textContent.includes('添加页面或插件') && r.height >= 160 && r.width >= 44 && r.left >= 0 && r.right <= innerWidth && document.documentElement.scrollWidth <= innerWidth;
 })()`))
		if err != nil || !got {
			t.Fatalf("want a visible, labelled plus card after the plugins that fits %dpx: %v %v", width, got, err)
		}
	}
	do(t, ctx, chromedp.EmulateViewport(1280, 800))
	drag(t, ctx, `#cards .card[data-id="c"]`, `#cards .card[data-id="a"]`)
	savedLayout(t, dir, "cards", []string{"c", "a", "b"})
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('#cards').lastElementChild.id === 'empty-add'`))
	if err != nil || !got {
		t.Fatalf("reordering must leave the add card last: %v %v", got, err)
	}
	do(t, ctx, chromedp.Focus(chromedp.CSS(`#empty-add`)), chromedp.KeyEvent(kb.Enter), chromedp.WaitVisible(chromedp.CSS(`#catalog-available .catalog-item`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const address = document.querySelector('#page-address'), r = address.getBoundingClientRect();
 return location.hash === '#/.plugins' && document.activeElement === address && r.top >= 0 && r.bottom <= innerHeight && [...document.querySelector('#page-kind').options].map(o => o.value).join(',') === 'proxy,link' && document.querySelector('#catalog-add').contains(document.querySelector('#catalog-available'));
 })()`)); err != nil {
		t.Fatalf("the add card must take the keyboard to the existing page and built-in plugin add area: %v", err)
	}
}

func TestAnEmptyOverviewShowsTheSameAddCard(t *testing.T) {
	url := startConsole(t, t.TempDir())
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#empty`)))
	got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const add = document.querySelector('#cards > #empty-add');
 return !!add && add.checkVisibility() && add.textContent.includes('Add a page or plugin') && add.getBoundingClientRect().height >= 160 && document.querySelectorAll('#empty-add').length === 1;
 })()`))
	if err != nil || !got {
		t.Fatalf("an empty overview must keep one full add card: %v %v", got, err)
	}
}
