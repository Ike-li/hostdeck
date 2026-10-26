package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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
	config := `{"version":1,"tasks":[{"id":"backup","name":"Backup 中文","enabled":false,"schedule":{"cron":"0 2 * * *","timezone":"Asia/Shanghai"},"exec":{"command":["missing-task-program","argument with spaces"]}}]}`
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
				ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.detail-name').textContent==='Backup 中文' && document.querySelector('.detail-view').textContent.includes('Asia/Shanghai') && document.querySelector('.detail-view').textContent.includes('missing-task-program') && document.querySelectorAll('.detail-actions button').length===0 && document.documentElement.scrollWidth<=innerWidth`))
				if err != nil || !ok {
					t.Fatalf("configuration at %dpx: %v %v", width, ok, err)
				}
			}
		})
	}
	b, err := os.ReadFile(filepath.Join(p, "data/state.json"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var state struct{ Runs []any }
	if err := json.Unmarshal(b, &state); err != nil || len(state.Runs) != 0 {
		t.Fatalf("reading ran a task: %s %v", b, err)
	}
}

func TestEnabledScheduledTasksRunWithoutAnOpenPage(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "plugins")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := catalog.Add(examples.Files, root, "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	task := map[string]any{"id": "help", "name": "Scheduled help", "enabled": false, "schedule": map[string]any{"every": "1m"}, "exec": map[string]any{"command": []string{binary, "help"}}, "timeout": "10s"}
	config := map[string]any{"version": 1, "tasks": []any{task}}
	write := func() {
		b, _ := json.Marshal(config)
		if err := os.WriteFile(filepath.Join(p, "tasks.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	url := startConsole(t, dir)
	task["enabled"] = true
	write()
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/plugins/reload", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", url)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reload status %d", response.StatusCode)
	}
	for {
		b, err := os.ReadFile(filepath.Join(p, "data/state.json"))
		if err != nil {
			t.Fatal(err)
		}
		var s struct {
			Tasks map[string]struct {
				Anchor time.Time `json:"anchor_at"`
			}
			Runs []struct {
				Trigger, Status string
				Scheduled       time.Time `json:"scheduled_at"`
			}
		}
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		if len(s.Runs) > 0 {
			r := s.Runs[0]
			if r.Status == "succeeded" {
				if r.Trigger != "scheduled" || !r.Scheduled.Equal(s.Tasks["help"].Anchor.Add(time.Minute)) {
					t.Fatalf("scheduled reading %+v", s)
				}
				break
			}
			if r.Status != "starting" && r.Status != "running" {
				t.Fatalf("scheduled failure %s", b)
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no background run: %s", b)
		case <-time.After(100 * time.Millisecond):
		}
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
			before := 0
			if b, err := os.ReadFile(filepath.Join(p, "data/state.json")); err == nil {
				var s struct{ Runs []any }
				json.Unmarshal(b, &s)
				before = len(s.Runs)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(".detail-actions button")), chromedp.Click(chromedp.CSS(".detail-actions button")))
			until := time.Now().Add(10 * time.Second)
			for {
				state, err := os.ReadFile(filepath.Join(p, "data/state.json"))
				if err == nil {
					var s struct {
						Runs []struct{ Status, Trigger string }
					}
					if json.Unmarshal(state, &s) == nil && len(s.Runs) > before {
						last := s.Runs[len(s.Runs)-1]
						if last.Status == "succeeded" && last.Trigger == "manual" {
							break
						}
					}
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
