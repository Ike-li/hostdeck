package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestTheSidebarAlignsBrandIconsNamesAndActionsAcrossLanguagesAndWidths(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	svc := databaseService(t)
	writePlugin(t, dir, "proxy", fmt.Sprintf(proxyPlugin, svc))
	writePlugin(t, dir, "long", fmt.Sprintf("schema: 1\nname: %q\npage: {link: 'https://example.test/'}\n", strings.Repeat("Long name ", 20)))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-row[data-id="long"]`)))
	for _, language := range []string{"en", "zh-CN"} {
		do(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Click(chromedp.CSS(fmt.Sprintf(`[data-lang=%q]`, language))))
		for _, width := range []int64{1280, 768, 520} {
			do(t, ctx, chromedp.EmulateViewport(width, 800))
			if width == 520 {
				// The drawer slides in, so the columns are worth comparing
				// only once it has stopped.
				do(t, ctx, chromedp.Click(chromedp.CSS(`#menu`)))
				drawerSettled(t, ctx, true)
			}
			result, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
 const issues = [], rect = e => e.getBoundingClientRect(), close = (a,b) => Math.abs(a-b) < 1;
 const links = [...document.querySelectorAll('.sidebar .nav-item')], rows = [...document.querySelectorAll('#nav .nav-row')];
 const nameX = rect(links[0].querySelector('.nav-name')).left, iconX = rect(links[0].querySelector('.icon')).left;
 const walker = document.createTreeWalker(document.querySelector('.brand'),NodeFilter.SHOW_TEXT);
 let brandX = NaN;
 for (let n; n = walker.nextNode();) if (n.textContent.trim() === 'hostdeck') { const range = document.createRange(); range.selectNodeContents(n); brandX = range.getBoundingClientRect().left; }
 if (!close(brandX,nameX)) issues.push('brand and navigation text start on different columns');
 for (const link of links) {
 const icon = link.querySelector('.icon'), name = link.querySelector('.nav-name');
 if (!close(rect(icon).left,iconX) || rect(icon).width !== 24 || !close(rect(name).left,nameX) || rect(link).height !== 44) issues.push('uneven icon, text or row grid');
 }
 for (const id of ['nav-overview','nav-catalog']) if (!document.querySelector('#'+id+' .icon svg')) issues.push('fixed navigation has an empty icon slot');
 const actionX = rect(rows[0].querySelector('.more-trigger')).left;
 for (const row of rows) {
 const rr = rect(row), action = rect(row.querySelector('.more-trigger')), icon = rect(row.querySelector('.icon'));
 if (!close(action.left,actionX) || !close(action.y+action.height/2,rr.y+rr.height/2) || !close(icon.y+icon.height/2,rr.y+rr.height/2)) issues.push('actions or icons are off centre');
 }
 const indicators = [...document.querySelectorAll('#nav .nav-item .dot, #nav .nav-item .nav-ext')];
 const indicatorX = rect(indicators[0]).x + rect(indicators[0]).width/2;
 if (indicators.some(e => !close(rect(e).x + rect(e).width/2,indicatorX))) issues.push('status and external-link indicators use different columns');
 const lang = document.querySelector('.lang'), selected = lang.querySelector('[aria-pressed="true"]'), other = lang.querySelector('[aria-pressed="false"]');
 if (!close(rect(lang).left,rect(links[0]).left) || !close(rect(lang).right,rect(links[0]).right) || getComputedStyle(selected).backgroundColor === getComputedStyle(other).backgroundColor) issues.push('language switch has no aligned segmented selection');
 const long = document.querySelector('#nav .nav-row[data-id="long"] .nav-name');
 if (long.scrollWidth <= long.clientWidth || getComputedStyle(long).textOverflow !== 'ellipsis' || document.documentElement.scrollWidth > innerWidth) issues.push('long names overflow');
 return issues.join('; ') || 'ok';
 })()`))
			if err != nil || result != "ok" {
				t.Fatalf("sidebar at %dpx in %s: %s %v", width, language, result, err)
			}
		}
	}
	do(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Click(chromedp.CSS(`#nav .nav-item[data-id="proxy"]`)), chromedp.WaitVisible(chromedp.CSS(`#service-name`)))
	selected, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const row = document.querySelector('#nav .nav-row[data-id="proxy"]'), action = row.querySelector('.more-trigger');
 return row.querySelector('.nav-item').classList.contains('active') && getComputedStyle(row).backgroundColor !== 'rgba(0, 0, 0, 0)' && action.getBoundingClientRect().right <= row.getBoundingClientRect().right;
 })()`))
	if err != nil || !selected {
		t.Fatalf("an active plugin should highlight the complete row, including its action column: %v %v", selected, err)
	}
	do(t, ctx, chromedp.Focus(chromedp.CSS(`#nav .nav-row[data-id="proxy"] .more-trigger`)), chromedp.KeyEvent(kb.Enter), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-row[data-id="proxy"] .hide`)), chromedp.KeyEvent(kb.Escape))
}
