// Package console serves the console page and its API.
package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/local"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/proxy"
	"github.com/Ike-li/hostdeck/web"
)

// cardTimeout bounds one overview request, so a hung service shows up as an
// error card instead of an overview that never loads.
const cardTimeout = 10 * time.Second

type Config struct {
	Plugins   []plugin.Plugin
	PluginDir string
	// Served holds, by plugin ID, where each embedded service is served.
	Served map[string]Served
	Tokens *onetime.Tokens
}

// Served is where the browser finds an embedded service, or why it cannot.
type Served struct {
	URL string
	Err error
}

func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	mux.HandleFunc("GET /api/plugins", cfg.servePlugins)
	mux.HandleFunc("GET /api/status", cfg.serveStatus)
	mux.HandleFunc("GET /api/cards", cfg.serveCards)
	mux.HandleFunc("POST /api/clear/{id}", cfg.serveClear)
	return local.Only(sameOriginAPI(noFraming(mux)))
}

// entry is a sidebar entry as the page receives it.
type entry struct {
	ID    string      `json:"id"`
	Name  plugin.Text `json:"name"`
	Icon  string      `json:"icon,omitempty"`
	Type  string      `json:"type"` // proxy | link
	URL   string      `json:"url,omitempty"`
	Error *card.Error `json:"error,omitempty"`
}

func (cfg Config) servePlugins(w http.ResponseWriter, r *http.Request) {
	entries := []entry{}
	for _, p := range cfg.Plugins {
		if p.Page == nil {
			continue
		}
		e := entry{ID: p.ID, Name: p.Name, Icon: p.Icon}
		if p.Page.Link != "" {
			e.Type, e.URL = "link", p.Page.Link
		} else {
			e.Type = "proxy"
			if s := cfg.Served[p.ID]; s.Err != nil {
				e.Error = &card.Error{Code: "port_unavailable", Detail: s.Err.Error()}
			} else {
				e.URL = s.URL
			}
		}
		entries = append(entries, e)
	}
	writeJSON(w, entries)
}

// probe asks a service directly, not through its proxy. Any answer, a login
// page or an error included, means the service is running.
var probe = &http.Client{
	Timeout:       2 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (cfg Config) serveStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range cfg.Plugins {
		if p.Page == nil || p.Page.Proxy == "" {
			continue
		}
		wg.Go(func() {
			s := "down"
			if answers(r.Context(), p.Page.Proxy) {
				s = "up"
			}
			mu.Lock()
			status[p.ID] = s
			mu.Unlock()
		})
	}
	wg.Wait()
	writeJSON(w, status)
}

func answers(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := probe.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func (cfg Config) serveCards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cardTimeout)
	defer cancel()
	var withCard []plugin.Plugin
	for _, p := range cfg.Plugins {
		if p.Card != nil {
			withCard = append(withCard, p)
		}
	}
	cards := make([]card.View, len(withCard))
	var wg sync.WaitGroup
	for i, p := range withCard {
		wg.Go(func() { cards[i] = card.Fetch(ctx, p) })
	}
	wg.Wait()
	writeJSON(w, struct {
		PluginDir string      `json:"pluginDir"`
		Cards     []card.View `json:"cards"`
	}{cfg.PluginDir, cards})
}

// serveClear hands out the address at which an embedded service's proxy has
// the browser clear that service's cache and storage. The address works once.
func (cfg Config) serveClear(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, ok := cfg.Served[id]
	if !ok || s.Err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]card.Error{"error": {Code: "not_embedded"}})
		return
	}
	served, err := url.Parse(s.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	clear := url.URL{
		Scheme:   served.Scheme,
		Host:     served.Host,
		Path:     proxy.ClearPath,
		RawQuery: url.Values{"token": {cfg.Tokens.Issue(id)}}.Encode(),
	}
	writeJSON(w, map[string]string{"url": clear.String()})
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

// noFraming keeps the console out of every frame, and lets it frame only the
// services it re-serves on 127.0.0.1.
func noFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-src http://127.0.0.1:*; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
