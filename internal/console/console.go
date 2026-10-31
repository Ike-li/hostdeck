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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ike-li/hostdeck/internal/appearance"
	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/diskhistory"
	"github.com/Ike-li/hostdeck/internal/layout"
	"github.com/Ike-li/hostdeck/internal/local"
	"github.com/Ike-li/hostdeck/internal/notify"
	"github.com/Ike-li/hostdeck/internal/observation"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/ports"
	"github.com/Ike-li/hostdeck/internal/proxy"
	"github.com/Ike-li/hostdeck/internal/tasks"
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
	// Appearance is shared by every browser using this console directory.
	Appearance *appearance.Store
	// Reload reads the plugins folder again after the plugins page changed
	// it. Nil leaves Plugins, Broken and Served as they are.
	Reload func() (Loaded, error)
	// Notifications says what the background checks of notifications.json
	// know; nil when nothing watches the cards.
	Notifications func() notify.Status
	// Watch puts a plugin in the watch list of notifications.json, or takes
	// it out; nil refuses.
	Watch func(id string, on bool) error
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
	changing     sync.Mutex
	observations *observation.Store
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

// Served is where the browser finds an embedded service, or why it cannot.
type Served struct {
	URL string
	Err error
}

// Console serves the console page and its API. It also reads cards for the
// notifications, through the readings the overview keeps.
type Console struct {
	http.Handler
	s *server
}

// ReadCard reads the card of the loaded plugin id as the overview does, and
// says how long a reading is kept. ok is false when no loaded plugin of that
// id has a card.
func (c *Console) ReadCard(ctx context.Context, id string) (v card.View, interval time.Duration, ok bool) {
	l := c.s.loaded()
	for _, p := range l.Plugins {
		if p.ID == id && p.Card != nil {
			return l.cards.Get(ctx, p), time.Duration(p.Card.Interval), true
		}
	}
	return card.View{}, 0, false
}

func Handler(cfg Config) *Console {
	s := &server{cfg: cfg, observations: observation.New(filepath.Join(filepath.Dir(cfg.PluginDir), "data", "service-observations"))}
	s.use(Loaded{cfg.Plugins, cfg.Broken, cfg.Served})
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	mux.HandleFunc("GET /{$}", s.serveIndex)
	mux.HandleFunc("GET /index.html", s.serveIndex)
	mux.HandleFunc("GET /api/appearance", s.serveAppearance)
	mux.HandleFunc("POST /api/appearance", s.serveAppearanceSave)
	mux.HandleFunc("GET /api/plugins", s.servePlugins)
	mux.HandleFunc("GET /api/status", s.serveStatus)
	mux.HandleFunc("GET /api/cards", s.serveCards)
	mux.HandleFunc("GET /api/cards/{id}", s.serveCard)
	mux.HandleFunc("POST /api/cards/refresh", s.serveRefresh)
	mux.HandleFunc("GET /api/notifications", s.serveNotifications)
	mux.HandleFunc("POST /api/notifications/watch", s.serveWatch)
	mux.HandleFunc("POST /api/clear/{id}", s.serveClear)
	mux.HandleFunc("GET /api/detail/{id}", s.serveDetail)
	mux.HandleFunc("POST /api/observations/{id}", s.serveObservationCreate)
	mux.HandleFunc("POST /api/observations/session/{session}/read", s.serveObservationRead)
	mux.HandleFunc("GET /api/operations/{id}", s.serveOperations)
	mux.HandleFunc("GET /api/disk-history/{id}", s.serveDiskHistory)
	mux.HandleFunc("POST /api/action/{id}", s.serveAction)
	mux.HandleFunc("GET /api/catalog", s.serveCatalog)
	mux.HandleFunc("POST /api/plugins/reload", s.serveReload)
	mux.HandleFunc("POST /api/catalog/{id}", s.serveAdd)
	mux.HandleFunc("POST /api/pages", s.serveAddPage)
	mux.HandleFunc("POST /api/plugins/{id}/disable", s.serveRename(catalog.Disable))
	mux.HandleFunc("POST /api/plugins/{id}/enable", s.serveRename(catalog.Enable))
	mux.HandleFunc("POST /api/plugins/{id}/trash", s.serveTrash)
	mux.HandleFunc("POST /api/trash/{folder}/restore", s.serveTrashEntry(catalog.Restore))
	mux.HandleFunc("POST /api/trash/{folder}/purge", s.serveTrashEntry(catalog.Purge))
	mux.HandleFunc("POST /api/plugins/{id}/page", s.serveEditPage)
	mux.HandleFunc("GET /api/plugins/{id}/scripts", s.serveScriptList)
	mux.HandleFunc("POST /api/plugins/{id}/scripts", s.serveScriptCreate)
	mux.HandleFunc("GET /api/plugins/{id}/scripts/{name}", s.serveScriptRead)
	mux.HandleFunc("POST /api/plugins/{id}/scripts/{name}", s.serveScriptUpdate)
	mux.HandleFunc("POST /api/plugins/{id}/scripts/{name}/delete", s.serveScriptDelete)
	mux.HandleFunc("POST /api/plugins/{id}/scripts/{name}/run", s.serveScriptRun)
	mux.HandleFunc("GET /api/plugins/{id}/tasks", s.serveTaskList)
	mux.HandleFunc("POST /api/plugins/{id}/tasks", s.serveTaskCreate)
	mux.HandleFunc("GET /api/plugins/{id}/tasks/{task}", s.serveTaskRead)
	mux.HandleFunc("POST /api/plugins/{id}/tasks/{task}", s.serveTaskUpdate)
	mux.HandleFunc("POST /api/plugins/{id}/tasks/{task}/delete", s.serveTaskDelete)
	mux.HandleFunc("GET /api/layout", s.serveLayoutGet)
	mux.HandleFunc("POST /api/layout", s.serveLayout)
	return &Console{local.Only(sameOriginAPI(noFraming(mux))), s}
}

