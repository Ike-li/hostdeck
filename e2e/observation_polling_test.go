package e2e

import (
	"github.com/Ike-li/hostdeck/web"
	"github.com/chromedp/chromedp"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObservationPollingWaitsForCompletionBacksOffAndResumesImmediately(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	mux.HandleFunc("GET /fixture", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><html><body><script type="module">
import {observationPanel,wakeObservations} from '/observations.js';
import {state} from '/state.js';
state.dict=await (await fetch('/i18n/en.json')).json();state.lang='en';state.current={id:'jobs'};
window.fixtureState=state;window.wake=wakeObservations;window.reads=0;window.pending=0;window.peak=0;window.timers=[];window.responses=[];
const nativeFetch=window.fetch;window.fetch=(url,init)=>{
 if(String(url).startsWith('/api/observations/session/')){
  reads++;pending++;peak=Math.max(peak,pending);
  return new Promise((resolve,reject)=>{let ended=false;const finish=(value,error)=>{if(ended)return;ended=true;pending--;error?reject(error):resolve(value);};
   responses.push((ok=true)=>finish(new Response(JSON.stringify({state:'running',collectedAt:'2026-10-28T00:00:00Z',reset:true,cursor:String(reads),log:'retained output',sources:[{state:'available'}]}),{status:ok?200:503})));
   init.signal.addEventListener('abort',()=>finish(null,new DOMException('aborted','AbortError')));
  });
 }
 if(String(url).startsWith('/api/observations/'))return Promise.resolve(new Response(' {"id":"session"}'));
 return nativeFetch(url,init);
};
const nativeTimeout=window.setTimeout;const nativeClear=window.clearTimeout;
window.setTimeout=(fn,delay,...args)=>{if([1000,2000,5000,10000].includes(delay)){timers.push({fn,delay,cancelled:false});return -timers.length;}return nativeTimeout(fn,delay,...args)};
window.clearTimeout=(id)=>{if(id<0&&timers[-id-1])timers[-id-1].cancelled=true;else nativeClear(id)};
const view=document.createElement('div');document.body.append(view);window.box=observationPanel({id:'jobs'},{id:'one'},view);view.append(box);box.open=true;
Object.defineProperty(document,'hidden',{get:()=>!!window.background});
window.fixtureReady=true;
</script></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(srv.URL+"/fixture"))
	poll := func(expression string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](expression)); err != nil {
			t.Fatal(expression, err)
		}
	}
	evaluate := func(expression string) { t.Helper(); do(t, ctx, chromedp.Evaluate[chromedp.Void](expression)) }
	poll(`window.fixtureReady&&reads===1`)
	evaluate(`wake();wake();window.slowOK=pending===1&&peak===1&&timers.length===0;responses.shift()(true)`)
	poll(`slowOK&&timers.length===1&&timers[0].delay===1000`)
	for _, delay := range []int{2000, 5000, 10000} {
		evaluate(`void timers.at(-1).fn()`)
		poll(`pending===1`)
		evaluate(`responses.shift()(false)`)
		expected := "2000"
		if delay == 5000 {
			expected = "5000"
		}
		if delay == 10000 {
			expected = "10000"
		}
		poll(`pending===0&&timers.at(-1).delay===` + expected)
	}
	poll(`box.querySelector('pre.observation-log').textContent==='retained output'&&box.querySelector('.observation-time').textContent.includes('2026')&&box.querySelector('.observation-status').textContent.includes('Unknown')`)
	evaluate(`window.beforeLeave=reads;fixtureState.current=null;wake();void timers.at(-1).fn()`)
	poll(`reads===beforeLeave&&pending===0`)
	evaluate(`fixtureState.current={id:'jobs'};wake()`)
	poll(`reads===beforeLeave+1&&pending===1`)
	evaluate(`background=true;document.dispatchEvent(new Event('visibilitychange'));background=false;document.dispatchEvent(new Event('visibilitychange'))`)
	poll(`reads===beforeLeave+2&&pending===1&&peak===1`)
	evaluate(`responses.at(-1)(true);responses=[];box.open=false`)
	poll(`pending===0&&!box.open`)
}
