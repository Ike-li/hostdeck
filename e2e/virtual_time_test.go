package e2e

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Control the browser clock at its protocol boundary. Product constants and
// the backend clock keep their real values; every case owns its virtual clock.
func pauseTime(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := chromedp.Call(ctx, emulation.SetVirtualTimePolicy, emulation.SetVirtualTimePolicyParams{Policy: emulation.VirtualTimePolicyPause}); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => {
 const schedule = window.setTimeout.bind(window);
 window.__clockTimerStarts = [];
 window.__clockTimerTimes = [];
 window.setTimeout = (callback, delay, ...args) => {
   const id = schedule(callback, delay, ...args);
   window.__clockTimerStarts.push(delay);
   window.__clockTimerTimes.push(performance.now());
   return id;
 };
})()`)); err != nil {
		t.Fatal(err)
	}
}

// Saving the file does not prove the browser processed its response yet.
// Observe timer creation at the clock boundary while virtual time is paused.
// A paused clock also holds back tasks on most of Blink's queues, such as a
// promise resolution Blink defers, so each look that finds no timer lets the
// clock run for a microsecond. Timer discovery must not require every pending
// request to finish: its response task can itself be held by the paused clock.
func waitForTimer(t *testing.T, ctx context.Context, delay, cursor int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		started, err := chromedp.Run(ctx, chromedp.Evaluate[bool](fmt.Sprintf(`window.__clockTimerStarts.slice(%d).includes(%d)`, cursor, delay)))
		if err != nil {
			t.Fatal(err)
		}
		if started {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the browser never armed its %d ms timer", delay)
		}
		advanceClock(t, ctx, time.Microsecond, emulation.VirtualTimePolicyAdvance)
		time.Sleep(10 * time.Millisecond)
	}
}

// clockBound is how long a budget may take in real time. Only the stalled-clock
// probe, in a process of its own, shortens it.
var clockBound = 10 * time.Second

func advanceTime(t *testing.T, ctx context.Context, duration time.Duration) {
	t.Helper()
	advanceClock(t, ctx, duration, emulation.VirtualTimePolicyPauseIfNetworkFetchesPending)
}

func advanceClock(t *testing.T, ctx context.Context, duration time.Duration, policy emulation.VirtualTimePolicy) {
	t.Helper()
	bounded, cancel := context.WithTimeout(ctx, clockBound)
	defer cancel()
	events := chromedp.Events(bounded, emulation.VirtualTimeBudgetExpired)
	budget := float64(duration) / float64(time.Millisecond)
	if _, err := chromedp.Call(bounded, emulation.SetVirtualTimePolicy, emulation.SetVirtualTimePolicyParams{
		Policy: policy, Budget: &budget, MaxVirtualTimeTaskStarvationCount: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	for _, err := range events {
		if err != nil {
			t.Fatalf("the browser clock did not finish %v within %v: %v; %s", duration, clockBound, err, clockStall(ctx))
		}
		return
	}
	t.Fatalf("the browser clock did not finish %v: %s", duration, clockStall(ctx))
}

// clockStall says what may be holding the browser clock: the requests still
// open, and whether the page answers at all.
func clockStall(ctx context.Context) string {
	report := "no request was open"
	if log, ok := openRequests.Load(ctx); ok {
		if open := log.(*requestLog).list(time.Now()); len(open) > 0 {
			report = "requests still open: " + strings.Join(open, ", ")
		}
	}
	probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	state, err := chromedp.Run(probe, chromedp.Evaluate[string](`performance.now().toFixed(3) + ' ms, ' + document.readyState`))
	if err != nil {
		return report + "; the page did not answer within 2 s"
	}
	if strings.Contains(report, "/api/layout") {
		report += "; layout.json may already be written — paused virtual time can still hold the fetch answer"
	}
	return report + "; the page answered at " + state
}

// openRequests holds each browser case's requestLog, keyed by its context.
var openRequests sync.Map

// requestLog keeps the requests a case's page sent and has not finished. The
// three events arrive on separate subscriptions, so an end can come first.
type requestLog struct {
	mu    sync.Mutex
	open  map[network.RequestID]sentRequest
	ended map[network.RequestID]bool
}

type sentRequest struct {
	line  string
	since time.Time
}

func (l *requestLog) sent(id network.RequestID, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ended[id] {
		l.open[id] = sentRequest{line, time.Now()}
	}
}

func (l *requestLog) end(id network.RequestID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.open, id)
	l.ended[id] = true
}

func (l *requestLog) list(now time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	requests := slices.SortedFunc(maps.Values(l.open), func(a, b sentRequest) int { return a.since.Compare(b.since) })
	lines := make([]string, len(requests))
	for i, r := range requests {
		lines[i] = fmt.Sprintf("%s (open %.1f s)", r.line, now.Sub(r.since).Seconds())
	}
	return lines
}

// watchRequests records a case's open requests from its start, so that one
// sent before the clock was paused is named too.
func watchRequests(t *testing.T, ctx context.Context) {
	t.Helper()
	watch, cancel := context.WithCancel(ctx)
	log := &requestLog{open: map[network.RequestID]sentRequest{}, ended: map[network.RequestID]bool{}}
	sent := chromedp.Events(watch, network.RequestWillBeSent)
	finished := chromedp.Events(watch, network.LoadingFinished)
	failed := chromedp.Events(watch, network.LoadingFailed)
	var wg sync.WaitGroup
	wg.Go(func() {
		for ev, err := range sent {
			if err != nil {
				return
			}
			log.sent(ev.RequestID, ev.Request.Method+" "+ev.Request.URL)
		}
	})
	wg.Go(func() {
		for ev, err := range finished {
			if err != nil {
				return
			}
			log.end(ev.RequestID)
		}
	})
	wg.Go(func() {
		for ev, err := range failed {
			if err != nil {
				return
			}
			log.end(ev.RequestID)
		}
	})
	openRequests.Store(ctx, log)
	t.Cleanup(func() { openRequests.Delete(ctx); cancel(); wg.Wait() })
}

// An open request or a busy page can each keep the clock from finishing its
// budget. The failure names both, so a stall on CI explains itself.
func TestAStalledBrowserClockSaysWhichRequestsWereOpenAndWhetherThePageAnswered(t *testing.T) {
	if testing.Short() {
		t.Skip("browser clock diagnostics")
	}
	if os.Getenv("HOSTDECK_TEST_CLOCK_STALL_PROBE") == "1" {
		clockBound = time.Second
		sent, release := make(chan struct{}), make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/held" {
				close(sent)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				return
			}
			fmt.Fprint(w, "<!doctype html><title>stall</title>")
		}))
		t.Cleanup(server.Close)
		t.Cleanup(func() { close(release) })
		t.Run("open request", func(t *testing.T) {
			ctx := openBrowser(t, "en")
			do(t, ctx, chromedp.Navigate(server.URL))
			pauseTime(t, ctx)
			if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`fetch('/held'), undefined`)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-sent:
			case <-time.After(10 * time.Second):
				t.Fatal("the held request never reached the server")
			}
			advanceTime(t, ctx, time.Second)
			t.Fatal("the open request let the clock finish its budget")
		})
		t.Run("busy page", func(t *testing.T) {
			ctx := openBrowser(t, "en")
			do(t, ctx, chromedp.Navigate(server.URL))
			pauseTime(t, ctx)
			// The paused clock holds the message back until the budget lets it run.
			if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => {
 const channel = new MessageChannel();
 channel.port1.onmessage = () => { for (let i = 0; i < 1e11; i++) {} };
 channel.port2.postMessage(null);
})()`)); err != nil {
				t.Fatal(err)
			}
			advanceTime(t, ctx, time.Second)
			t.Fatal("the busy page let the clock finish its budget")
		})
		return
	}
	// Use the same optional-browser policy as other browser tests.
	openBrowser(t, "en")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAStalledBrowserClockSaysWhichRequestsWereOpenAndWhetherThePageAnswered$")
	cmd.Env = append(os.Environ(), "HOSTDECK_ARTIFACT_DIR=", "HOSTDECK_E2E=1", "HOSTDECK_TEST_CLOCK_STALL_PROBE=1")
	out, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil {
		t.Fatalf("unexpected probe result: %s %v", out, err)
	}
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`requests still open: [^;]*GET http://127\.0\.0\.1:\d+/held \(open \d+\.\d s\); the page answered at \d+\.\d{3} ms, complete`),
		regexp.MustCompile(`no request was open; the page did not answer within 2 s`),
	} {
		if !want.Match(out) {
			t.Fatalf("the stalled clock's failure should match %s: %s", want, out)
		}
	}
}

