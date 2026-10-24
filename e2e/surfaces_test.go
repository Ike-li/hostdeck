package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// A card shows the state it reported and answers the pointer, and neither one
// moves the card by a pixel: a grid of cards is laid out once and stays where
// it was put.
func TestACardShowsItsStateAndAnswersThePointerWithoutMoving(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"waiting": 72, "workers": 3}`)
	}))
	t.Cleanup(svc.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "queue", `schema: 1
name: Queue
card:
  http: {url: '`+svc.URL+`'}
  value: waiting
  warn: {above: 50}
`)
	writePlugin(t, dir, "plain", `schema: 1
name: Plain
card:
  http: {url: '`+svc.URL+`'}
  value: workers
`)
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="queue"] .card-value`)),
		chromedp.WaitVisible(chromedp.CSS(`.card[data-id="plain"] .card-value`)))

	check := func(js string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", js, got, err)
		}
	}

	// Only a card that reported a state carries the rail, and the rail is
	// drawn rather than laid out: both cards keep the box the grid gave them.
	check(`(() => {
 const cards = [...document.querySelectorAll('#cards .card')];
 const stated = cards.find(c => c.dataset.status);
 const plain = cards.find(c => !c.dataset.status && c.dataset.id);
 if (!stated || !plain) return false;
 const rail = getComputedStyle(stated, '::before');
 const none = getComputedStyle(plain, '::before');
 const a = stated.getBoundingClientRect(), b = plain.getBoundingClientRect();
 return rail.content === '""' && rail.width === '3px' &&
        rail.backgroundColor !== getComputedStyle(stated).backgroundColor &&
        none.content === 'none' &&
        a.width > 0 && b.width > 0 && a.width === b.width &&
        document.documentElement.scrollWidth <= innerWidth;
 })()`)

	// Hovering a card that opens something answers on its edge, its fill and
	// its shadow, and leaves the card exactly where it was.
	probe := `(() => {
 const card = document.querySelector('#cards .card:has(.card-name a)');
 if (!card) return 'null';
 const s = getComputedStyle(card), r = card.getBoundingClientRect();
 return JSON.stringify({hover: card.matches(':hover'), shadow: s.boxShadow, bg: s.backgroundColor,
   border: s.borderTopColor, left: r.left, top: r.top, width: r.width, height: r.height});
 })()`
	read := func() struct {
		Hover                    bool
		Shadow, Bg, Border       string
		Left, Top, Width, Height float64
	} {
		t.Helper()
		raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](probe))
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Hover                    bool
			Shadow, Bg, Border       string
			Left, Top, Width, Height float64
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	before := read()

	point, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](
		`(() => { const r = document.querySelector('#cards .card:has(.card-name a)').getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`))
	if err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, point[0], point[1]))
	after := read()
	if !after.Hover {
		t.Fatalf("the pointer never reached the card: %+v", after)
	}
	if after.Shadow == before.Shadow || after.Bg == before.Bg || after.Border == before.Border {
		t.Errorf("a card should answer the pointer on its shadow, its fill and its edge: %+v -> %+v", before, after)
	}
	if after.Left != before.Left || after.Top != before.Top || after.Width != before.Width || after.Height != before.Height {
		t.Errorf("a card must not move when it is hovered: %+v -> %+v", before, after)
	}

	// Leaving puts it back.
	do(t, ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, 4, 4))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`(() => { const c = document.querySelector('#cards .card:has(.card-name a)');
 return !c.matches(':hover') && getComputedStyle(c).boxShadow === 'none'; })()`,
		chromedp.WithPollingTimeout(2*time.Second))); err != nil {
		t.Fatalf("a card should settle back when the pointer leaves: %v", err)
	}
}
