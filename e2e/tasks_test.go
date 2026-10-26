package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/chromedp/chromedp"
)

func TestScheduledTaskConfigurationIsReadableInBothLanguagesAndNarrowWindows(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.Mkdir(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := catalog.Add(examples.Files, pluginDir, "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	config := `{"version":1,"tasks":[{"id":"backup","name":"Backup 中文","enabled":true,"schedule":{"cron":"0 2 * * *","timezone":"Asia/Shanghai"},"exec":{"command":["missing-task-program","argument with spaces"]}}]}`
	if err := os.WriteFile(filepath.Join(p, "tasks.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/scheduled-tasks"), chromedp.WaitVisible(chromedp.CSS(`.detail-item`)))
			for _, width := range []int64{1280, 520} {
				do(t, ctx, chromedp.EmulateViewport(width, 800))
				ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.detail-name').textContent==='Backup 中文' && document.querySelector('.detail-view').textContent.includes('Asia/Shanghai') && document.querySelector('.detail-view').textContent.includes('missing-task-program') && document.querySelectorAll('.detail-actions button').length===1 && document.documentElement.scrollWidth<=innerWidth`))
				if err != nil || !ok {
					t.Fatalf("configuration at %dpx: %v %v", width, ok, err)
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(p, "data")); !os.IsNotExist(err) {
		t.Fatalf("preview created runtime data: %v", err)
	}
}

func TestAScheduledTaskCanBeRunFromTheBrowserAndItsRawLogIsShown(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "plugins")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := catalog.Add(examples.Files, root, "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"version": 1, "tasks": []any{map[string]any{"id": "help", "name": "Local help 中文", "enabled": true, "schedule": map[string]any{"every": "1m"}, "exec": map[string]any{"command": []string{binary, "help"}}, "timeout": "5s"}}}
	b, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(p, "tasks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/scheduled-tasks"), chromedp.WaitVisible(chromedp.CSS(".detail-item")))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-actions button')!==null`, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(".detail-actions button")), chromedp.Click(chromedp.CSS(".detail-actions button")))
			until := time.Now().Add(10 * time.Second)
			for {
				state, err := os.ReadFile(filepath.Join(p, "data/state.json"))
				if err == nil && strings.Contains(string(state), `"status":"succeeded"`) {
					break
				}
				if time.Now().After(until) {
					t.Fatalf("no successful run: %s %v", state, err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS("#service-reload")))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-view').textContent.includes('run-') && document.querySelector('.detail-log pre')?.textContent.includes('commands')`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
				t.Fatal(err)
			}
		})
	}
}
