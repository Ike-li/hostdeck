package e2e

import (
	"context"
	"fmt"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	cdppage "github.com/chromedp/cdproto/page"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestAppearancePreviewsCanBeCancelledAndSavedForAnotherBrowser(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#overview-title`)))
	check := func(js string) {
		t.Helper()
		if got, err := chromedp.Run(ctx, chromedp.Poll[bool](js, chromedp.WithPollingTimeout(5*time.Second))); err != nil || !got {
			t.Fatalf("%s: %v %v", js, got, err)
		}
	}
	check(`!!document.querySelector('#appearance-open')`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)), chromedp.WaitVisible(chromedp.CSS(`#appearance-preset`)))
	check(`document.activeElement.id === 'appearance-mode'`)
	check(`(() => { const p = document.querySelector('#appearance-preset'); p.value = 'forest'; p.dispatchEvent(new Event('change', {bubbles:true})); return document.documentElement.dataset.theme === 'forest'; })()`)
	check(`(() => { const p=document.querySelector('#appearance-picker'); p.value='#aa3366'; p.dispatchEvent(new Event('input',{bubbles:true})); return document.querySelector('#appearance-accent').value==='#aa3366'; })()`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-use-preset`)))
	check(`document.querySelector('#appearance-use-preset').disabled && document.querySelector('#appearance-accent').value==='#246b4b'`)
	check(`(() => { const p=document.querySelector('#appearance-accent'); p.value='#bad'; p.dispatchEvent(new Event('input',{bubbles:true})); return !p.checkValidity(); })()`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-save`)))
	check(`document.querySelector('#page-editor').open && document.documentElement.dataset.theme==='forest'`)
	if _, err := os.Stat(filepath.Join(dir, "appearance.json")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote settings: %v", err)
	}
	do(t, ctx, chromedp.KeyEvent(kb.Escape))
	check(`document.documentElement.dataset.theme === 'default' && document.activeElement.id === 'appearance-open'`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)))
	check(`(() => { for (const [id, value] of [['appearance-preset','dusk'],['appearance-mode','dark']]) { const p = document.getElementById(id); p.value = value; p.dispatchEvent(new Event('change', {bubbles:true})); } const c = document.querySelector('#appearance-accent'); c.value = '#FFFF00'; c.dispatchEvent(new Event('input', {bubbles:true})); return document.documentElement.dataset.theme === 'dusk' && document.documentElement.dataset.mode === 'dark'; })()`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-save`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('#page-editor').open`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "appearance.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("saved appearance: %s", data)
	other := openBrowser(t, "zh-CN")
	do(t, other, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#overview-title`)))
	if got, err := chromedp.Run(other, chromedp.Evaluate[bool](`document.documentElement.dataset.theme === 'dusk' && document.documentElement.dataset.mode === 'dark' && document.querySelector('#appearance-open').textContent.includes('外观')`)); err != nil || !got {
		t.Fatalf("another browser did not use saved appearance: %v %v", got, err)
	}
	do(t, ctx, chromedp.Reload(), chromedp.WaitVisible(chromedp.CSS(`#overview-title`)))
	check(`document.documentElement.dataset.theme === 'dusk' && document.documentElement.dataset.mode === 'dark'`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)), chromedp.Click(chromedp.CSS(`#appearance-reset`)))
	check(`document.documentElement.dataset.theme === 'default' && document.querySelector('#appearance-mode').value === 'system'`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-cancel`)))
	check(`document.documentElement.dataset.theme === 'dusk'`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)), chromedp.Click(chromedp.CSS(`#appearance-reset`)), chromedp.Click(chromedp.CSS(`#appearance-save`)))
	check(`!document.querySelector('#page-editor').open && document.documentElement.dataset.theme==='default'`)
	if data, err := os.ReadFile(filepath.Join(dir, "appearance.json")); err != nil || strings.Contains(string(data), "accent") || !strings.Contains(string(data), `"mode": "system"`) {
		t.Fatalf("saved defaults: %s %v", data, err)
	}
	// Opening and closing must also fit the existing drawer and short dialog.
	for _, language := range []string{"en", "zh-CN"} {
		do(t, ctx, chromedp.EmulateViewport(1280, 640))
		check(`!document.querySelector('.side-menu').inert`)
		do(t, ctx, chromedp.Click(chromedp.CSS(fmt.Sprintf(`[data-lang=%q]`, language))))
		check(fmt.Sprintf(`document.documentElement.lang===%q`, language))
		do(t, ctx, chromedp.EmulateViewport(520, 640), chromedp.Click(chromedp.CSS(`#menu`)))
		drawerSettled(t, ctx, true)
		do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)))
		check(`(() => { const r = document.querySelector('#appearance-save').getBoundingClientRect(); return r.top >= 0 && r.bottom <= innerHeight && document.documentElement.scrollWidth <= innerWidth && !document.querySelector('main').inert; })()`)
		do(t, ctx, chromedp.Focus(chromedp.CSS(`#appearance-save`)), chromedp.KeyEvent(kb.Tab))
		check(`document.activeElement.id === 'appearance-mode'`)
		do(t, ctx, chromedp.KeyEvent(kb.Escape))
		check(`document.activeElement.id === 'menu'`)
	}
}

