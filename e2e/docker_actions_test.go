package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func dockerMutations(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result [][]string
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		var args []string
		if err := json.Unmarshal(scan.Bytes(), &args); err != nil {
			t.Fatal(err)
		}
		if len(args) > 3 && (args[3] == "start" || args[3] == "restart") {
			result = append(result, args)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDockerActionsRequireTheConsoleOriginAndASecondClick(t *testing.T) {
	dir, trace := dockerFixture(t)
	url := startConsole(t, dir)
	cliCommand(t, 0, "plugin", "install", "--id", "docker", "--url", url)
	statePath := os.Getenv("HOSTDECK_E2E_DOCKER_STATE")
	initial, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			if err := os.WriteFile(statePath, initial, 0600); err != nil {
				t.Fatal(err)
			}
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/docker"), chromedp.WaitVisible(chromedp.CSS(`.detail-item .action`)), chromedp.Click(chromedp.CSS(`.detail-groups [data-group="stopped"]`)), chromedp.SendKeys(chromedp.CSS(`.detail-search input`), "worker"))
			before := len(dockerMutations(t, trace))
			resp, err := http.Get(url + "/api/detail/docker")
			if err != nil {
				t.Fatal(err)
			}
			var view struct {
				Items []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				}
			}
			err = json.NewDecoder(resp.Body).Decode(&view)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			id := ""
			for _, it := range view.Items {
				if it.Name == "worker" {
					id = it.ID
				}
			}
			if id == "" {
				t.Fatal("worker missing")
			}
			body, _ := json.Marshal(map[string]string{"item": id, "action": "start"})
			req, _ := http.NewRequest(http.MethodPost, url+"/api/action/docker", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://127.0.0.1:1")
			refused, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, refused.Body)
			refused.Body.Close()
			if refused.StatusCode != http.StatusForbidden || len(dockerMutations(t, trace)) != before {
				t.Fatalf("foreign origin was not refused: HTTP %d", refused.StatusCode)
			}
			doubleClick(t, ctx, `.detail-item .action`)
			armed, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.detail-item .action').classList.contains('armed')`))
			if err != nil || !armed || len(dockerMutations(t, trace)) != before {
				t.Fatalf("double click executed or did not arm: %v %v", armed, err)
			}
			do(t, ctx, chromedp.Focus(chromedp.CSS(`.detail-item .action`)), chromedp.KeyEvent(kb.Enter))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-note:not([hidden])')?.textContent.includes('worker') && document.querySelectorAll('.detail-item').length===0`, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
				t.Fatal(err)
			}
			after := dockerMutations(t, trace)
			if len(after) != before+1 || after[len(after)-1][3] != "start" || after[len(after)-1][len(after[len(after)-1])-1] != strings.Repeat("0", 63)+"2" {
				t.Fatalf("wrong start commands %+v", after)
			}
			ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.detail-search input').value==='worker' && document.querySelector('[data-group="stopped"]').getAttribute('aria-pressed')==='true'`))
			if err != nil || !ok {
				t.Fatalf("action lost filters %v %v", ok, err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-groups [data-group="running"]`)), chromedp.Click(chromedp.CSS(`.detail-item .action`)), chromedp.Click(chromedp.CSS(`.detail-item .action`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-note:not([hidden])')?.textContent.includes('worker') && !document.querySelector('.detail-item .action').disabled`, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
				t.Fatal(err)
			}
			after = dockerMutations(t, trace)
			if len(after) != before+2 || after[len(after)-1][3] != "restart" {
				t.Fatalf("restart was not observed: %+v", after)
			}
		})
	}
}
