package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Diagnostics are opt-in locally and enabled by CI. Record before navigating,
// but write only failed cases, before openBrowser cancels their tabs.
func recordBrowserFailure(t *testing.T, ctx context.Context) {
	t.Helper()
	root := os.Getenv("HOSTDECK_ARTIFACT_DIR")
	if root == "" {
		return
	}
	logs, cancelLogs := context.WithCancel(ctx)
	messages := chromedp.Console(logs)
	var mu sync.Mutex
	var lines []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for message, err := range messages {
			if err != nil {
				return
			}
			line := message.String()
			if len(line) > 4096 {
				line = line[:4096] + " [truncated]"
			}
			mu.Lock()
			if len(lines) == 200 {
				copy(lines, lines[1:])
				lines = lines[:199]
			}
			lines = append(lines, line)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		defer func() { cancelLogs(); <-done }()
		if !t.Failed() {
			return
		}
		name := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, t.Name())
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Logf("artifacts: %v", err)
			return
		}
		dir, err := os.MkdirTemp(root, name+"-")
		if err != nil {
			t.Logf("artifacts: %v", err)
			return
		}
		write := func(name string, data []byte) {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Logf("saving %s: %v", name, err)
			}
		}
		// A case deadline must not prevent a bounded attempt to save its page.
		snapshot, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if html, err := chromedp.Run(snapshot, chromedp.Evaluate[string](`document.documentElement.outerHTML`)); err == nil {
			write("page.html", []byte(html))
		} else {
			t.Logf("capturing page: %v", err)
		}
		if png, err := chromedp.Run(snapshot, chromedp.CaptureScreenshot()); err == nil {
			write("page.png", png)
		} else {
			t.Logf("capturing screenshot: %v", err)
		}
		mu.Lock()
		console := strings.Join(lines, "\n")
		mu.Unlock()
		write("console.log", []byte(console))
		write("case.txt", []byte(fmt.Sprintf("test: %s\nbrowser: %s\n", t.Name(), browsers.version.Product)))
		t.Logf("browser failure artifacts: %s", dir)
	})
}
