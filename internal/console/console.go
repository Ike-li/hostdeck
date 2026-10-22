// Package console serves the console page and its API.
package console

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/web"
)

// cardTimeout bounds one overview request, so a hung service shows up as an
// error card instead of an overview that never loads.
const cardTimeout = 10 * time.Second

func Handler(plugins []plugin.Plugin, pluginDir string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	mux.HandleFunc("GET /api/cards", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), cardTimeout)
		defer cancel()
		cards := make([]card.View, len(plugins))
		var wg sync.WaitGroup
		for i, p := range plugins {
			wg.Go(func() { cards[i] = card.Fetch(ctx, p) })
		}
		wg.Wait()
		writeJSON(w, struct {
			PluginDir string      `json:"pluginDir"`
			Cards     []card.View `json:"cards"`
		}{pluginDir, cards})
	})
	return localOnly(sameOriginAPI(noFraming(mux)))
}

// localOnly answers requests addressed to 127.0.0.1 only, so a web page that
// rebinds its own domain to 127.0.0.1 cannot read the console. localhost is sent
// to 127.0.0.1: they are different sites to the browser, and keeping one of them
// keeps one set of cookies and storage for each embedded service.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, err := net.SplitHostPort(r.Host)
		if err != nil {
			host, port = r.Host, ""
		}
		switch host {
		case "127.0.0.1":
			next.ServeHTTP(w, r)
		case "localhost":
			to := "http://127.0.0.1"
			if port != "" {
				to += ":" + port
			}
			http.Redirect(w, r, to+r.URL.RequestURI(), http.StatusTemporaryRedirect)
		default:
			http.Error(w, "hostdeck answers requests to 127.0.0.1 only", http.StatusForbidden)
		}
	})
}

// sameOriginAPI keeps other pages from calling the API. Every port on 127.0.0.1
// is the same site, embedded services included, so "same-site" is refused too.
// Programs that send no Sec-Fetch-Site run on this machine and are let through.
func sameOriginAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			switch r.Header.Get("Sec-Fetch-Site") {
			case "", "same-origin":
			case "none":
				if r.Method != http.MethodGet {
					http.Error(w, "the API answers the console page only", http.StatusForbidden)
					return
				}
			default:
				http.Error(w, "the API answers the console page only", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func noFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
