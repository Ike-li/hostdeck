package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/tidwall/gjson"
)

func TestDiskHistoryInstallsScansTracksAndShowsSavedChangesInBothLanguages(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			dir, target := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(target, "目录"), 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(target, "目录", "file")
			if err := os.WriteFile(file, bytes.Repeat([]byte("a"), 4096), 0600); err != nil {
				t.Fatal(err)
			}
			url := startConsole(t, dir)
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-available .catalog-item[data-id="disk-history"] .btn`)))
			const add = `#catalog-available .catalog-item[data-id="disk-history"] .btn`
			do(t, ctx, chromedp.Click(chromedp.CSS(add)), chromedp.Click(chromedp.CSS(add)), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-item[data-id="disk-history"]`)))
			pluginDir := filepath.Join(dir, "plugins", "disk-history")
			config, _ := json.Marshal(map[string]any{"roots": []string{target}})
			if err := os.WriteFile(filepath.Join(pluginDir, "config.json"), config, 0600); err != nil {
				t.Fatal(err)
			}
			cliCommand(t, 0, "plugin", "reload", "--url", url)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#nav .nav-item[data-id="disk-history"]`)), chromedp.WaitVisible(chromedp.CSS(`.disk-history .btn.action`)))
			if _, err := os.Stat(filepath.Join(pluginDir, "data")); !os.IsNotExist(err) {
				t.Fatalf("installation/read scanned or wrote data: %v", err)
			}
			resp, err := http.Get(url + "/api/detail/disk-history")
			if err != nil {
				t.Fatal(err)
			}
			var initial struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			}
			err = json.NewDecoder(resp.Body).Decode(&initial)
			resp.Body.Close()
			if err != nil || len(initial.Items) != 1 {
				t.Fatalf("initial detail %+v, %v", initial, err)
			}
			body, _ := json.Marshal(map[string]string{"item": initial.Items[0].ID, "action": "scan"})
			req, _ := http.NewRequestWithContext(context.Background(), "POST", url+"/api/action/disk-history", bytes.NewReader(body))
			req.Header.Set("Origin", "https://foreign.example")
			refused, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			refused.Body.Close()
			if refused.StatusCode != http.StatusForbidden {
				t.Fatalf("foreign scan: HTTP %d", refused.StatusCode)
			}
			const scan = `.disk-actions .btn.action[data-action="scan"]`
			do(t, ctx, chromedp.Click(chromedp.CSS(scan)), chromedp.WaitVisible(chromedp.CSS(scan+`.armed`)), chromedp.Click(chromedp.CSS(scan)))
			historyFile := filepath.Join(pluginDir, "data", "history.json")
			waitDiskSnapshots(t, historyFile, target, 1)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			waitDiskValue(t, ctx, "4.10 KB")
			if err := os.WriteFile(file, bytes.Repeat([]byte("a"), 8192), 0600); err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(scan)), chromedp.Click(chromedp.CSS(scan)))
			waitDiskSnapshots(t, historyFile, target, 2)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			waitDiskValue(t, ctx, "8.19 KB")
			for _, width := range []int{1280, 520} {
				do(t, ctx, chromedp.EmulateViewport(int64(width), 860))
				fits, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.documentElement.scrollWidth <= innerWidth && [...document.querySelectorAll('.disk-history')].every(e => e.scrollWidth <= e.clientWidth + 1)`))
				if err != nil || !fits {
					t.Fatalf("%s at %dpx: overflow=%v, %v", lang, width, !fits, err)
				}
			}
			do(t, ctx, chromedp.EmulateViewport(1280, 860))
			trackLabel, pauseLabel := "Start hourly tracking", "Pause hourly tracking"
			if lang == "zh-CN" {
				trackLabel, pauseLabel = "开始每小时跟踪", "暂停每小时跟踪"
			}
			clickDiskAction(t, ctx, trackLabel)
			waitDiskSnapshots(t, historyFile, target, 3)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			clickDiskAction(t, ctx, pauseLabel)
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(historyFile)
				if gjson.GetBytes(data, "tracking").Exists() && !gjson.GetBytes(data, "tracking").Bool() {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			data, err := os.ReadFile(historyFile)
			if err != nil || gjson.GetBytes(data, "tracking").Bool() {
				t.Fatalf("tracking did not pause: %s, %v", data, err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`#nav-overview`)), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="disk-history"] .card-value`)))
			if got := text(t, ctx, `.card[data-id="disk-history"] .card-value`); got != "+4.10 KB" {
				t.Fatalf("change card %q", got)
			}
			cliCommand(t, 0, "plugin", "disable", "--url", url, "--id", "disk-history")
			if _, err := os.Stat(filepath.Join(dir, "plugins", ".disk-history", "data", "history.json")); err != nil {
				t.Fatalf("disable lost history: %v", err)
			}
			cliCommand(t, 0, "plugin", "enable", "--url", url, "--id", "disk-history")
			// Reacquire the native writer before exercising the other directory
			// mutation: trash must stop it before moving the folder on Windows.
			request, _ := http.NewRequestWithContext(context.Background(), "POST", url+"/api/action/disk-history", bytes.NewReader(body))
			request.Header.Set("Origin", url)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("rescan before trash: HTTP %d", response.StatusCode)
			}
			waitDiskSnapshots(t, historyFile, target, 4)
			request, _ = http.NewRequestWithContext(context.Background(), "POST", url+"/api/plugins/disk-history/trash", nil)
			request.Header.Set("Origin", url)
			response, err = http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			var trash struct {
				Folder string `json:"folder"`
			}
			err = json.NewDecoder(response.Body).Decode(&trash)
			response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK || trash.Folder == "" {
				t.Fatalf("trash %+v: HTTP %d, %v", trash, response.StatusCode, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "plugins", ".trash", trash.Folder, "data", "history.json")); err != nil {
				t.Fatalf("trash lost history: %v", err)
			}
		})
	}
}

func waitDiskSnapshots(t *testing.T, file, target string, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(file)
		var state struct {
			Scans map[string][]json.RawMessage `json:"scans"`
		}
		if json.Unmarshal(data, &state) == nil && len(state.Scans[target]) == count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(file)
	t.Fatalf("wanted %d snapshots: %s", count, data)
}

func waitDiskValue(t *testing.T, ctx context.Context, value string) {
	t.Helper()
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`document.querySelector('.disk-total')?.textContent === %q`, value))); err != nil {
		t.Fatal(err)
	}
}

func clickDiskAction(t *testing.T, ctx context.Context, label string) {
	t.Helper()
	selector := fmt.Sprintf(`(() => [...document.querySelectorAll('.disk-actions .btn.action')].find(b => b.textContent === %q))()`, label)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](selector+` !== undefined`)); err != nil {
		t.Fatal(err)
	}
	// Keep the second click on the same element after its confirmation label changes.
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => { const b = `+selector+`; b.click(); b.click(); })()`)); err != nil {
		t.Fatal(err)
	}
}