// While virtual time is paused Blink holds back tasks on most queues,
// including a promise resolution it had to defer, as a saved layout's answer
// sometimes is. A message between two ports takes one of those queues.
func TestWaitingForATimerRunsWorkThePausedClockHoldsBackWithoutMovingIt(t *testing.T) {
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate("about:blank"))
	pauseTime(t, ctx)
	before, err := chromedp.Run(ctx, chromedp.Evaluate[float64](`(() => {
 const channel = new MessageChannel();
 channel.port1.onmessage = () => setTimeout(() => {}, 1234);
 channel.port2.postMessage(null);
 return performance.now();
})()`))
	if err != nil {
		t.Fatal(err)
	}
	waitForTimer(t, ctx, 1234, 0)
	after, err := chromedp.Run(ctx, chromedp.Evaluate[float64](`performance.now()`))
	if err != nil {
		t.Fatal(err)
	}
	if moved := after - before; moved >= 1 {
		t.Fatalf("waiting for the timer moved the browser clock %.3f ms", moved)
	}
}

func TestTheLayoutNoticeUsesEightWholeIdleSeconds(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	// The sidebar appears before the initial card fetch has completed.
	// Finish that reading before freezing the browser scheduler.
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="a"] .card-value`)))
	pauseTime(t, ctx)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .move-down`)))
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
	waitForTimer(t, ctx, 8000, 0)
	// Discovery can advance the clock after registration. Measure from the
	// latest actual timer start, rather than assuming discovery was its start.
	remaining, err := chromedp.Run(ctx, chromedp.Evaluate[float64](`(() => {
 const i = window.__clockTimerStarts.lastIndexOf(8000);
 return window.__clockTimerTimes[i] + 8000 - performance.now();
})()`))
	if err != nil || remaining <= 1 || remaining > 8001 {
		t.Fatalf("invalid notice deadline %.3f ms: %v", remaining, err)
	}
	advanceTime(t, ctx, time.Duration((remaining-1)*float64(time.Millisecond)))
	visible, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!document.querySelector('#layout-note').hidden`))
	if err != nil || !visible {
		t.Fatalf("notice expired before eight idle seconds: %v %v", visible, err)
	}
	// Let the callback at the deadline run before the next budget expires.
	advanceTime(t, ctx, 2*time.Millisecond)
	hidden, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('#layout-note').hidden`))
	if err != nil || !hidden {
		t.Fatalf("notice remained after eight idle seconds: %v %v", hidden, err)
	}
}

