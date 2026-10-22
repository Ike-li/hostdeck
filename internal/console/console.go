// Package console serves the console page and its API.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/layout"
	"github.com/Ike-li/hostdeck/internal/local"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/ports"
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
	// Layout is how the user arranged the sidebar and the cards; nil is the
	// order the plugins load in.
	Layout *layout.Store
	// Reload reads the plugins folder again after the plugins page changed
	// it. Nil leaves Plugins, Broken and Served as they are.
	Reload func() (Loaded, error)
}

// Loaded is what one reading of the plugins folder gave.
type Loaded struct {
	Plugins []plugin.Plugin
	Broken  []plugin.Broken
	Served  map[string]Served
}

type server struct {
	cfg     Config
	current atomic.Pointer[state]
	// changing lets one change to the plugins folder, with its reload, run
	// at a time.
	changing sync.Mutex
}

// state is the plugins in use, with a card cache of their own: a plugin
// enabled again, perhaps changed, is asked anew.
type state struct {
	Loaded
	cards *card.Cache
}

func (s *server) loaded() *state { return s.current.Load() }

func (s *server) use(l Loaded) {
	s.current.Store(&state{l, card.NewCache(time.Now)})
}

func (s *server) layout() layout.Layout {
	if s.cfg.Layout == nil {
		return layout.Layout{}
	}
	return s.cfg.Layout.Get()
}

func pluginID(p plugin.Plugin) string { return p.ID }

// Served is where the browser finds an embedded service, or why it cannot.
type Served struct {
	URL string
	Err error
}

