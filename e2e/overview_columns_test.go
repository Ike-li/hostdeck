package e2e

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestTheOverviewPutsAtMostFourCardsInARow(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		writePlugin(t, dir, id, fmt.Sprintf("schema: 1\nname: %s\npage: {link: 'https://%s.example/'}\n", id, id))
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="e"]`)))
	// Below the width where four fit, the cards keep their room instead of
	// squeezing into four columns.
	for _, c := range []struct{ width, want int }{{1100, 3}, {1280, 4}, {1512, 4}, {1728, 4}, {2964, 4}} {
		do(t, ctx, chromedp.EmulateViewport(int64(c.width), 900))
		got, err := chromedp.Run(ctx, chromedp.Evaluate[int](`(() => {
 const items = [...document.querySelector('#cards').children].filter(e => e.checkVisibility());
 const top = Math.round(items[0].getBoundingClientRect().top);
 return items.filter(e => Math.round(e.getBoundingClientRect().top) === top).length;
 })()`))
		if err != nil || got != c.want {
			t.Errorf("at %dpx the first row holds %d cards, want %d: %v", c.width, got, c.want, err)
		}
	}
}
