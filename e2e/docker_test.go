package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func fakeDockerCLI(args []string) {
	if len(args) < 4 || args[0] != "--host" || args[1] != "unix:///hostdeck-fixture.sock" || args[2] != "container" {
		fmt.Fprint(os.Stderr, "unexpected Docker fixture target")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Getenv("HOSTDECK_E2E_DOCKER_STATE"))
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
	if trace := os.Getenv("HOSTDECK_E2E_DOCKER_TRACE"); trace != "" {
		f, err := os.OpenFile(trace, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(1)
		}
		json.NewEncoder(f).Encode(args)
		f.Close()
	}
	switch args[3] {
	case "ls":
		for _, r := range rows {
			fmt.Println(r["id"])
		}
	case "inspect":
		for _, r := range rows {
			if slices.Contains(args, r["id"].(string)) {
				json.NewEncoder(os.Stdout).Encode(r)
			}
		}
	case "start", "restart":
		id := args[len(args)-1]
		found := false
		for _, row := range rows {
			if row["id"] == id {
				row["status"] = "running"
				row["exitCode"] = 0
				found = true
			}
		}
		if !found {
			fmt.Fprint(os.Stderr, "fixture container missing")
			os.Exit(1)
		}
		encoded, _ := json.Marshal(rows)
		if err := os.WriteFile(os.Getenv("HOSTDECK_E2E_DOCKER_STATE"), encoded, 0600); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(id)
	case "logs":
		fmt.Println("2026-10-26T00:00:00Z stdout line 中文")
		fmt.Fprintln(os.Stderr, "2026-10-26T00:00:01Z stderr line")
	default:
		fmt.Fprint(os.Stderr, "unexpected Docker fixture command")
		os.Exit(2)
	}
}

func dockerFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	name := "docker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), content, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "unix:///hostdeck-fixture.sock")
	state := filepath.Join(dir, "state.json")
	trace := filepath.Join(dir, "trace.jsonl")
	rows := []map[string]any{}
	for i, s := range []string{"running", "exited", "exited", "paused"} {
		exit := 0
		if i == 2 {
			exit = 7
		}
		rows = append(rows, map[string]any{"id": fmt.Sprintf("%064x", i+1), "name": []string{"/web-中文", "/worker", "/broken", "/paused"}[i], "status": s, "exitCode": exit, "image": "example/image:1", "restartPolicy": map[string]any{"Name": "unless-stopped"}, "ports": map[string]any{"80/tcp": []map[string]string{{"HostIp": "127.0.0.1", "HostPort": "8080"}}}, "mounts": []map[string]any{{"Type": "bind", "Source": strings.Repeat("/long-path", 25), "Destination": "/data", "RW": true}}})
	}
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(state, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOSTDECK_E2E_DOCKER_STATE", state)
	t.Setenv("HOSTDECK_E2E_DOCKER_TRACE", trace)
	return dir, trace
}

func TestTheDockerPluginInstallsAndFiltersNativeContainerReadings(t *testing.T) {
	dir, trace := dockerFixture(t)
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			if lang == "en" {
				do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-available .catalog-item[data-id="docker"] .btn`)), chromedp.Click(chromedp.CSS(`#catalog-available .catalog-item[data-id="docker"] .btn`)), chromedp.Click(chromedp.CSS(`#catalog-available .catalog-item[data-id="docker"] .btn`)))
				do(t, ctx, chromedp.WaitVisible(chromedp.CSS(`#nav .nav-item[data-id="docker"]`)))
			}
			do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="docker"] .card-value`)))
			if got := text(t, ctx, `.card[data-id="docker"] .card-value`); got != "1" {
				t.Fatalf("running count %q", got)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.card[data-id="docker"] .card-name a`)), chromedp.WaitVisible(chromedp.CSS(`.detail-item`)))
			check := func(js string) {
				t.Helper()
				ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
				if err != nil || !ok {
					debug, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`JSON.stringify([...document.querySelectorAll(".detail-name")].map(n=>n.textContent))`))
					t.Fatalf("%s: %v %v, names %s", js, ok, err, debug)
				}
			}
			check(`document.querySelectorAll('.detail-item').length===4 && document.querySelector('.detail-name').textContent==='broken' && document.querySelector('.detail-view').textContent.includes('127.0.0.1:8080')`)
			for _, width := range []int64{1280, 768, 520} {
				for _, theme := range []string{"light", "dark"} {
					do(t, ctx, chromedp.EmulateViewport(width, 800))
					if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: theme}}}); err != nil {
						t.Fatal(err)
					}
					check(`document.documentElement.scrollWidth<=innerWidth && document.querySelector('.detail-view').scrollWidth<=document.querySelector('.detail-view').clientWidth+1`)
				}
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-groups [data-group="running"]`)), chromedp.SendKeys(chromedp.CSS(`.detail-search input`), " WEB "))
			check(`document.querySelectorAll('.detail-item').length===1 && document.querySelector('.detail-name').textContent==='web-中文' && document.querySelector('[data-group="stopped"]').textContent.includes('(0)')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-log summary`)))
			check(`document.querySelector('.detail-log pre').textContent.includes('stdout line 中文') && document.querySelector('.detail-log pre').textContent.includes('stderr line')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-view').getAttribute('aria-busy')==='false'`, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
				t.Fatal(err)
			}
			check(`document.querySelector('.detail-search input').value===' WEB ' && document.querySelectorAll('.detail-item').length===1`)
		})
	}
	data, err := os.ReadFile(trace)
	if err != nil || !strings.Contains(string(data), `"inspect"`) {
		t.Fatalf("native Docker commands were not observed: %s %v", data, err)
	}
}