func Handler(cfg Config) http.Handler {
	s := &server{cfg: cfg}
	s.use(Loaded{cfg.Plugins, cfg.Broken, cfg.Served})
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	mux.HandleFunc("GET /api/plugins", s.servePlugins)
	mux.HandleFunc("GET /api/status", s.serveStatus)
	mux.HandleFunc("GET /api/cards", s.serveCards)
	mux.HandleFunc("POST /api/cards/refresh", s.serveRefresh)
	mux.HandleFunc("POST /api/clear/{id}", s.serveClear)
	mux.HandleFunc("GET /api/detail/{id}", s.serveDetail)
	mux.HandleFunc("POST /api/action/{id}", s.serveAction)
	mux.HandleFunc("GET /api/catalog", s.serveCatalog)
	mux.HandleFunc("POST /api/catalog/{id}", s.serveAdd)
	mux.HandleFunc("POST /api/pages", s.serveAddPage)
	mux.HandleFunc("POST /api/plugins/{id}/disable", s.serveRename(catalog.Disable))
	mux.HandleFunc("POST /api/plugins/{id}/enable", s.serveRename(catalog.Enable))
	mux.HandleFunc("GET /api/layout", s.serveLayoutGet)
	mux.HandleFunc("POST /api/layout", s.serveLayout)
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

func (s *server) servePlugins(w http.ResponseWriter, r *http.Request) {
	now := s.loaded()
	l := s.layout()
	entries := []entry{}
	for _, p := range layout.Arrange(now.Plugins, l.Sidebar, pluginID) {
		if slices.Contains(l.Hidden, p.ID) {
			continue
		}
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
			if served := now.Served[p.ID]; served.Err != nil {
				e.Error = portError(served.Err)
			} else {
				e.URL = served.URL
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

func (s *server) serveStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range s.loaded().Plugins {
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

func (s *server) serveCards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), overviewWait)
	defer cancel()
	now := s.loaded()
	var withCard []plugin.Plugin
	for _, p := range now.Plugins {
		if p.Card != nil {
			withCard = append(withCard, p)
		}
	}
	cards := make([]card.View, len(withCard))
	var wg sync.WaitGroup
	for i, p := range withCard {
		wg.Go(func() { cards[i] = now.cards.Get(ctx, p) })
	}
	wg.Wait()
	for _, b := range now.Broken {
		name := b.Name
		if name.IsZero() {
			name = plugin.Text{All: b.ID}
		}
		cards = append(cards, card.View{ID: b.ID, Name: name, Error: manifestError(b)})
	}
	l := s.layout()
	type placed struct {
		card.View
		Wide bool `json:"wide,omitempty"`
	}
	arranged := []placed{}
	for _, c := range layout.Arrange(cards, l.Cards, func(c card.View) string { return c.ID }) {
		if slices.Contains(l.Hidden, c.ID) {
			continue
		}
		arranged = append(arranged, placed{c, slices.Contains(l.Wide, c.ID)})
	}
	writeJSON(w, struct {
		PluginDir string   `json:"pluginDir"`
		Cards     []placed `json:"cards"`
	}{s.cfg.PluginDir, arranged})
}

// serveRefresh forgets the kept card values. The next read asks again.
// It writes nothing.
func (s *server) serveRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	if now := s.loaded(); now != nil {
		now.cards.Drop()
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveClear hands out the address at which an embedded service's proxy has
// the browser clear that service's cache and storage. The address works once.
func (s *server) serveClear(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	served, ok := s.loaded().Served[id]
	if !ok || served.Err != nil {
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_embedded"})
		return
	}
	u, err := url.Parse(served.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	clear := url.URL{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Path:     proxy.ClearPath,
		RawQuery: url.Values{"token": {s.cfg.Tokens.Issue(id)}}.Encode(),
	}
	writeJSON(w, map[string]string{"url": clear.String()})
}

func (s *server) plugin(id string) (plugin.Plugin, bool) {
	plugins := s.loaded().Plugins
	i := slices.IndexFunc(plugins, func(p plugin.Plugin) bool { return p.ID == id })
	if i < 0 {
		return plugin.Plugin{}, false
	}
	return plugins[i], true
}

func (s *server) serveDetail(w http.ResponseWriter, r *http.Request) {
	p, ok := s.plugin(r.PathValue("id"))
	if !ok || p.Detail == nil {
		writeError(w, http.StatusNotFound, &card.Error{Code: "no_detail"})
		return
	}
	writeJSON(w, detail.Read(r.Context(), p))
}

// serveAction runs an action that the console page asked for. Every other
// page on 127.0.0.1, embedded services included, is turned away by its Origin.
func (s *server) serveAction(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	p, ok := s.plugin(r.PathValue("id"))
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
func (s *server) serveCatalog(w http.ResponseWriter, r *http.Request) {
	installed, err := catalog.Installed(s.cfg.PluginDir)
	if err == nil {
		var builtIn []catalog.Entry
		builtIn, err = catalog.List(s.cfg.BuiltIn, s.cfg.PluginDir)
		if err == nil {
			s.writeCatalog(w, installed, builtIn)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, &card.Error{Code: "catalog_failed", Detail: err.Error()})
}

// manifestError is the overview and plugins-page wording of a plugin that
// did not load. A missing variable names that variable. The file path is
// kept in File, out of the sentence.
func manifestError(b plugin.Broken) *card.Error {
	err := b.Err
	file := ""
	var located *plugin.Located
	if errors.As(err, &located) {
		file = located.Path
		err = located.Err
	}
	var missing *plugin.EnvMissing
	if errors.As(err, &missing) {
		return &card.Error{Code: "env_missing", Detail: missing.Name, File: file}
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return &card.Error{Code: "bad_manifest", Detail: detail, File: file}
}

// portError words a forwarding port that could not be opened. A port this
// plugin keeps is reported as that port number; the page says it is not moved.
func portError(err error) *card.Error {
	var inUse *ports.InUse
	if errors.As(err, &inUse) {
		return &card.Error{Code: "port_unavailable", Detail: strconv.Itoa(inUse.Port)}
	}
	return &card.Error{Code: "port_unavailable", Detail: err.Error()}
}

func (s *server) writeCatalog(w http.ResponseWriter, installed []catalog.Plugin, builtIn []catalog.Entry) {
	type listed struct {
		catalog.Plugin
		Loaded bool        `json:"loaded"`
		Broken bool        `json:"broken"`
		Error  *card.Error `json:"error,omitempty"`
	}
	listing := struct {
		PluginDir string          `json:"pluginDir"`
		Installed []listed        `json:"installed"`
		Available []catalog.Entry `json:"available"`
	}{s.cfg.PluginDir, []listed{}, []catalog.Entry{}}
	for _, p := range installed {
		_, loaded := s.plugin(p.ID)
		var problem *card.Error
		for _, b := range s.loaded().Broken {
			if b.ID == p.ID {
				problem = manifestError(b)
				break
			}
		}
		listing.Installed = append(listing.Installed, listed{p, loaded || problem != nil, problem != nil, problem})
	}
	for _, e := range builtIn {
		if !e.Added {
			listing.Available = append(listing.Available, e)
		}
	}
	writeJSON(w, listing)
}

// serveAdd copies a built-in plugin into the plugins folder and puts it to
// use, for the console page only.
func (s *server) serveAdd(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	path, err := catalog.Add(s.cfg.BuiltIn, s.cfg.PluginDir, r.PathValue("id"))
	switch {
	case errors.Is(err, catalog.ErrUnknown):
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_built_in"})
	case errors.Is(err, catalog.ErrAdded):
		writeError(w, http.StatusConflict, &card.Error{Code: "already_added"})
	case err != nil:
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "add_failed", Detail: err.Error()})
	case !s.reload(w):
	default:
		writeJSON(w, struct {
			Path string `json:"path"`
		}{path})
	}
}

// serveAddPage adds a plugin that embeds a web page on this machine or the
// local network and puts it to use, for the console page only.
func (s *server) serveAddPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	var req struct {
		Address string `json:"address"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	id, err := catalog.AddPage(s.cfg.PluginDir, req.Address, req.Name)
	switch {
	case errors.Is(err, catalog.ErrNotWeb):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "not_web"})
	case errors.Is(err, catalog.ErrNotLocal):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "not_local"})
	case err != nil:
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "add_failed", Detail: err.Error()})
	case !s.reload(w):
	default:
		writeJSON(w, struct {
			ID string `json:"id"`
		}{id})
	}
}

// serveRename disables or enables a plugin, for the console page only, and
// stops or starts it.
func (s *server) serveRename(rename func(pluginDir, id string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != s.cfg.Origin {
			writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
			return
		}
		s.changing.Lock()
		defer s.changing.Unlock()
		switch err := rename(s.cfg.PluginDir, r.PathValue("id")); {
		case errors.Is(err, catalog.ErrUnknown):
			writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		case errors.Is(err, catalog.ErrTaken):
			writeError(w, http.StatusConflict, &card.Error{Code: "name_taken"})
		case err != nil:
			writeError(w, http.StatusInternalServerError, &card.Error{Code: "rename_failed", Detail: err.Error()})
		case !s.reload(w):
		default:
			writeJSON(w, struct{}{})
		}
	}
}

// reload reads the plugins folder again and puts what it holds to use. If
// that fails, what was in use stays, and reload writes the error to w.
func (s *server) reload(w http.ResponseWriter) bool {
	if s.cfg.Reload == nil {
		return true
	}
	l, err := s.cfg.Reload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "reload_failed", Detail: err.Error()})
		return false
	}
	s.use(l)
	return true
}

// serveLayoutGet returns the saved layout. The page needs the whole hidden
// list before it can add to it, and the lists it shows no longer contain
// those IDs.
func (s *server) serveLayoutGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.cfg.Layout.Get())
}

// serveLayout saves how the user arranged the console, for the console page
// only. A list the request leaves out stays as it was. Reset replaces the
// whole layout and is not itself stored. A hidden plugin keeps its place in
// any list the request does send.
func (s *server) serveLayout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	prev := s.cfg.Layout.Get()
	// A second copy: decoding the request reuses a slice's backing array, which
	// would otherwise overwrite the places just read.
	l := s.cfg.Layout.Get()
	req := struct {
		*layout.Layout
		Reset bool `json:"reset"`
	}{Layout: &l}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	if req.Reset {
		l = layout.Layout{}
	} else {
		l.Sidebar = keepPlace(l.Sidebar, prev.Sidebar, l.Hidden)
		l.Cards = keepPlace(l.Cards, prev.Cards, l.Hidden)
		l.Wide = keepPlace(l.Wide, prev.Wide, l.Hidden)
	}
	if err := s.cfg.Layout.Set(l); err != nil {
		log.Printf("layout: %v", err)
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "layout_failed", Detail: err.Error()})
		return
	}
	writeJSON(w, l)
}

// keepPlace puts hidden IDs back into the places they had in previous.
// next is the list the page still shows.
func keepPlace(next, previous, hidden []string) []string {
	if len(hidden) == 0 {
		return next
	}
	hide := make(map[string]bool, len(hidden))
	for _, id := range hidden {
		hide[id] = true
	}
	visible := make([]string, 0, len(next))
	for _, id := range next {
		if !hide[id] {
			visible = append(visible, id)
		}
	}
	result := make([]string, 0, len(previous)+len(visible))
	placed := make(map[string]bool, len(previous)+len(visible))
	v := 0
	for _, id := range previous {
		if hide[id] {
			if !placed[id] {
				result = append(result, id)
				placed[id] = true
			}
			continue
		}
		if v < len(visible) {
			id = visible[v]
			v++
			if !placed[id] {
				result = append(result, id)
				placed[id] = true
			}
		}
	}
	for ; v < len(visible); v++ {
		if !placed[visible[v]] {
			result = append(result, visible[v])
			placed[visible[v]] = true
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
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