func confirmationExpires(t *testing.T, ctx context.Context, selector, armed, idle string) {
	t.Helper()
	waitForTimer(t, ctx, 4000, 0)
	if got := text(t, ctx, selector); got != armed {
		t.Fatalf("first click says %q, want %q", got, armed)
	}
	advanceTime(t, ctx, 3999*time.Millisecond)
	if got := text(t, ctx, selector); got != armed {
		t.Fatalf("confirmation expired before four seconds: %q", got)
	}
	// Let the callback at the deadline run before the next budget expires.
	advanceTime(t, ctx, 2*time.Millisecond)
	if got := text(t, ctx, selector); got != idle {
		t.Fatalf("confirmation did not expire after four seconds: %q", got)
	}
}

func TestTimerDiscoveryRunsRendererTasksWhileAnotherRequestIsStillOpen(t *testing.T) {
	sent, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/held" {
			close(sent)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		fmt.Fprint(w, "<!doctype html><title>timer discovery</title>")
	}))
	defer server.Close()
	defer close(release)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(server.URL))
	pauseTime(t, ctx)
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`fetch('/held'), undefined`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the pending request never reached the server")
	}
	before, err := chromedp.Run(ctx, chromedp.Evaluate[float64](`(() => {
 const channel=new MessageChannel();
 channel.port1.onmessage=()=>{window.__timerDiscoveryStartedAt=performance.now();setTimeout(()=>{},1234);};
 channel.port2.postMessage(null);
 return performance.now();
 })()`))
	if err != nil {
		t.Fatal(err)
	}
	waitForTimer(t, ctx, 1234, 0)
	times, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](`[performance.now(), window.__timerDiscoveryStartedAt]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != 2 || times[0]-times[1] >= 1 {
		t.Fatalf("timer discovery consumed the new timer's budget: %v (origin %.3f)", times, before)
	}
}

// CI named the paused-clock stall as POST /api/layout still open while the page
// answered. A held layout POST reproduces that discovery deadlock: network-aware
// microsteps cannot run the renderer task that arms the notice timer. Advance
// discovery must still arm it; product durations stay on the network-aware clock.
func TestTimerDiscoveryRunsWithAHeldLayoutPost(t *testing.T) {
	sent, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/layout" {
			close(sent)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprint(w, "<!doctype html><title>layout post</title>")
	}))
	defer server.Close()
	defer close(release)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(server.URL))
	pauseTime(t, ctx)
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`fetch('/api/layout',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'}), undefined`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the layout POST never reached the server")
	}
	before, err := chromedp.Run(ctx, chromedp.Evaluate[float64](`(() => {
 const channel=new MessageChannel();
 channel.port1.onmessage=()=>{window.__layoutDiscoveryStartedAt=performance.now();setTimeout(()=>{},8000);};
 channel.port2.postMessage(null);
 return performance.now();
})()`))
	if err != nil {
		t.Fatal(err)
	}
	waitForTimer(t, ctx, 8000, 0)
	times, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](`[performance.now(), window.__layoutDiscoveryStartedAt]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != 2 || times[0]-times[1] >= 1 {
		t.Fatalf("timer discovery consumed the notice timer budget: %v (origin %.3f)", times, before)
	}
}
