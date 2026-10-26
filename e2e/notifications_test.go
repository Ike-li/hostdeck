package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// setNotifications stands in for the user's answer to the browser's prompt,
// which a headless browser cannot show.
func setNotifications(t *testing.T, ctx context.Context, origin string, setting browser.PermissionSetting) {
	t.Helper()
	if _, err := chromedp.CallBrowser(ctx, browser.SetPermission, browser.SetPermissionParams{
		Permission:       &browser.PermissionDescriptor{Name: "notifications"},
		Setting:          setting,
		Origin:           origin,
		BrowserContextID: chromedp.FromContext(ctx).BrowserContextID,
	}); err != nil {
		t.Fatal(err)
	}
}

func waitText(t *testing.T, ctx context.Context, css, want string) {
	t.Helper()
	// An interval, not animation frames: the window opened as a tab puts the
	// console in the background, where no frame is drawn. A bounded wait leaves
	// the case time to say what the element says instead.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector(%q)?.textContent.includes(%q)`, css, want), chromedp.WithPollingInterval(100*time.Millisecond), chromedp.WithPollingTimeout(20*time.Second))); err != nil {
		t.Fatalf("%s never said %q; it says %q", css, want, text(t, ctx, css))
	}
}

// A browser keys notifications by the top-level page: the console's origin
// for a framed page, the service's own port for its service worker. Each step
// of the dialog asks, and says the answer of, the one address it covers.
func TestEachNotificationStepAsksForTheAddressItCovers(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, page(t, servicePage)))
	console := startConsole(t, dir)
	served := originOf(t, sidebarURL(t, console, "svc"))
	ctx := openBrowser(t, "en-US,en;q=0.9")
	setNotifications(t, ctx, served, browser.PermissionSettingDenied)

	do(t, ctx,
		chromedp.Navigate(console+"/#/svc"),
		chromedp.WaitVisible(chromedp.CSS("#frames iframe")),
		chromedp.Click(chromedp.CSS("#service-more .more-trigger")),
		chromedp.Click(chromedp.CSS("#service-notify")),
		chromedp.WaitVisible(chromedp.CSS("#notify-page-allow")),
	)
	if title := text(t, ctx, "#editor-title"); title != "Notifications from Service" {
		t.Errorf("dialog title %q", title)
	}
	waitText(t, ctx, "#notify-page-status", "Not allowed yet")

	// A headless browser cannot show the prompt and refuses at once, which is
	// enough to see that the click asked for the console's own address.
	do(t, ctx, chromedp.Click(chromedp.CSS("#notify-page-allow")))
	waitText(t, ctx, "#notify-page-status", console)

	do(t, ctx, chromedp.Click(chromedp.CSS("#notify-push-allow")))
	waitText(t, ctx, "#notify-push-status", "Blocked")
	if status := text(t, ctx, "#notify-push-status"); !strings.Contains(status, served) {
		t.Errorf("the blocked push step says %q, want it to name %s, the address to allow", status, served)
	}

	setNotifications(t, ctx, served, browser.PermissionSettingGranted)
	waitText(t, ctx, "#notify-push-status", "On")

	do(t, ctx, chromedp.Click(chromedp.CSS("#notify-close")))
	// The dialog says it closed in a task of its own, after the click returns.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('#page-editor').open && document.activeElement === document.querySelector('#service-more .more-trigger')`)); err != nil {
		t.Errorf("after Close the dialog is still open or focus is not on More actions: %v", err)
	}
}

func TestClosingTheNotificationsWindowUnansweredStopsWaiting(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, page(t, servicePage)))
	console := startConsole(t, dir)
	ctx := openBrowser(t, "en-US,en;q=0.9")

	do(t, ctx,
		chromedp.Navigate(console+"/#/svc"),
		chromedp.WaitVisible(chromedp.CSS("#frames iframe")),
		chromedp.Click(chromedp.CSS("#service-more .more-trigger")),
		chromedp.Click(chromedp.CSS("#service-notify")),
		chromedp.Click(chromedp.CSS("#notify-push-allow")),
	)
	waitText(t, ctx, "#notify-push-status", "Waiting")
	// The user closes the small window instead of answering. Opening its name
	// again with no address reaches the same window without navigating it.
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`open('', 'hostdeck-notifications').close()`)); err != nil {
		t.Fatal(err)
	}
	waitText(t, ctx, "#notify-push-status", "Not checked yet")
}

// A blocked address cannot be asked again from a page. Each step says where in
// the browser to turn notifications on for its address, and notices by itself
// when they are.
func TestABlockedStepSaysWhereToTurnNotificationsOnAndNoticesWhenTheyAre(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, page(t, servicePage)))
	console := startConsole(t, dir)
	served := originOf(t, sidebarURL(t, console, "svc"))
	ctx := openBrowser(t, "en-US,en;q=0.9")
	setNotifications(t, ctx, console, browser.PermissionSettingDenied)
	setNotifications(t, ctx, served, browser.PermissionSettingDenied)

	do(t, ctx,
		chromedp.Navigate(console+"/#/svc"),
		chromedp.WaitVisible(chromedp.CSS("#frames iframe")),
		chromedp.Click(chromedp.CSS("#service-more .more-trigger")),
		chromedp.Click(chromedp.CSS("#service-notify")),
		chromedp.WaitVisible(chromedp.CSS("#notify-push-allow")),
	)
	waitText(t, ctx, "#notify-page-status", "address bar")
	if status := text(t, ctx, "#notify-page-status"); !strings.Contains(status, console) {
		t.Errorf("the blocked page step says %q, want it to name %s", status, console)
	}
	if hidden, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('#notify-page-allow').hidden`)); err != nil || !hidden {
		t.Errorf("a blocked page step still offers to ask (%v)", err)
	}
	setNotifications(t, ctx, console, browser.PermissionSettingGranted)
	waitText(t, ctx, "#notify-page-status", "On")

	do(t, ctx, chromedp.Click(chromedp.CSS("#notify-push-allow")))
	waitText(t, ctx, "#notify-push-status", "address bar")
	host := strings.TrimPrefix(served, "http://")
	if label := text(t, ctx, "#notify-push-allow"); label != "Open "+host {
		t.Errorf("the blocked push step's button says %q, want Open %s", label, host)
	}
	// The user closes the window and comes back to it later from the dialog.
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`open('', 'hostdeck-notifications').close()`)); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS("#notify-push-allow")))
	// The click says it is waiting; the window then reads the address still
	// blocked, and waits for it to be turned on.
	waitText(t, ctx, "#notify-push-status", "address bar")
	setNotifications(t, ctx, served, browser.PermissionSettingGranted)
	waitText(t, ctx, "#notify-push-status", "On")
}
