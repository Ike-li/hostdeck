package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

func TestBrowserCasesShareAProcessButKeepStorageAndPermissionsApart(t *testing.T) {
	origin := storageService(t, "isolation")
	container := page(t, `<!doctype html><title>Browser isolation</title>`)
	pid := 0
	firstContext := ""
	for _, name := range []string{"first", "next"} {
		t.Run(name, func(t *testing.T) {
			ctx := openBrowser(t, "en")
			current := chromedp.FromContext(ctx)
			if pid == 0 {
				pid = current.Browser.Process().Pid
				firstContext = string(current.BrowserContextID)
			} else {
				if got := current.Browser.Process().Pid; got != pid {
					t.Fatalf("browser process %d, want shared process %d", got, pid)
				}
				if firstContext == "" || string(current.BrowserContextID) == firstContext {
					t.Fatal("cases need distinct non-default browser contexts")
				}
			}
			do(t, ctx, chromedp.Navigate(container))
			collectStates(t, ctx)
			if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf(`document.body.innerHTML = '<iframe></iframe>'; document.querySelector('iframe').src = %q`, origin+"/start.html"))); err != nil {
				t.Fatal(err)
			}
			state := serviceState(t, ctx, origin, func(siteState) bool { return true })
			if name == "first" {
				if !state.full() {
					t.Fatalf("fixture did not fill each storage kind: %+v", state)
				}
				if _, err := chromedp.CallBrowser(ctx, browser.SetPermission, browser.SetPermissionParams{
					Permission: &browser.PermissionDescriptor{Name: "geolocation"}, Setting: browser.PermissionSettingGranted, Origin: origin, BrowserContextID: current.BrowserContextID,
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				if state.LocalStorage != nil || state.IndexedDB || state.CacheStorage != 0 || state.ServiceWorkers != 0 || state.HTTPCache || state.Cookie {
					t.Fatalf("previous case leaked storage: %+v", state)
				}
				permission, err := chromedp.Run(ctx, chromedp.Evaluate[string](`navigator.permissions.query({name:'geolocation'}).then(p=>p.state)`, chromedp.EvalAwaitPromise))
				if err != nil || permission == "granted" {
					t.Fatalf("previous permission leaked: %q %v", permission, err)
				}
			}
		})
	}
}

func TestABrowserThatExistsButCannotStartFailsInsteadOfSkipping(t *testing.T) {
	if testing.Short() {
		t.Skip("console integration test")
	}
	if os.Getenv("HOSTDECK_TEST_BROWSER_FAILURE_PROBE") == "1" {
		openBrowser(t, "en")
		return
	}
	broken := filepath.Join(t.TempDir(), "broken-browser.exe")
	if err := os.WriteFile(broken, []byte("not an executable browser\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestABrowserThatExistsButCannotStartFailsInsteadOfSkipping$")
	cmd.Env = append(os.Environ(), "HOSTDECK_BROWSER="+broken, "HOSTDECK_E2E=0", "HOSTDECK_TEST_BROWSER_FAILURE_PROBE=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err == nil || !strings.Contains(string(out), "starting the browser") {
		t.Fatalf("startup failure should fail, not skip: %s %v", out, err)
	}
}
