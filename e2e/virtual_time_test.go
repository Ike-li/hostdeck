package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
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
 window.setTimeout = (callback, delay, ...args) => {
   const id = schedule(callback, delay, ...args);
   window.__clockTimerStarts.push(delay);
   return id;
 };
})()`)); err != nil {
		t.Fatal(err)
	}
}

// Saving the file does not prove the browser processed its response yet.
// Observe timer creation at the clock boundary while virtual time is paused.
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
		time.Sleep(10 * time.Millisecond)
	}
}

func advanceTime(t *testing.T, ctx context.Context, duration time.Duration) {
	t.Helper()
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	events := chromedp.Events(bounded, emulation.VirtualTimeBudgetExpired)
	budget := float64(duration / time.Millisecond)
	if _, err := chromedp.Call(bounded, emulation.SetVirtualTimePolicy, emulation.SetVirtualTimePolicyParams{
		Policy: emulation.VirtualTimePolicyPauseIfNetworkFetchesPending, Budget: &budget, MaxVirtualTimeTaskStarvationCount: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	for _, err := range events {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("the browser clock did not finish its time budget")
}

func TestTheLayoutNoticeUsesEightWholeIdleSeconds(t *testing.T) {
	dir := t.TempDir()
	threeLinks(t, dir)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)))
	pauseTime(t, ctx)
	do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .more-trigger`)), chromedp.Click(chromedp.CSS(`#nav .nav-row[data-id="a"] .move-down`)))
	savedLayout(t, dir, "sidebar", []string{"b", "a", "c"})
	waitForTimer(t, ctx, 8000, 0)
	advanceTime(t, ctx, 7999*time.Millisecond)
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
