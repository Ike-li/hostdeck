package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/web"
	"github.com/chromedp/chromedp"
)

func TestAnOpenObservationFollowsWithoutReplacingTheFocusedAction(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	var reads, actions, badBindings atomic.Int32
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	jsonReply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /api/plugins", func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, []any{map[string]any{"id": "jobs", "type": "detail", "name": "Fixture"}})
	})
	mux.HandleFunc("GET /api/layout", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, map[string]any{}) })
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, map[string]any{}) })
	mux.HandleFunc("GET /api/detail/jobs", func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, map[string]any{"observation": true, "identities": map[string]any{"short": map[string]any{"pid": 77, "started": "2026-10-28T00:00:00Z"}}, "items": []any{map[string]any{"id": "short", "name": "Short task", "actions": []any{map[string]any{"id": "start", "label": "Run"}}}}})
	})
	mux.HandleFunc("POST /api/observations/jobs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Expected struct {
				PID     int    `json:"pid"`
				Started string `json:"started"`
			}
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Expected.PID != 77 || body.Expected.Started != "2026-10-28T00:00:00Z" {
			badBindings.Add(1)
			w.WriteHeader(400)
			return
		}
		jsonReply(w, map[string]string{"id": "session"})
	})
	mux.HandleFunc("POST /api/observations/session/session/read", func(w http.ResponseWriter, r *http.Request) {
		n := reads.Add(1)
		jsonReply(w, map[string]any{"id": "session", "state": "idle", "collectedAt": time.Now(), "reset": true, "cursor": fmt.Sprint(n), "log": strings.Repeat("line\n", 160) + fmt.Sprintf("delayed-%d", n), "sources": []any{map[string]string{"state": "available"}}})
	})
	mux.HandleFunc("GET /api/operations/jobs", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, []any{}) })
	mux.HandleFunc("POST /api/action/jobs", func(w http.ResponseWriter, r *http.Request) {
		actions.Add(1)
		jsonReply(w, map[string]string{"message": "Accepted"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for _, lang := range []string{"en", "zh-CN"} {
		for _, theme := range []string{"light", "dark"} {
			for _, width := range []int64{1280, 520} {
				t.Run(fmt.Sprintf("%s/%s/%d", lang, theme, width), func(t *testing.T) {
					ctx := openBrowser(t, lang)
					do(t, ctx, chromedp.EmulateViewport(width, 800), chromedp.Navigate(srv.URL+"#/jobs"), chromedp.WaitVisible(chromedp.CSS(".detail-item")))
					do(t, ctx, chromedp.Evaluate[chromedp.Void](fmt.Sprintf(`document.documentElement.dataset.theme=%q`, theme)))
					button := ".detail-item .action"
					do(t, ctx, chromedp.Click(chromedp.CSS(button)), chromedp.Click(chromedp.CSS(button)))
					if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.observation-log')?.textContent.includes('delayed-')`)); err != nil {
						t.Fatal(err)
					}
					do(t, ctx, chromedp.Evaluate[chromedp.Void](`window.keptAction=document.querySelector('.detail-item .action');keptAction.focus();window.keptLog=document.querySelector('.observation-log');keptLog.scrollTop=0;keptLog.dispatchEvent(new Event('scroll'));window.previousLog=keptLog.textContent;`))
					if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`keptLog.textContent!==previousLog`)); err != nil {
						t.Fatal(err)
					}
					ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`keptAction===document.activeElement && keptAction===document.querySelector('.detail-item .action') && keptLog.scrollTop===0 && !document.querySelector('.observation-latest').hidden && document.documentElement.scrollWidth<=innerWidth`))
					if err != nil || !ok {
						t.Fatalf("focus/scroll/layout: %v %v", ok, err)
					}
					do(t, ctx, chromedp.Click(chromedp.CSS(".observation-latest")), chromedp.Click(chromedp.CSS(".observation summary")))
					before := reads.Load()
					do(t, ctx, chromedp.Sleep(1200*time.Millisecond))
					if reads.Load() != before {
						t.Fatal("collapsed observation kept reading")
					}
				})
			}
		}
	}
	if badBindings.Load() != 0 {
		t.Fatalf("unbound browser sessions: %d", badBindings.Load())
	}
	if actions.Load() != 8 {
		t.Fatalf("executed %d actions", actions.Load())
	}
}
