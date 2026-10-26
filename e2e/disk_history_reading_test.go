package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestADiskReadingThatArrivesDuringConfirmationKeepsTheArmedButton(t *testing.T) {
	url := startConsole(t, diskReadingFixture(t))
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/renamed-history"), chromedp.WaitVisible(chromedp.CSS(`.disk-total`)))
	watch, cancel := context.WithCancel(ctx)
	defer cancel()
	events := chromedp.Events(watch, fetch.RequestPaused)
	pausedResponses := make(chan fetch.EventRequestPaused, 1)
	go func() {
		for event, err := range events {
			if err == nil {
				pausedResponses <- event
			}
			return
		}
	}()
	if _, err := chromedp.Call(ctx, fetch.Enable, fetch.EnableParams{Patterns: []*fetch.RequestPattern{{URLPattern: "*/api/disk-history/*", RequestStage: fetch.RequestStageResponse}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = chromedp.Call(ctx, fetch.Disable, cdp.Empty{}) })
	do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
	var paused fetch.EventRequestPaused
	select {
	case event := <-pausedResponses:
		paused = event
	case <-time.After(5 * time.Second):
		t.Fatal("the refreshed history response never reached the browser")
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`.disk-actions [data-action="scan"]`)), chromedp.WaitVisible(chromedp.CSS(`.disk-actions .armed`)))
	if _, err := chromedp.Call(ctx, fetch.ContinueResponse, fetch.ContinueResponseParams{RequestID: paused.RequestID}); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.disk-view').getAttribute('aria-busy')==='false'`)); err != nil {
		t.Fatal(err)
	}
	armed, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.disk-actions [data-action="scan"]').classList.contains('armed')`))
	if err != nil || !armed {
		t.Fatalf("a completed reading replaced the armed action: %v %v", armed, err)
	}
}

func diskReadingFixture(t *testing.T) string {
	t.Helper()
	dir, root := t.TempDir(), t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "renamed-history")
	if err := os.MkdirAll(filepath.Join(pluginDir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	writePlugin(t, dir, "renamed-history", "schema: 1\nname: {en: Disk history, zh-CN: 磁盘变化}\ndetail:\n  exec: {command: ['@hostdeck', disk-history, detail]}\nactions:\n  exec: {command: ['@hostdeck', disk-history, action]}\n")
	config, _ := json.Marshal(map[string]any{"roots": []string{root}})
	if err := os.WriteFile(filepath.Join(pluginDir, "config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	var scans []any
	for i := 0; i < 6; i++ {
		scans = append(scans, map[string]any{"time": fmt.Sprintf("2026-10-26T%02d:00:00Z", 7+i), "allocated": true, "unreadable": 8, "sizes": map[string]any{
			".":                                    map[string]any{"bytes": 100_000_000_000 + int64(i)*1_000_000_000, "partial": true, "missed": "same"},
			"Library":                              map[string]any{"bytes": 50_000_000_000 + int64(i)*500_000_000},
			filepath.Join("Library", "Containers"): map[string]any{"bytes": 20_000_000_000 + int64(i)*200_000_000},
			"code":                                 map[string]any{"bytes": 30_000_000_000 - int64(i)*100_000_000},
		}})
	}
	data, _ := json.Marshal(map[string]any{"version": 1, "tracking": false, "scans": map[string]any{root: scans}})
	if err := os.WriteFile(filepath.Join(pluginDir, "data", "history.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiskHistoryLetsReadersFollowTheNumbersThroughTimeAndIntoADirectory(t *testing.T) {
	dir := diskReadingFixture(t)
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/renamed-history"), chromedp.WaitVisible(chromedp.CSS(`.disk-history .disk-total`)))
			if got := text(t, ctx, `.disk-total`); got != "105.0 GB" {
				t.Fatalf("current size %q", got)
			}
			if got := text(t, ctx, `.disk-change`); got != "+5.00 GB" {
				t.Fatalf("change %q", got)
			}
			check := func(js string) {
				t.Helper()
				ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
				if err != nil || !ok {
					t.Fatalf("%s: %v %v", js, ok, err)
				}
			}
			check(`document.querySelectorAll('.disk-directory tbody tr').length===2 && !document.querySelector('.disk-directory').textContent.includes('Containers') && document.querySelectorAll('.disk-line .disk-point').length===6 && document.querySelectorAll('.disk-bars .disk-bar').length===5`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.disk-bar`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.disk-change')?.textContent==='+1.00 GB'`)); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.disk-clear-period`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('.disk-clear-period') && document.querySelector('.disk-change')?.textContent==='+5.00 GB'`)); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`button[data-path="Library"]`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.disk-total')?.textContent==='52.5 GB' && document.querySelector('.disk-directory')?.textContent.includes('Containers')`)); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`[data-range="7d"]`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('[data-range="7d"]').getAttribute('aria-pressed')==='true' && document.querySelector('.disk-view').getAttribute('aria-busy')==='false'`)); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			check(`document.querySelector('[data-range="7d"]').getAttribute('aria-pressed')==='true' && document.querySelector('.disk-crumbs').textContent.includes('Library')`)
			for _, width := range []int64{1280, 768, 520} {
				for _, theme := range []string{"light", "dark"} {
					do(t, ctx, chromedp.EmulateViewport(width, 900))
					if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: theme}}}); err != nil {
						t.Fatal(err)
					}
					check(`document.documentElement.scrollWidth<=innerWidth && document.querySelector('.disk-history').scrollWidth<=document.querySelector('.disk-history').clientWidth+1 && [...document.querySelectorAll('.disk-history button')].filter(b=>!b.hidden).every(b=>b.getBoundingClientRect().height>=44)`)
				}
			}
			do(t, ctx, chromedp.Focus(chromedp.CSS(`.disk-line svg`)), chromedp.KeyEvent(kb.ArrowRight))
			check(`document.activeElement.matches('.disk-line svg') && document.querySelector('.disk-line .disk-chart-reading').textContent.includes('50.5 GB')`)
			do(t, ctx, chromedp.Focus(chromedp.CSS(`.disk-bars svg`)), chromedp.KeyEvent(kb.Enter), chromedp.WaitVisible(chromedp.CSS(`.disk-clear-period`)))
			check(`document.activeElement.matches('.disk-bars svg')`)
			do(t, ctx, chromedp.KeyEvent(kb.Escape))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('.disk-clear-period')`)); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.EmulateViewport(1280, 900))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('.side-menu').inert`)); err != nil {
				t.Fatal(err)
			}
			next := "en"
			if lang == "en" {
				next = "zh-CN"
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`[data-lang="`+next+`"]`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.documentElement.lang===%q && document.querySelector('.disk-crumbs').textContent.includes('Library') && document.querySelector('[data-range="7d"]').getAttribute('aria-pressed')==='true'`, next))); err != nil {
				t.Fatal(err)
			}
		})
	}
}