// Measure the rendered values independently of the adaptation algorithm.
func TestAppearanceColoursRemainReadableAcrossPresetsModesAndCustomHues(t *testing.T) {
	url := startConsole(t, t.TempDir())
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#appearance-open`)), chromedp.Click(chromedp.CSS(`#appearance-open`)))
	const measure = `(() => {
 const input = document.querySelector('#appearance-accent');
 const change = (id, value) => { const c = document.getElementById(id); c.value=value; c.dispatchEvent(new Event('change', {bubbles:true})); };
 const rgb = x => [1,3,5].map(i=>parseInt(x.slice(i,i+2),16)/255);
 const lum = c => c.map(x=>x<=.04045?x/12.92:((x+.055)/1.055)**2.4).reduce((v,x,i)=>v+x*[.2126,.7152,.0722][i],0);
 const ratio = (a,b) => { a=lum(a); b=lum(b); return (Math.max(a,b)+.05)/(Math.min(a,b)+.05); };
 const mix = (a,b,p) => a.map((x,i)=>x*(1-p)+b[i]*p);
 const failures = [];
 const colours = ['#000000','#ffffff','#808080','#ff0000','#00ff00','#0000ff','#ffff00','#00ffff','#ff00ff','#123456','#eeee00'];
 for (const preset of ['default','forest','dusk','sand']) for (const mode of ['light','dark']) {
  change('appearance-preset',preset); change('appearance-mode',mode);
  for (const colour of colours) {
   input.value=colour; input.dispatchEvent(new Event('input',{bubbles:true}));
   const s=getComputedStyle(document.documentElement), token=n=>rgb(s.getPropertyValue(n).trim());
   const a=token('--accent'), panel=token('--panel'), bg=token('--bg'), label=token('--on-accent'), text=token('--text');
   const primary=['#ff0000','#00ff00','#0000ff'].indexOf(colour);
   if(primary>=0) { const others=a.filter((_,i)=>i!==primary); if(a[primary]<=Math.max(...others)||Math.abs(others[0]-others[1])>1/255) failures.push('custom hue was not applied: '+preset+'/'+mode+'/'+colour); }
   if(['#000000','#ffffff','#808080'].includes(colour)&&Math.max(...a)-Math.min(...a)>1/255) failures.push('neutral custom accent gained a hue');
   const check=(x,y,min,why)=>{if(ratio(x,y)<min)failures.push(preset+'/'+mode+'/'+colour+': '+why+' '+ratio(x,y));};
   check(a,panel,4.5,'accent on panel'); check(a,bg,4.5,'accent on background'); check(a,mix(bg,a,.14),4.5,'selected group');
   for(const n of ['--accent','--accent-strong','--accent-pressed']) check(label,token(n),4.5,n+' label');
   for(const surface of [bg,panel]) { check(text,mix(surface,a,.22),4.5,'pressed text'); check(a,surface,3,'focus'); }
   if(s.getPropertyValue('--accent')===s.getPropertyValue('--accent-strong')||s.getPropertyValue('--accent-strong')===s.getPropertyValue('--accent-pressed')) failures.push('indistinguishable button states');
   if (!document.querySelector('#appearance-actual').textContent.includes(s.getPropertyValue('--accent').trim())) failures.push('actual colour not shown');
  }
 }
 return failures.join('; ') || 'ok';
})()`
	if got, err := chromedp.Run(ctx, chromedp.Evaluate[string](measure)); err != nil || got != "ok" {
		t.Fatalf("custom palette contrast: %s %v", got, err)
	}
	// A system-mode draft follows OS changes; a fixed choice ignores them.
	for _, mode := range []string{"system", "light"} {
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => { const c=document.querySelector('#appearance-mode'); c.value='`+mode+`'; c.dispatchEvent(new Event('change',{bubbles:true})); })()`)); err != nil {
			t.Fatal(err)
		}
		for _, system := range []string{"light", "dark"} {
			if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: system}}}); err != nil {
				t.Fatal(err)
			}
			want := mode
			if mode == "system" {
				want = system
			}
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.documentElement.dataset.mode === %q`, want))); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func saveAppearanceElsewhere(t *testing.T, url, preset string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url+"/api/appearance", strings.NewReader(`{"version":1,"preset":"`+preset+`","mode":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", url)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("external save: %s", response.Status)
	}
}

func TestAppearanceSynchronizationKeepsDraftsAndDiscardsReadsFromBeforeASave(t *testing.T) {
	url := startConsole(t, t.TempDir())
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#appearance-open`)))
	saveAppearanceElsewhere(t, url, "forest")
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`window.dispatchEvent(new Event('focus'))`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.dataset.theme==='forest'`)); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)))
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => { const c=document.querySelector('#appearance-preset'); c.value='dusk'; c.dispatchEvent(new Event('change',{bubbles:true})); })()`)); err != nil {
		t.Fatal(err)
	}
	saveAppearanceElsewhere(t, url, "sand")
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`window.__appearanceReads = performance.getEntriesByName(location.origin+'/api/appearance').length`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`window.dispatchEvent(new Event('focus'))`)); err != nil {
		t.Fatal(err)
	}
	// The read has to finish; observing only the unchanged preview would be vacuous.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`performance.getEntriesByName(location.origin+'/api/appearance').length > window.__appearanceReads`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `document.documentElement.dataset.theme`); got != "dusk" {
		t.Fatalf("remote save replaced draft: %s", got)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-cancel`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.dataset.theme==='sand'`)); err != nil {
		t.Fatal(err)
	}
	// Hold an older network response, then save a newer choice through the UI.
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`window.__heldReadCount = performance.getEntriesByName(location.origin+'/api/appearance').length + 1`)); err != nil {
		t.Fatal(err)
	}
	watch, cancel := context.WithCancel(ctx)
	defer cancel()
	paused := make(chan fetch.EventRequestPaused, 1)
	go func() {
		for event, err := range chromedp.Events(watch, fetch.RequestPaused) {
			if err == nil {
				paused <- event
			}
			return
		}
	}()
	if _, err := chromedp.Call(ctx, fetch.Enable, fetch.EnableParams{Patterns: []*fetch.RequestPattern{{URLPattern: "*/api/appearance", RequestStage: fetch.RequestStageResponse}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = chromedp.Call(ctx, fetch.Disable, cdp.Empty{}) })
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`window.dispatchEvent(new Event('focus'))`)); err != nil {
		t.Fatal(err)
	}
	var response fetch.EventRequestPaused
	select {
	case response = <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("appearance read never reached response boundary")
	}
	// Stop interception before POST, leaving the held GET in place.
	if _, err := chromedp.Call(ctx, fetch.Enable, fetch.EnableParams{Patterns: []*fetch.RequestPattern{{URLPattern: "*/api/appearance-held-only"}}}); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)))
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => { const c=document.querySelector('#appearance-preset'); c.value='dusk'; c.dispatchEvent(new Event('change',{bubbles:true})); })()`)); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-save`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('#page-editor').open`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Call(ctx, fetch.ContinueResponse, fetch.ContinueResponseParams{RequestID: response.RequestID}); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`performance.getEntriesByName(location.origin+'/api/appearance').length > window.__heldReadCount`)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `document.documentElement.dataset.theme`); got != "dusk" {
		t.Fatalf("old response undid save: %s", got)
	}
}

func TestAppearanceInitialPaintUsesTheServerAndFailedSavesKeepThePreview(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "appearance.json")
	if err := os.WriteFile(file, []byte(`{"version":1,"preset":"sand","mode":"dark","accent":"#ffff00"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	if _, err := chromedp.Call(ctx, cdppage.AddScriptToEvaluateOnNewDocument, cdppage.AddScriptToEvaluateOnNewDocumentParams{Source: `const observer = new MutationObserver(() => { if (!document.body) return; observer.disconnect(); const r = document.documentElement; window.__firstAppearance = {preset:r.dataset.theme,mode:r.dataset.mode,accent:getComputedStyle(r).getPropertyValue('--accent').trim()}; }); observer.observe(document,{childList:true,subtree:true});`}); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#appearance-open`)))
	if got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => { const v=window.__firstAppearance; return v.preset==='sand'&&v.mode==='dark'&&v.accent!=='#d3ad77'&&v.accent===getComputedStyle(document.documentElement).getPropertyValue('--accent').trim(); })()`)); err != nil || !got {
		t.Fatalf("initial body saw wrong theme: %v %v", got, err)
	}
	if err := os.Mkdir(file+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)))
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => { const c=document.querySelector('#appearance-preset'); c.value='forest'; c.dispatchEvent(new Event('change',{bubbles:true})); })()`)); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-save`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#page-editor').open && document.querySelector('#appearance-note').getAttribute('role')==='alert' && !document.querySelector('#appearance-save').disabled && document.documentElement.dataset.theme==='forest'`)); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-cancel`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.dataset.theme==='sand'`)); err != nil {
		t.Fatal(err)
	}
}

