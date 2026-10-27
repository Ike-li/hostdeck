package e2e

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestADetailReadThatArrivesWhileAButtonIsArmedDoesNotDisarmIt(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := dir + "/late"
	writePlugin(t, dir, "late", fmt.Sprintf(`schema: 1
name: Late detail
detail:
  exec: {command: [%q, fake-plugin, detail-late, %q]}
actions:
  exec: {command: [%q, fake-plugin, action-late, %q]}
`, exe, marker, exe, marker))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/late"), chromedp.WaitVisible(chromedp.CSS(`.detail-name`)))
	const run = `.detail-actions button:nth-child(1)`
	const stop = `.detail-actions button:nth-child(2)`
	do(t, ctx, chromedp.Click(chromedp.CSS(run)), chromedp.Click(chromedp.CSS(run)), chromedp.Click(chromedp.CSS(stop)))
	until := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker + ".done"); err == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("the late detail read did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if got := text(t, ctx, stop); got != "Confirm Stop?" {
		t.Fatalf("armed button %q", got)
	}
}
