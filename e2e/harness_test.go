package e2e

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestShortModeDoesNotBuildAConsoleOrStartABrowser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.short", "-test.run=^(TestTheBinaryExposesConnectionCommandsWithoutStartingAnotherServer|TestABrowserInEnglishGetsEnglish)$")
	cmd.Env = append(os.Environ(), "PATH=", "HOSTDECK_BROWSER="+filepath.Join(t.TempDir(), "missing-browser"), "HOSTDECK_E2E=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("short mode should need neither go nor a browser: %s %v", out, err)
	}
}

func TestAFailedBrowserCaseSavesItsPageScreenshotAndConsole(t *testing.T) {
	if testing.Short() {
		t.Skip("browser failure diagnostics")
	}
	if os.Getenv("HOSTDECK_TEST_FAILURE_ARTIFACT_PROBE") == "1" {
		ctx := openBrowser(t, "en")
		do(t, ctx, chromedp.Navigate(page(t, `<!doctype html><h1>Failure fixture</h1>`)))
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`console.error('fixture console failure')`)); err != nil {
			t.Fatal(err)
		}
		t.Fatal("intentional browser failure")
	}
	// Use the same optional-browser policy as other browser tests.
	openBrowser(t, "en")
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAFailedBrowserCaseSavesItsPageScreenshotAndConsole$")
	cmd.Env = append(os.Environ(), "HOSTDECK_ARTIFACT_DIR="+dir, "HOSTDECK_E2E=1", "HOSTDECK_TEST_FAILURE_ARTIFACT_PROBE=1")
	out, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil || !strings.Contains(string(out), "intentional browser failure") {
		t.Fatalf("unexpected probe result: %s %v", out, err)
	}
	var html, screenshot, console bool
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch filepath.Ext(path) {
		case ".html":
			html = bytes.Contains(data, []byte("Failure fixture"))
		case ".png":
			config, err := png.DecodeConfig(bytes.NewReader(data))
			screenshot = err == nil && config.Width > 0 && config.Height > 0
		case ".log":
			console = bytes.Contains(data, []byte("fixture console failure"))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !html || !screenshot || !console {
		t.Fatalf("failure artifacts missing: HTML=%v screenshot=%v console=%v; child: %s", html, screenshot, console, out)
	}
}
