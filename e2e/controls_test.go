package e2e

import (
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A disabled button does not answer the pointer: under it, its edge and fill
// stay what they are beside it, while an enabled button's change. A button
// that cannot be pressed would otherwise look as if it could. This holds for a
// primary button and for an ordinary one.
func TestADisabledButtonDoesNotAnswerThePointer(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	// Without transitions, the colour read under the pointer is the one it settles on.
	if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
		Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
	}); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`.page-form button[type=submit]`)))

	type look struct {
		Hover        bool
		Border, Fill string
	}
	read := func(selector string) look {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[look](`(() => { const b = document.querySelector('`+selector+`'), s = getComputedStyle(b);
 return {hover: b.matches(':hover'), border: s.borderTopColor, fill: s.backgroundColor}; })()`))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, selector := range []string{`#catalog-add-open`, `.page-form button[type=submit]`} {
		for _, disabled := range []bool{false, true} {
			point, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](`(() => { const b = document.querySelector('`+selector+`'); b.disabled = `+map[bool]string{false: "false", true: "true"}[disabled]+`;
 const r = b.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`))
			if err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, 4, 4))
			beside := read(selector)
			do(t, ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, point[0], point[1]))
			under := read(selector)
			switch {
			case !disabled && (!under.Hover || under == look{Hover: true, Border: beside.Border, Fill: beside.Fill}):
				t.Errorf("%s enabled should answer the pointer: %+v beside, %+v under", selector, beside, under)
			case disabled && (under.Border != beside.Border || under.Fill != beside.Fill):
				t.Errorf("%s disabled answers the pointer: %+v beside, %+v under", selector, beside, under)
			}
		}
	}
}

// A control reads as the same control wherever it appears: one border, corner
// and size for buttons and one for fields, one fade when it is disabled, one
// outline when it is focused, and a pressed state of its own so a click
// answers before the console does.
func TestEveryControlSharesOneChromeAndOneStateSystem(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-add`)))

	check := func(js string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", js, got, err)
		}
	}

	check(`(() => {
 const same = (els, props) => {
   const key = e => { const s = getComputedStyle(e); return props.map(p => s[p]).join('|'); };
   const first = key(els[0]);
   return els.every(e => key(e) === first);
 };
 const buttons = [...document.querySelectorAll('.btn')].filter(e => e.getClientRects().length);
 const fields = [...document.querySelectorAll('input:not([type=hidden]), select, textarea')].filter(e => e.getClientRects().length);
 return buttons.length >= 2 && fields.length >= 2 &&
   same(buttons, ['borderTopWidth', 'borderTopStyle', 'borderRadius', 'fontSize', 'minHeight']) &&
   same(fields, ['borderTopWidth', 'borderTopStyle', 'borderRadius', 'backgroundColor']);
 })()`)

	check(`(() => {
 const controls = [...document.querySelectorAll('button, select, input:not([type=hidden])')].filter(e => e.getClientRects().length);
 const seen = new Set();
 for (const c of controls) { c.disabled = true; seen.add(getComputedStyle(c).opacity); c.disabled = false; }
 return controls.length >= 2 && seen.size === 1 && !seen.has('1');
 })()`)

	check(`(() => {
 const pressed = [];
 for (const sheet of document.styleSheets) {
   const walk = rules => { for (const r of rules) { if (r.cssRules && r.cssRules.length) walk(r.cssRules); else if (r.selectorText && r.selectorText.includes(':active')) pressed.push(r.selectorText); } };
   try { walk(sheet.cssRules); } catch (e) {}
 }
 const matches = (el, selector) => {
   const bare = selector.replace(/:active|:hover|:not\([^)]*\)/g, '').trim();
   if (!bare) return false;
   try { return el.matches(bare); } catch (e) { return false; }
 };
 const controls = [...document.querySelectorAll('.btn, .more-trigger, .lang button, .nav-item')].filter(e => e.getClientRects().length);
 return controls.length >= 2 && controls.every(e => pressed.some(sel => sel.split(',').some(part => matches(e, part))));
 })()`)

	const onControl = `(() => { const el = document.activeElement; return !!el && el !== document.body && el.matches('.btn, .nav-item, .lang button, input:not([type=hidden]), select'); })()`
	reached := false
	for i := 0; i < 10 && !reached; i++ {
		do(t, ctx, chromedp.KeyEvent(kb.Tab))
		if got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](onControl)); err == nil && got {
			reached = true
		}
	}
	if !reached {
		t.Fatal("Tab never reaches a control")
	}
	check(`(() => {
 const el = document.activeElement;
 const s = getComputedStyle(el);
 return el.matches(':focus-visible') && s.outlineWidth === '2px' && s.outlineStyle === 'solid' && s.outlineOffset === '3px' && s.outlineColor !== 'rgba(0, 0, 0, 0)';
 })()`)
}