// entry is a sidebar entry as the page receives it.
type entry struct {
	ID    string      `json:"id"`
	Name  plugin.Text `json:"name"`
	Icon  string      `json:"icon,omitempty"`
	Type  string      `json:"type"` // proxy | link | detail | card
	View  string      `json:"view,omitempty"`
	URL   string      `json:"url,omitempty"`
	Error *card.Error `json:"error,omitempty"`
}

func (s *server) servePlugins(w http.ResponseWriter, r *http.Request) {
	now := s.loaded()
	l := s.layout()
	entries := []entry{}
	for _, p := range now.Plugins {
		e := entry{ID: p.ID, Name: p.Name, Icon: p.Icon}
		switch {
		case p.Detail != nil:
			e.Type = "detail"
			if diskhistory.IsNative(p) {
				e.View = "disk-history"
			} else if tasks.IsNative(p) {
				e.View = "tasks"
			}
			entries = append(entries, e)
			continue
		case p.Page == nil:
			e.Type = "card"
			entries = append(entries, e)
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
	for _, b := range now.Broken {
		name := b.Name
		if name.IsZero() {
			name = plugin.Text{All: b.ID}
		}
		entries = append(entries, entry{ID: b.ID, Name: name, Type: "card", Error: manifestError(b)})
	}
	shown := []entry{}
	for _, e := range layout.Arrange(entries, l.Sidebar, func(e entry) string { return e.ID }) {
		if !slices.Contains(l.Hidden, e.ID) {
			shown = append(shown, e)
		}
	}
	writeJSON(w, shown)
}

// probe asks a service directly, not through its proxy. Any answer, a login
// page or an error included, means the service is running.
var probe = &http.Client{
	Timeout:       2 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (s *server) serveNotifications(w http.ResponseWriter, _ *http.Request) {
	status := notify.Status{Cards: map[string]notify.CardStatus{}}
	if s.cfg.Notifications != nil {
		status = s.cfg.Notifications()
	}
	writeJSON(w, status)
}

// serveWatch watches a card, or stops, writing only the watch list of
// notifications.json. A card is watched only when a loaded plugin has one.
func (s *server) serveWatch(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	var req struct {
		ID    string `json:"id"`
		Watch bool   `json:"watch"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	if req.Watch && !slices.ContainsFunc(s.loaded().Plugins, func(p plugin.Plugin) bool { return p.ID == req.ID && p.Card != nil }) {
		writeError(w, http.StatusNotFound, &card.Error{Code: "no_card"})
		return
	}
	if s.cfg.Watch == nil {
		writeError(w, http.StatusConflict, &card.Error{Code: "notifications_no_way_to_tell"})
		return
	}
	switch err := s.cfg.Watch(req.ID, req.Watch); {
	case errors.Is(err, notify.ErrNoWayToTell):
		writeError(w, http.StatusConflict, &card.Error{Code: "notifications_no_way_to_tell"})
		return
	case err != nil:
		writeError(w, http.StatusConflict, &card.Error{Code: "notifications_failed", Detail: err.Error()})
		return
	}
	s.serveNotifications(w, r)
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
	cards := make([]workspaceCard, len(now.Plugins))
	var wg sync.WaitGroup
	for i, p := range now.Plugins {
		wg.Go(func() {
			if p.Card != nil {
				cards[i].View, cards[i].Watchable = now.cards.Get(ctx, p), true
				return
			}
			c := workspaceCard{View: card.View{ID: p.ID, Name: p.Name, Icon: p.Icon}}
			switch {
			case p.Detail != nil:
				c.Destination = "detail"
			case p.Page != nil && p.Page.Link != "":
				c.Destination, c.Address = "link", p.Page.Link
			case p.Page != nil:
				c.Destination, c.Address = "proxy", p.Page.Proxy
				if served := now.Served[p.ID]; served.Err != nil {
					c.Error = portError(served.Err)
				}
			}
			cards[i] = c
		})
	}
	wg.Wait()
	for _, b := range now.Broken {
		name := b.Name
		if name.IsZero() {
			name = plugin.Text{All: b.ID}
		}
		cards = append(cards, workspaceCard{View: card.View{ID: b.ID, Name: name, Error: manifestError(b)}})
	}
	l := s.layout()
	arranged := []workspaceCard{}
	for _, c := range layout.Arrange(cards, l.Cards, func(c workspaceCard) string { return c.ID }) {
		if slices.Contains(l.Hidden, c.ID) {
			continue
		}
		c.Wide = slices.Contains(l.Wide, c.ID)
		arranged = append(arranged, c)
	}
	writeJSON(w, struct {
		PluginDir string          `json:"pluginDir"`
		Cards     []workspaceCard `json:"cards"`
	}{s.cfg.PluginDir, arranged})
}

// A connection without monitoring still has an overview shortcut. Its address
// describes where it opens, without claiming that the external service is up.
type workspaceCard struct {
	card.View
	Destination string `json:"destination,omitempty"`
	Address     string `json:"address,omitempty"`
	Wide        bool   `json:"wide,omitempty"`
	Watchable   bool   `json:"watchable,omitempty"` // a card notifications.json can watch
}

// The generated monitor view uses the same cache and pending contract as the
// overview, but does not read unrelated plugins.
func (s *server) serveCard(w http.ResponseWriter, r *http.Request) {
	now := s.loaded()
	for _, p := range now.Plugins {
		if p.ID != r.PathValue("id") || p.Card == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), overviewWait)
		defer cancel()
		writeJSON(w, now.cards.Get(ctx, p))
		return
	}
	writeError(w, http.StatusNotFound, &card.Error{Code: "no_card"})
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
// Minting the token needs the console's Origin, like other console actions.
func (s *server) serveClear(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
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
	if item := r.URL.Query().Get("item"); item != "" {
		writeJSON(w, detail.ReadItem(r.Context(), p, item))
		return
	}
	v := detail.Read(r.Context(), p)
	kind := observationKind(p)
	v.Observation = kind != ""
	if kind == "listeners" || kind == "manual" {
		identities := map[string]observation.Identity{}
		for _, item := range v.Items {
			for _, field := range item.Fields {
				if textEnglish(field.Name) == "PID" {
					pid, _ := strconv.Atoi(field.Value.String())
					identities[item.ID] = observation.ProcessIdentity(pid)
				}
			}
		}
		writeJSON(w, struct {
			detail.View
			Identities map[string]observation.Identity `json:"identities"`
		}{v, identities})
		return
	}
	writeJSON(w, v)
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
		Item        string `json:"item"`
		Action      string `json:"action"`
		Observation string `json:"observation,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	if req.Observation == "" && observationKind(p) == "manual" && req.Action == "restart" {
		target, err := s.resolveObservation(r.Context(), p, "manual", req.Item, observation.Identity{})
		if err != nil {
			s.observationError(w, 502, err)
			return
		}
		req.Observation, err = s.observations.Create(p.ID, req.Item, req.Action, target)
		if err != nil {
			s.observationError(w, 409, err)
			return
		}
	}
	actionContext := r.Context()
	if req.Observation != "" {
		if observationKind(p) == "" {
			s.observationError(w, 403, observation.ErrSession)
			return
		}
		if observationKind(p) != "jobs" {
			_, _, _, identity, bindingErr := s.observations.Binding(req.Observation)
			if bindingErr != nil || !identity.Equal(observation.ProcessIdentity(identity.PID)) {
				s.observationError(w, 409, observation.ErrSession)
				return
			}
			actionContext = observation.WithIdentity(actionContext, identity)
		}
		if err := s.observations.Begin(req.Observation, p.ID, req.Item, req.Action); err != nil {
			s.observationError(w, 409, err)
			return
		}
	}
	if req.Observation != "" && observationKind(p) == "manual" && req.Action == "restart" {
		actionContext = observation.WithCapture(actionContext, s.observations.Root(), req.Observation)
	}
	if req.Observation != "" {
		w.Header().Set("X-Hostdeck-Observation", req.Observation)
	}
	message, err := detail.Act(actionContext, p, req.Item, req.Action)
	if req.Observation != "" {
		result, notice := "returned", message
		if err != nil {
			result, notice = "failed", err.Text
			if notice.IsZero() {
				notice = plugin.Text{All: err.Code + ": " + err.Detail}
			}
		}
		if saveErr := s.observations.Finish(req.Observation, result, notice); saveErr != nil {
			s.observationError(w, 500, saveErr)
			return
		}
	}
	switch {
	case err != nil && err.Code == "not_offered":
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, struct {
			Message     plugin.Text `json:"message,omitzero"`
			Observation string      `json:"observation,omitempty"`
		}{message, req.Observation})
	}
}

// serveCatalog lists the plugins folder, with whether this run of hostdeck
// loaded each plugin, and the built-in plugins that are not in it.
func (s *server) serveCatalog(w http.ResponseWriter, r *http.Request) {
	installed, err := catalog.Installed(s.cfg.PluginDir)
	if err == nil {
		var builtIn []catalog.Entry
		builtIn, err = catalog.List(s.cfg.BuiltIn, s.cfg.PluginDir)
		var trashed []catalog.Trashed
		if err == nil {
			trashed, err = catalog.TrashedPlugins(s.cfg.PluginDir)
		}
		if err == nil {
			s.writeCatalog(w, installed, builtIn, trashed)
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

func (s *server) writeCatalog(w http.ResponseWriter, installed []catalog.Plugin, builtIn []catalog.Entry, trashed []catalog.Trashed) {
	pluginDir, err := filepath.Abs(s.cfg.PluginDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "catalog_failed", Detail: err.Error()})
		return
	}
	executable, err := os.Executable()
	if err != nil {
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "catalog_failed", Detail: err.Error()})
		return
	}
	type listed struct {
		catalog.Plugin
		Loaded bool        `json:"loaded"`
		Broken bool        `json:"broken"`
		Error  *card.Error `json:"error,omitempty"`
	}
	listing := struct {
		PluginDir  string            `json:"pluginDir"`
		Executable string            `json:"executable"`
		Installed  []listed          `json:"installed"`
		Available  []catalog.Entry   `json:"available"`
		Trash      []catalog.Trashed `json:"trash"`
	}{pluginDir, executable, []listed{}, []catalog.Entry{}, trashed}
	for _, p := range installed {
		_, loaded := s.plugin(p.ID)
		var problem *card.Error
		broken := false
		for _, b := range s.loaded().Broken {
			if b.ID == p.ID {
				problem = manifestError(b)
				broken = true
				break
			}
		}
		if problem == nil {
			if served := s.loaded().Served[p.ID]; served.Err != nil {
				problem = portError(served.Err)
			}
		}
		if problem == nil {
			if native, ok := s.plugin(p.ID); ok && tasks.IsNative(native) {
				if issue := tasks.Issue(native.Dir); !issue.IsZero() {
					problem = &card.Error{Code: "reported", Text: issue}
				}
			}
		}
		listing.Installed = append(listing.Installed, listed{p, loaded || broken, broken, problem})
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
// local network, or, when mode is link, a link that need not be local. It
// puts the plugin to use, for the console page only. A link's manifest has
// no command.
func (s *server) serveAddPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	var req struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Mode    string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	if req.Mode != "" && req.Mode != "proxy" && req.Mode != "link" {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	add := catalog.AddPage
	if req.Mode == "link" {
		add = catalog.AddLink
	}
	id, err := add(s.cfg.PluginDir, req.Address, req.Name)
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

// serveEditPage writes a page's name, icon or embed address, for the console
// page only, and leaves the plugin's commands as they are.
func (s *server) serveEditPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	var req struct {
		Name    *string `json:"name"`
		Icon    *string `json:"icon"`
		Proxy   *string `json:"proxy"`
		Link    *string `json:"link"`
		CardURL *string `json:"cardUrl"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request", Detail: err.Error()})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	err := catalog.EditPage(s.cfg.PluginDir, r.PathValue("id"), catalog.PageChange{
		Name: req.Name, Icon: req.Icon, Proxy: req.Proxy, Link: req.Link, CardURL: req.CardURL,
	})
	switch {
	case errors.Is(err, catalog.ErrUnknown):
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
	case errors.Is(err, catalog.ErrNotWeb):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "not_web"})
	case errors.Is(err, catalog.ErrNotLocal):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "not_local"})
	case errors.Is(err, catalog.ErrBoth), errors.Is(err, catalog.ErrEmptyName):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "bad_request"})
	case errors.Is(err, catalog.ErrNoCardURL):
		writeError(w, http.StatusBadRequest, &card.Error{Code: "no_card_url"})
	case err != nil:
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "edit_failed", Detail: err.Error()})
	case !s.reload(w):
	default:
		writeJSON(w, struct{}{})
	}
}

// serveTrash moves a plugin into the trash, for the console page only, and
// stops serving it. The folder name comes back so the page can restore it.
func (s *server) serveTrash(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	resume, err := s.suspendNative(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "trash_failed", Detail: err.Error()})
		return
	}
	defer resume()
	folder, err := catalog.Trash(s.cfg.PluginDir, r.PathValue("id"))
	switch {
	case errors.Is(err, catalog.ErrUnknown):
		writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
	case errors.Is(err, catalog.ErrTaken):
		writeError(w, http.StatusConflict, &card.Error{Code: "name_taken"})
	case err != nil:
		writeError(w, http.StatusInternalServerError, &card.Error{Code: "trash_failed", Detail: err.Error()})
	case !s.reload(w):
	default:
		writeJSON(w, struct {
			Folder string `json:"folder"`
		}{folder})
	}
}

// serveTrashEntry restores or purges one folder in the trash, for the console
// page only. Purge deletes that folder and nothing else.
func (s *server) serveTrashEntry(act func(pluginDir, folder string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != s.cfg.Origin {
			writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
			return
		}
		s.changing.Lock()
		defer s.changing.Unlock()
		switch err := act(s.cfg.PluginDir, r.PathValue("folder")); {
		case errors.Is(err, catalog.ErrUnknown):
			writeError(w, http.StatusNotFound, &card.Error{Code: "not_installed"})
		case errors.Is(err, catalog.ErrTaken):
			writeError(w, http.StatusConflict, &card.Error{Code: "name_taken"})
		case err != nil:
			writeError(w, http.StatusInternalServerError, &card.Error{Code: "trash_failed", Detail: err.Error()})
		case !s.reload(w):
		default:
			writeJSON(w, struct{}{})
		}
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
		resume, err := s.suspendNative(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, &card.Error{Code: "rename_failed", Detail: err.Error()})
			return
		}
		defer resume()
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

func (s *server) suspendNative(ctx context.Context, id string) (func(), error) {
	p, ok := s.plugin(id)
	if !ok {
		return func() {}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, detail.ReadTimeout)
	defer cancel()
	if tasks.IsNative(p) {
		return tasks.Suspend(ctx, p.Dir)
	}
	return diskhistory.Suspend(ctx, p.Dir)
}

// serveReload loads trusted on-disk plugins without changing their files.
func (s *server) serveReload(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		writeError(w, http.StatusForbidden, &card.Error{Code: "not_console"})
		return
	}
	s.changing.Lock()
	defer s.changing.Unlock()
	if s.reload(w) {
		s.serveCatalog(w, r)
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
