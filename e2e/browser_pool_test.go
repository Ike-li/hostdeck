package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

var noBrowser = errors.New("Chrome or Edge was not found")

// Only the process is shared. Each openBrowser owns and disposes a fresh
// browser context; cancellation of one case never owns the suite process.
var browsers struct {
	once          sync.Once
	ctx           context.Context
	stopBrowser   context.CancelFunc
	stopAllocator context.CancelFunc
	version       browser.GetVersionResult
	err           error
}

func findBrowser() (string, error) {
	if configured := os.Getenv("HOSTDECK_BROWSER"); configured != "" {
		path, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("HOSTDECK_BROWSER: %w", err)
		}
		return path, nil
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}
	case "windows":
		candidates = []string{"chrome", `C:\Program Files\Google\Chrome\Application\chrome.exe`, `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`, filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`), "msedge", `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, `C:\Program Files\Microsoft\Edge\Application\msedge.exe`}
	default:
		candidates = []string{"google-chrome", "chromium", "chromium-browser", "chrome", "msedge"}
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", noBrowser
}

func startBrowser() {
	path, err := findBrowser()
	if err != nil {
		browsers.err = err
		return
	}
	opts := append(slices.Clone(chromedp.DefaultExecAllocatorOptions[:]), chromedp.ExecPath(path))
	alloc, stopAllocator := chromedp.NewExecAllocator(context.Background(), opts...)
	browsers.stopAllocator = stopAllocator
	browsers.ctx, browsers.stopBrowser = chromedp.NewContext(alloc)
	// The first Run owns the process lifetime. Do not give that Run a
	// short-lived child context, which would kill the shared process later.
	timer := time.AfterFunc(30*time.Second, browsers.stopBrowser)
	browsers.err = chromedp.Do(browsers.ctx)
	timer.Stop()
	if browsers.err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(browsers.ctx, 10*time.Second)
	defer cancel()
	browsers.version, browsers.err = chromedp.CallBrowser(ctx, browser.GetVersion, cdp.Empty{})
	if browsers.err == nil {
		fmt.Fprintf(os.Stderr, "hostdeck e2e browser: %s (%s)\n", browsers.version.Product, path)
	}
}

func closeBrowser() {
	if browsers.stopBrowser != nil {
		browsers.stopBrowser()
	}
	if browsers.stopAllocator != nil {
		browsers.stopAllocator()
	}
}

// openBrowser starts a fresh isolated case in the suite's one browser process.
func openBrowser(t *testing.T, langs string) context.Context {
	t.Helper()
	browsers.once.Do(startBrowser)
	if browsers.err != nil {
		if errors.Is(browsers.err, noBrowser) && os.Getenv("HOSTDECK_E2E") != "1" {
			t.Skip(browsers.err)
		}
		t.Fatalf("starting the browser: %v", browsers.err)
	}
	tab, cancelTab := chromedp.NewContext(browsers.ctx, chromedp.WithNewBrowserContext())
	ctx, cancelSteps := context.WithTimeout(tab, time.Minute)
	t.Cleanup(func() { cancelSteps(); cancelTab() })
	if err := chromedp.Do(ctx); err != nil {
		t.Fatalf("opening a browser case: %v", err)
	}
	if _, err := chromedp.Call(ctx, emulation.SetUserAgentOverride, emulation.SetUserAgentOverrideParams{
		UserAgent: browsers.version.UserAgent, AcceptLanguage: langs,
	}); err != nil {
		t.Fatal(err)
	}
	return ctx
}
