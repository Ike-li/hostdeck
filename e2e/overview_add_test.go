package e2e

import (
	"fmt"
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

// Enough shortcut cards that the plus card sits below an 800px window. The
// header link is what stays on the first screen.
func TestTheOverviewHeaderKeepsAddInViewWhenThePlusCardIsBelowTheFold(t *testing.T) {
	dir := t.TempDir()
	const cards = 28
	for i := range cards {
		writePlugin(t, dir, fmt.Sprintf("p%02d", i), fmt.Sprintf("schema: 1\nname: Page %02d\npage: {link: 'https://p%02d.example/'}\n", i, i))
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelectorAll('#cards .card').length >= %d`, cards))); err != nil {
		t.Fatalf("shortcut cards did not fill the overview: %v", err)
	}

	for _, width := range []int64{1280, 520} {
		do(t, ctx, chromedp.EmulateViewport(width, 800))
		if reason, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
 const scroller = document.querySelector('main');
 scroller.scrollTop = 0;
 scrollTo(0, 0);
 const plus = document.querySelector('#cards > #empty-add');
 if (!plus) return 'fixture has no #empty-add';
 const r = plus.getBoundingClientRect();
 if (scroller.scrollTop !== 0 || scrollY !== 0) return 'not at scroll 0';
 if (r.top < innerHeight) return 'fixture is too short: #empty-add top is ' + Math.round(r.top) + ' inside a ' + innerHeight + 'px window';
 return '';
})()`)); err != nil || reason != "" {
			t.Fatalf("at %dpx: %s (%v)", width, reason, err)
		}
		if reason, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
 const add = document.querySelector('#overview-add');
 if (!add) return 'missing #overview-add';
 const r = add.getBoundingClientRect();
 const primary = [...document.querySelectorAll('#overview .btn.primary')].map(e => e.id);
 const actions = [...document.querySelector('#overview .overview-actions').children].map(e => e.id);
 const cards = document.querySelector('#cards');
 if (add.tagName !== 'A' || !add.classList.contains('btn') || add.classList.contains('primary')) return 'not a secondary button link';
 if (add.getAttribute('href') !== '#/.plugins') return 'href ' + add.getAttribute('href');
 if (cards.contains(add)) return 'link is inside #cards';
 if (r.width <= 0 || r.top < 0 || r.left < 0 || r.bottom > innerHeight || r.right > innerWidth) return 'link is not fully inside the viewport';
 if (add.textContent.trim() !== '添加内容') return 'label ' + JSON.stringify(add.textContent);
 if (primary.join(',') !== 'refresh') return 'primary ' + primary.join(',');
 if (actions.join(',') !== 'layout-reset,overview-add,refresh') return 'order ' + actions.join(',');
 if (cards.lastElementChild.id !== 'empty-add' || document.querySelectorAll('#empty-add').length !== 1) return 'plus card is not the only last cell';
 if (getComputedStyle(document.querySelector('#overview h1')).fontSize !== '16px') return 'overview title ' + getComputedStyle(document.querySelector('#overview h1')).fontSize;
 return '';
})()`)); err != nil || reason != "" {
			t.Fatalf("overview header at %dpx: %s (%v)", width, reason, err)
		}
	}

	// The 520px pass leaves the sidebar inert until the wide layout settles.
	// A click in that turn misses the language control.
	do(t, ctx, chromedp.EmulateViewport(1280, 800))
	clickOnScreen(t, ctx, `[data-lang="en"]`)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#overview-add')?.textContent.trim() === 'Add content'`)); err != nil {
		t.Fatalf("English overview add link: %v", err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#overview-add`)), chromedp.WaitVisible(chromedp.CSS(`#catalog-available .catalog-item`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const address = document.querySelector('#page-address'), r = address.getBoundingClientRect();
 return location.hash === '#/.plugins' && document.activeElement === address && r.top >= 0 && r.bottom <= innerHeight && [...document.querySelector('#page-kind').options].map(o => o.value).join(',') === 'proxy,link' && document.querySelector('#catalog-add').contains(document.querySelector('#catalog-available')) && getComputedStyle(document.querySelector('#catalog h1')).fontSize === '24px';
})()`)); err != nil {
		t.Fatalf("the header add link must open the same add area as the plus card: %v", err)
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