func TestAppearanceVisibleWindowsSynchronizeEveryFifteenSeconds(t *testing.T) {
	url := startConsole(t, t.TempDir())
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#appearance-open`)))
	saveAppearanceElsewhere(t, url, "forest")
	pauseTime(t, ctx)
	advanceTime(t, ctx, 15001*time.Millisecond)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.dataset.theme==='forest'`)); err != nil {
		t.Fatal(err)
	}
}

func TestAppearanceReportsABrokenConfigurationAndOnlyASaveReplacesIt(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "appearance.json")
	if err := os.WriteFile(file, []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#appearance-warning`)), chromedp.Click(chromedp.CSS(`#appearance-open`)))
	if got := text(t, ctx, "#appearance-note"); !strings.Contains(got, "保存将替换无效配置") {
		t.Fatal(got)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-reset`)))
	if data, _ := os.ReadFile(file); string(data) != "broken" {
		t.Fatal("reset preview changed broken file")
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-save`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('#page-editor').open && document.querySelector('#appearance-warning').hidden`)); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("explicit repair: %s", data)
	}
}

func TestAppearanceLeavesAnEmbeddedServicesOwnThemeAndPageIntact(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><span id="mode"></span><input id="draft" value="initial"><script>const m=matchMedia('(prefers-color-scheme: dark)');const update=()=>document.querySelector('#mode').textContent=m.matches?'dark':'light';m.addEventListener('change',update);update();</script>`)
	}))
	defer service.Close()
	dir := t.TempDir()
	writePlugin(t, dir, "site", fmt.Sprintf("schema: 1\nname: Site\npage: {proxy: %q}\n", service.URL))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	for _, system := range []string{"light", "dark"} {
		if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: system}}}); err != nil {
			t.Fatal(err)
		}
		do(t, ctx, chromedp.Navigate(url+"#/site"), chromedp.WaitVisible(chromedp.CSS(`#frames iframe`)))
		nodes, err := chromedp.Run(ctx, chromedp.Nodes(chromedp.CSS(`#frames iframe`)))
		if err != nil || len(nodes) != 1 {
			t.Fatalf("frame: %v %v", nodes, err)
		}
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector('#mode')?.textContent===%q`, system), chromedp.WithPollingInFrame(nodes[0]))); err != nil {
			t.Fatal(err)
		}
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => { document.querySelector('#draft').value='editing'; return true; })()`, chromedp.WithPollingInFrame(nodes[0]))); err != nil {
			t.Fatal(err)
		}
		do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-open`)))
		opposite := "dark"
		if system == "dark" {
			opposite = "light"
		}
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf(`(() => { const m=document.querySelector('#appearance-mode'); m.value=%q; m.dispatchEvent(new Event('change',{bubbles:true})); window.__originalFrame=document.querySelector('#frames iframe'); })()`, opposite))); err != nil {
			t.Fatal(err)
		}
		do(t, ctx, chromedp.Click(chromedp.CSS(`#appearance-save`)))
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('#page-editor').open`)); err != nil {
			t.Fatal(err)
		}
		if same, err := chromedp.Run(ctx, chromedp.Evaluate[bool](fmt.Sprintf(`document.querySelector('#frames iframe')===window.__originalFrame && getComputedStyle(window.__originalFrame).colorScheme===%q && document.documentElement.dataset.mode===%q`, system, opposite))); err != nil || !same {
			t.Fatalf("frame inherited console mode or was replaced: %v %v", same, err)
		}
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector('#mode')?.textContent===%q && document.querySelector('#draft')?.value==='editing'`, system), chromedp.WithPollingInFrame(nodes[0]))); err != nil {
			t.Fatalf("console theme changed or reloaded service: %v", err)
		}
	}
}
