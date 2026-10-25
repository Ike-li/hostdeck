package e2e

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestAddingContentStartsWithActionsAndKeepsConnectionHelpAfterThem(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			landing := func() {
				t.Helper()
				if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
 const address=document.querySelector('#page-address'), r=address.getBoundingClientRect();
 return location.hash==='#/.plugins' && document.activeElement===address && r.top>=0 && r.bottom<=innerHeight && r.left>=0 && r.right<=innerWidth;
})()`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
					t.Fatalf("add shortcut should reveal and focus the complete address field: %v", err)
				}
			}
			for _, width := range []int64{1280, 768, 520} {
				do(t, ctx, chromedp.EmulateViewport(width, 800), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="a"] .card-value`)), chromedp.Focus(chromedp.CSS(`#empty-add`)), chromedp.KeyEvent(kb.Enter), chromedp.WaitVisible(chromedp.CSS(`#catalog-available .add`)))
				landing()
				ordered, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const area=document.querySelector('#catalog-add'),form=document.querySelector('#page-form'),available=document.querySelector('#catalog-available'),help=document.querySelector('#connection-help');
 const before=(a,b)=>!!(a.compareDocumentPosition(b)&Node.DOCUMENT_POSITION_FOLLOWING);
 const f=form.getBoundingClientRect(),a=available.getBoundingClientRect(),h=help.getBoundingClientRect();
 return area.contains(form) && area.contains(available) && area.lastElementChild===help && before(form,available) && before(available,help)
   && f.top<a.top && a.bottom<=h.top && area.nextElementSibling.matches('.catalog-directory')
   && document.documentElement.scrollWidth<=innerWidth && document.querySelector('main').scrollWidth<=document.querySelector('main').clientWidth+1;
})()`))
				if err != nil || !ordered {
					t.Fatalf("actions should precede secondary help in DOM and layout at %dpx in %s: %v %v", width, lang, ordered, err)
				}
				do(t, ctx, chromedp.Focus(chromedp.CSS(`#catalog-add-open`)), chromedp.KeyEvent(kb.Enter))
				landing()
				do(t, ctx, chromedp.Focus(chromedp.CSS(`#page-form button[type="submit"]`)), chromedp.KeyEvent(kb.Tab))
				focused, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement===document.querySelector('#catalog-available .add')`))
				if err != nil || !focused {
					t.Fatalf("Tab should reach built-in add actions before help: %v %v", focused, err)
				}
				do(t, ctx, chromedp.Focus(chromedp.CSS(`#catalog-available .catalog-item:last-child .add`)), chromedp.KeyEvent(kb.Tab))
				focused, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.matches('#connection-help > summary')`))
				if err != nil || !focused {
					t.Fatalf("help should follow the last built-in action in keyboard order: %v %v", focused, err)
				}
				do(t, ctx, chromedp.KeyEvent(kb.Enter))
				intact, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const help=document.querySelector('#connection-help'),r=help.getBoundingClientRect();
 const ids=['connection-help','help-pages','help-plugins','copy-page-prompt','copy-plugin-prompt','page-agent-prompt','plugin-agent-prompt','copy-page-note','copy-plugin-note','page-prompt-preview','plugin-prompt-preview'];
 return help.open && ids.every(id=>document.querySelectorAll('#'+id).length===1) && ['page','plugin'].every(kind=>{const p=document.getElementById(kind+'-agent-prompt');return p.readOnly && p.value.length>500;})
   && help.scrollWidth<=help.clientWidth+1 && r.left>=0 && r.right<=innerWidth && document.documentElement.scrollWidth<=innerWidth;
})()`))
				if err != nil || !intact {
					t.Fatalf("expanded help and copy prompts must remain intact at %dpx in %s: %v %v", width, lang, intact, err)
				}
			}
		})
	}
}
