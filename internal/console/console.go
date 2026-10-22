// Package console serves the console page and its API.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/local"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/proxy"
	"github.com/Ike-li/hostdeck/web"
)

// overviewWait bounds how long the overview waits for its cards. A card that
// takes longer shows as pending, and the page asks again.
const overviewWait = 5 * time.Second

type Config struct {
	// Origin is the console page's own origin, the only one that may run actions.
	Origin    string
	Plugins   []plugin.Plugin
	Broken    []plugin.Broken // shown as error cards
	PluginDir string
	// BuiltIn holds the plugins the plugins page offers, one folder each.
	BuiltIn fs.FS
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
	mux.HandleFunc("GET /api/cards", cfg.serveCards(card.NewCache(time.Now)))
	mux.HandleFunc("POST /api/clear/{id}", cfg.serveClear)
	mux.HandleFunc("GET /api/detail/{id}", cfg.serveDetail)
	mux.HandleFunc("POST /api/action/{id}", cfg.serveAction)
	mux.HandleFunc("GET /api/catalog", cfg.serveCatalog)
	mux.HandleFunc("POST /api/catalog/{id}", cfg.serveAdd)
	mux.HandleFunc("POST /api/plugins/{id}/disable", cfg.serveRename(catalog.Disable))
	mux.HandleFunc("POST /api/plugins/{id}/enable", cfg.serveRename(catalog.Enable))
	return local.Only(sameOriginAPI(noFraming(mux)))
}

// entry is a sidebar entry as the page receives it.
type entry struct {
	ID    string      `json:"id"`
	Name  plugin.Text `json:"name"`
	Icon  string      `json:"icon,omitempty"`
	Type  string      `json:"type"` // proxy | link | detail
	URL   string      `json:"url,omitempty"`
	Error *card.Error `json:"error,omitempty"`
}

func (cfg Config) servePlugins(w http.ResponseWriter, r *http.Request) {
	entries := []entry{}
	for _, p := range cfg.Plugins {
		e := entry{ID: p.ID, Name: p.Name, Icon: p.Icon}
		switch {
		case p.Detail != nil:
			e.Type = "detail"
			entries = append(entries, e)
			continue
		case p.Page == nil:
			continue
		}
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

func (cfg Config) serveCards(cache *card.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), overviewWait)
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
			wg.Go(func() { cards[i] = cache.Get(ctx, p) })
		}
		wg.Wait()
		for _, b := range cfg.Broken {
			cards = append(cards, card.View{ID: b.ID, Name: plugin.Text{All: b.ID}, Error: &card.Error{Code: "bad_manifest", Detail: b.Err.Error()}})
		}
		writeJSON(w, struct {
			PluginDir string      `json:"pluginDir"`
			Cards     []card.View `json:"cards"`
		}{cfg.PluginDir, cards})
	}
}

// serveClear hands out the address at which an embedded service's proxy has
// the browser clear that service's cache and storage. The address works once.
func (cfg Config) serveClear(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, ok := cfg.Served[id]
	if !ok || s.Err != nil {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_embedded"})
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

func (cfg Config) plugin(id string) (plugin.Plugin, bool) {
	i := slices.IndexFunc(cfg.Plugins, func(p plugin.Plugin) bool { return p.ID == id })
	if i < 0 {
		return plugin.Plugin{}, false
	}
	return cfg.Plugins[i], true
}

func (cfg Config) serveDetail(w http.ResponseWriter, r *http.Request) {
	p, ok := cfg.plugin(r.PathValue("id"))
	if !ok || p.Detail == nil {
		writeError(w, http.StatusNotFound, &card.Error{Code: "no_detail"})
		return
	}
	writeJSON(w, detail.Read(r.Context(), p))
}

// serveAction runs an action that the console page asked for. Every other
// page on 127.0.0.1, embedded services included, is turned away by its Origin.
func (cfg Config) serveAction(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	p, ok := cfg.plugin(r.PathValue("id"))
	if !ok || p.Actions == nil {
		writeError(w, http.StatusNotFound, &card.Error{Code: "no_actions"})
		return
	}
	var req struct {
		Item   string `json:"item"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	message, err := detail.Act(r.Context(), p, req.Item, req.Action)
	switch {
	case err != nil && err.Code == "not_offered":
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, struct {
			Message plugin.Text `json:"message,omitzero"`
		}{message})
	}
}

// serveCatalog lists the plugins folder, with whether this run of hostdeck
// loaded each plugin, and the built-in plugins that are not in it.
func (cfg Config) serveCatalog(w http.ResponseWriter, r *http.Request) {
	installed, err := catalog.Installed(cfg.PluginDir)
	if err == nil {
		var builtIn []catalog.Entry
		builtIn, err = catalog.List(cfg.BuiltIn, cfg.PluginDir)
		if err == nil {
			cfg.writeCatalog(w, installed, builtIn)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, &card.Error{Code: "catalog_failed", Detail: err.Error()})
}

func (cfg Config) writeCatalog(w http.ResponseWriter, installed []catalog.Plugin, builtIn []catalog.Entry) {
	type listed struct {
		catalog.Plugin
		Loaded bool `json:"loaded"`
		Broken bool `json:"broken"`
	}
	listing := struct {
		PluginDir string          `json:"pluginDir"`
		Installed []listed        `json:"installed"`
		Available []catalog.Entry `json:"available"`
	}{cfg.PluginDir, []listed{}, []catalog.Entry{}}
	for _, p := range installed {
		_, loaded := cfg.plugin(p.ID)
		broken := slices.ContainsFunc(cfg.Broken, func(b plugin.Broken) bool { return b.ID == p.ID })
		listing.Installed = append(listing.Installed, listed{p, loaded || broken, broken})
	}
	for _, e := range builtIn {
		if !e.Added {
			listing.Available = append(listing.Available, e)
		}
	}
	writeJSON(w, listing)
}

// serveAdd copies a built-in plugin into the plugins folder, for the console
// page only: what is added there runs its commands once hostdeck restarts.
func (cfg Config) serveAdd(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	path, err := catalog.Add(cfg.BuiltIn, cfg.PluginDir, r.PathValue("id"))
	switch {
	case errors.Is(err, catalog.ErrUnknown):
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_built_in"})
	case errors.Is(err, catalog.ErrAdded):
		writeError(w, http.StatusConflict, &card.Error{Code: "already_added"})
	case err != nil:
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "add_failed", Detail: err.Error()})
	default:
		writeJSON(w, struct {
			Path string `json:"path"`
		}{path})
	}
}

// serveRename disables or enables a plugin, for the console page only.
func (cfg Config) serveRename(rename func(pluginDir, id string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != cfg.Origin {
			writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
			return
		}
		switch err := rename(cfg.PluginDir, r.PathValue("id")); {
		case errors.Is(err, catalog.ErrUnknown):
			writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		case errors.Is(err, catalog.ErrTaken):
			writeError(w, http.StatusConflict, &card.Error{Code: "name_taken"})
		case err != nil:
			writeError(w, http.StatusInternalServerError, &card.Error{Code: "rename_failed", Detail: err.Error()})
		default:
			writeJSON(w, struct{}{})
		}
	}
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

func writeError(w http.ResponseWriter, status int, e *card.Error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]*card.Error{"error": e})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
