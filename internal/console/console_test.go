package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tidwall/gjson"

	"github.com/Ike-li/hostdeck/internal/layout"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/ports"
	"github.com/Ike-li/hostdeck/internal/proxy"
)

func decodeJSON(t *testing.T, resp *http.Response) any {
	t.Helper()
	var v any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func parseJSON(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return v
}

func get(h http.Handler, url string, header map[string]string) *http.Response {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

func TestConsoleAnswersOnlyRequestsAddressedTo127001(t *testing.T) {
	h := Handler(Config{PluginDir: "plugins"})

	if resp := get(h, "http://127.0.0.1:4678/", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("127.0.0.1: status %d, want 200", resp.StatusCode)
	}
	// A page whose domain was rebound to 127.0.0.1 still sends its own name.
	if resp := get(h, "http://rebound.example:4678/api/cards", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("another host: status %d, want 403", resp.StatusCode)
	}
	resp := get(h, "http://localhost:4678/api/cards?x=1", nil)
	if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != "http://127.0.0.1:4678/api/cards?x=1" {
		t.Errorf("localhost: status %d to %q, want 307 to the same URL on 127.0.0.1", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestAPIAnswersOnlyTheConsolePageAndLocalPrograms(t *testing.T) {
	h := Handler(Config{PluginDir: "plugins"})
	for site, want := range map[string]int{
		"":            http.StatusOK, // curl and other programs on this machine
		"same-origin": http.StatusOK,
		"none":        http.StatusOK, // typed into the address bar
		"same-site":   http.StatusForbidden,
		"cross-site":  http.StatusForbidden,
	} {
		resp := get(h, "http://127.0.0.1:4678/api/cards", map[string]string{"Sec-Fetch-Site": site})
		if resp.StatusCode != want {
			t.Errorf("Sec-Fetch-Site %q: status %d, want %d", site, resp.StatusCode, want)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4678/api/cards", nil)
	req.Header.Set("Sec-Fetch-Site", "none")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("POST with Sec-Fetch-Site none: status %d, want 403", w.Code)
	}
}

func TestNoPageCanFrameTheConsole(t *testing.T) {
	csp := get(Handler(Config{PluginDir: "plugins"}), "http://127.0.0.1:4678/", nil).Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q, want frame-ancestors 'none'", csp)
	}
}

func TestCardsListTheOverviewAndThePluginFolder(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"database": "ok"}`)
	}))
	defer svc.Close()
	plugins := []plugin.Plugin{{
		ID:   "demo",
		Name: plugin.Text{All: "Demo"},
		Icon: "D",
		Card: &plugin.Card{
			HTTP:  &plugin.HTTP{URL: svc.URL},
			Value: "database",
			Label: plugin.Text{ByLang: map[string]string{"en": "Database", "zh-CN": "数据库"}},
		},
	}}

	plugins = append(plugins, plugin.Plugin{ID: "pageonly", Name: plugin.Text{All: "Page only"}, Page: &plugin.Page{Link: "https://wiki.example/"}})
	resp := get(Handler(Config{Plugins: plugins, PluginDir: "/somewhere/plugins"}), "http://127.0.0.1:4678/api/cards", nil)

	var got any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	card0 := got.(map[string]any)["cards"].([]any)[0].(map[string]any)
	updated, _ := card0["updated"].(string)
	if _, err := time.Parse(time.RFC3339Nano, updated); err != nil {
		t.Errorf("updated %q, want the time the card was read", updated)
	}
	delete(card0, "updated")
	var want any
	json.Unmarshal([]byte(`{
		"pluginDir": "/somewhere/plugins",
		"cards": [{"id": "demo", "name": "Demo", "icon": "D", "value": "ok",
		           "label": {"en": "Database", "zh-CN": "数据库"}, "watchable": true},
		          {"id": "pageonly", "name": "Page only", "destination": "link", "address": "https://wiki.example/"}]
	}`), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPluginsSayWhereEachSidebarEntryOpens(t *testing.T) {
	plugins := []plugin.Plugin{
		{ID: "grafana", Name: plugin.Text{All: "Grafana"}, Icon: "G", Page: &plugin.Page{Proxy: "http://127.0.0.1:3000/grafana/"}},
		{ID: "stuck", Name: plugin.Text{All: "Stuck"}, Page: &plugin.Page{Proxy: "http://127.0.0.1:3001/"}},
		{ID: "wiki", Name: plugin.Text{ByLang: map[string]string{"en": "Wiki", "zh-CN": "维基"}}, Page: &plugin.Page{Link: "https://wiki.example/"}},
		{ID: "cardonly", Name: plugin.Text{All: "Card only"}, Card: &plugin.Card{HTTP: &plugin.HTTP{URL: "http://127.0.0.1:3002/"}, Value: "v"}},
		{ID: "disks", Name: plugin.Text{All: "Disks"}, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"sh", "detail.sh"}}}},
	}
	served := map[string]Served{
		"grafana": {URL: "http://127.0.0.1:4679/grafana/"},
		"stuck":   {Err: &ports.InUse{Port: 4680, ID: "stuck"}},
	}

	resp := get(Handler(Config{Plugins: plugins, Served: served}), "http://127.0.0.1:4678/api/plugins", nil)

	want := parseJSON(`[
		{"id": "grafana", "name": "Grafana", "icon": "G", "type": "proxy", "url": "http://127.0.0.1:4679/grafana/"},
		{"id": "stuck", "name": "Stuck", "type": "proxy",
		 "error": {"code": "port_unavailable", "detail": "4680"}},
		{"id": "wiki", "name": {"en": "Wiki", "zh-CN": "维基"}, "type": "link", "url": "https://wiki.example/"},
		{"id": "cardonly", "name": "Card only", "type": "card"},
		{"id": "disks", "name": "Disks", "type": "detail"}
	]`)
	if got := decodeJSON(t, resp); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStatusSaysWhichEmbeddedServicesAnswer(t *testing.T) {
	// Any answer counts, a login demand included: the service is running.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer up.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	plugins := []plugin.Plugin{
		{ID: "up", Name: plugin.Text{All: "Up"}, Page: &plugin.Page{Proxy: up.URL + "/"}},
		{ID: "down", Name: plugin.Text{All: "Down"}, Page: &plugin.Page{Proxy: down.URL + "/"}},
		{ID: "wiki", Name: plugin.Text{All: "Wiki"}, Page: &plugin.Page{Link: "https://wiki.example/"}},
	}

	resp := get(Handler(Config{Plugins: plugins}), "http://127.0.0.1:4678/api/status", nil)

	want := parseJSON(`{"up": "up", "down": "down"}`)
	if got := decodeJSON(t, resp); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func post(h http.Handler, url string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

func TestClearingIsOfferedForEmbeddedServicesOnly(t *testing.T) {
	tokens := onetime.New(time.Now)
	plugins := []plugin.Plugin{
		{ID: "grafana", Name: plugin.Text{All: "Grafana"}, Page: &plugin.Page{Proxy: "http://127.0.0.1:3000/grafana/"}},
		{ID: "stuck", Name: plugin.Text{All: "Stuck"}, Page: &plugin.Page{Proxy: "http://127.0.0.1:3001/"}},
		{ID: "wiki", Name: plugin.Text{All: "Wiki"}, Page: &plugin.Page{Link: "https://wiki.example/"}},
	}
	served := map[string]Served{
		"grafana": {URL: "http://127.0.0.1:4679/grafana/"},
		"stuck":   {Err: errors.New("port 4680 is in use")},
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", Plugins: plugins, Served: served, Tokens: tokens})

	var got struct{ URL string }
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4678/api/clear/grafana", nil)
	req.Header.Set("Origin", "http://127.0.0.1:4678")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	prefix := "http://127.0.0.1:4679" + proxy.ClearPath + "?token="
	token, ok := strings.CutPrefix(got.URL, prefix)
	if !ok || !tokens.Redeem("grafana", token) {
		t.Errorf("clearing URL %q, want %s and a token for grafana", got.URL, prefix)
	}

	for _, id := range []string{"stuck", "wiki", "nothing"} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4678/api/clear/"+id, nil)
		req.Header.Set("Origin", "http://127.0.0.1:4678")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", id, w.Code)
		}
	}

	for name, origin := range map[string]string{
		"missing Origin": "",
		"wrong Origin":   "http://127.0.0.1:4679",
	} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4678/api/clear/grafana", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, w.Code)
		}
		var body struct {
			Error struct{ Code string }
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil || body.Error.Code != "not_console" {
			t.Errorf("%s: body %s, want not_console", name, w.Body.String())
		}
	}
}

// detailPlugin has a detail page and actions whose commands do not exist: a
// request that gets as far as running them answers 502.
func detailPlugin(t *testing.T) plugin.Plugin {
	missing := []string{t.TempDir() + "/no-such-program"}
	return plugin.Plugin{
		ID:      "disks",
		Dir:     t.TempDir(),
		Name:    plugin.Text{All: "Disks"},
		Detail:  &plugin.Detail{Exec: &plugin.Exec{Command: missing}},
		Actions: &plugin.Actions{Exec: &plugin.Exec{Command: missing}},
	}
}

func postAction(h http.Handler, id string, header map[string]string, body string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4678/api/action/"+id, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

func TestActionsAreTakenFromTheConsolePageOnly(t *testing.T) {
	h := Handler(Config{Origin: "http://127.0.0.1:4678", Plugins: []plugin.Plugin{detailPlugin(t)}})
	const body = `{"item": "a", "action": "open"}`
	for _, tc := range []struct {
		name   string
		header map[string]string
		want   int
	}{
		{"an embedded service's page", map[string]string{"Origin": "http://127.0.0.1:4679", "Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"a page claiming same-origin with another Origin", map[string]string{"Origin": "http://127.0.0.1:4679", "Sec-Fetch-Site": "same-origin"}, http.StatusForbidden},
		{"a program without an Origin", nil, http.StatusForbidden},
		{"the console page", map[string]string{"Origin": "http://127.0.0.1:4678", "Sec-Fetch-Site": "same-origin"}, http.StatusBadGateway},
	} {
		if resp := postAction(h, "disks", tc.header, body); resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
}

func TestDetailAndActionsAnswerOnlyForPluginsThatHaveThem(t *testing.T) {
	withDetail := detailPlugin(t)
	withoutActions := detailPlugin(t)
	withoutActions.ID, withoutActions.Actions = "plain", nil
	h := Handler(Config{Origin: "http://127.0.0.1:4678", Plugins: []plugin.Plugin{withDetail, withoutActions}})
	console := map[string]string{"Origin": "http://127.0.0.1:4678", "Sec-Fetch-Site": "same-origin"}

	if resp := get(h, "http://127.0.0.1:4678/api/detail/nothing", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("detail of a plugin without one: status %d, want 404", resp.StatusCode)
	}
	if resp := postAction(h, "plain", console, `{"item": "a", "action": "open"}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("action of a plugin without actions: status %d, want 404", resp.StatusCode)
	}
	if resp := postAction(h, "disks", console, `not json`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("action with a broken body: status %d, want 400", resp.StatusCode)
	}
	var got struct{ Error struct{ Code string } }
	json.NewDecoder(get(h, "http://127.0.0.1:4678/api/detail/disks", nil).Body).Decode(&got)
	if got.Error.Code != "command_start" {
		t.Errorf("detail whose command is missing: error %q, want command_start", got.Error.Code)
	}
}

func TestPluginsThatDidNotLoadShowAsErrorCards(t *testing.T) {
	path := "/tmp/plugins/bad/plugin.yaml"
	broken := []plugin.Broken{{
		ID:   "bad",
		Name: plugin.Text{ByLang: map[string]string{"en": "Broken", "zh-CN": "坏了"}},
		Err:  &plugin.Located{Path: path, Err: errors.New(`line 3: unknown field "lnk"`)},
	}}

	h := Handler(Config{Broken: broken, PluginDir: "plugins"})
	resp := get(h, "http://127.0.0.1:4678/api/cards", nil)

	want := parseJSON(`{"pluginDir": "plugins", "cards": [
		{"id": "bad", "name": {"en": "Broken", "zh-CN": "坏了"},
		 "error": {"code": "bad_manifest", "detail": "line 3: unknown field \"lnk\"", "file": "/tmp/plugins/bad/plugin.yaml"}}
	]}`)
	if got := decodeJSON(t, resp); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	sidebar := string(mustRead(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body))
	if !strings.Contains(sidebar, `"id":"bad"`) || !strings.Contains(sidebar, `"code":"bad_manifest"`) {
		t.Errorf("broken plugin missing from sidebar: %s", sidebar)
	}
}

func TestAMissingVariableIsItsOwnErrorAndNamesTheVariable(t *testing.T) {
	path := "/tmp/plugins/sync/plugin.yaml"
	broken := []plugin.Broken{{
		ID:   "sync",
		Name: plugin.Text{All: "Syncthing"},
		Err:  &plugin.Located{Path: path, Err: &plugin.EnvMissing{Field: "card.http.headers.X-API-Key", Name: "SYNCTHING_API_KEY"}},
	}}

	resp := get(Handler(Config{Broken: broken, PluginDir: "plugins"}), "http://127.0.0.1:4678/api/cards", nil)

	want := parseJSON(`{"pluginDir": "plugins", "cards": [
		{"id": "sync", "name": "Syncthing",
		 "error": {"code": "env_missing", "detail": "SYNCTHING_API_KEY", "file": "/tmp/plugins/sync/plugin.yaml"}}
	]}`)
	if got := decodeJSON(t, resp); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestThePluginsPageReportsAnAbsoluteDirectoryForAgentCommands(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("plugins", 0700); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("plugins")
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{PluginDir: "plugins", BuiltIn: fstest.MapFS{}})
	body, _ := io.ReadAll(get(h, "http://127.0.0.1:4678/api/catalog", nil).Body)
	if got := gjson.GetBytes(body, "pluginDir").String(); got != want {
		t.Fatalf("pluginDir %q; want absolute directory %q for commands run elsewhere", got, want)
	}
}

func TestThePluginsPageCarriesTheLoadError(t *testing.T) {
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "sync", "schema: 1\nname: Syncthing\npage: {link: 'https://example/'}\n")
	h := Handler(Config{
		PluginDir: pluginDir,
		BuiltIn:   fstest.MapFS{},
		Broken: []plugin.Broken{{
			ID:   "sync",
			Name: plugin.Text{All: "Syncthing"},
			Err:  &plugin.Located{Path: filepath.Join(pluginDir, "sync", "plugin.yaml"), Err: &plugin.EnvMissing{Name: "SYNCTHING_API_KEY"}},
		}},
	})

	body, _ := io.ReadAll(get(h, "http://127.0.0.1:4678/api/catalog", nil).Body)
	got := gjson.GetBytes(body, `installed.#(id=="sync")`).Map()
	if got["error"].Get("code").String() != "env_missing" || got["error"].Get("detail").String() != "SYNCTHING_API_KEY" {
		t.Errorf("catalog error %s, want env_missing SYNCTHING_API_KEY", got["error"].Raw)
	}
	if strings.Contains(got["error"].Get("detail").String(), pluginDir) {
		t.Errorf("detail contains the path: %s", got["error"].Raw)
	}
}

func postFrom(h http.Handler, url string, header map[string]string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, url, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

// catalogState is what /api/catalog says, by plugin ID.
func catalogState(t *testing.T, h http.Handler) (installed map[string]string, available []string) {
	t.Helper()
	body, _ := io.ReadAll(get(h, "http://127.0.0.1:4678/api/catalog", nil).Body)
	var got struct {
		Installed []struct {
			ID       string `json:"id"`
			Disabled bool   `json:"disabled"`
			Loaded   bool   `json:"loaded"`
			Broken   bool   `json:"broken"`
		} `json:"installed"`
		Available []struct {
			ID string `json:"id"`
		} `json:"available"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	installed = map[string]string{}
	for _, p := range got.Installed {
		installed[p.ID] = fmt.Sprintf("disabled=%v loaded=%v broken=%v", p.Disabled, p.Loaded, p.Broken)
	}
	for _, p := range got.Available {
		available = append(available, p.ID)
	}
	return installed, available
}

// writeManifest makes <pluginDir>/<name>/plugin.yaml.
func writeManifest(t *testing.T, pluginDir, name, manifest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(pluginDir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, name, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	fromConsole   = map[string]string{"Origin": "http://127.0.0.1:4678", "Sec-Fetch-Site": "same-origin"}
	fromOtherPage = map[string]map[string]string{
		"an embedded service's page":  {"Origin": "http://127.0.0.1:4679", "Sec-Fetch-Site": "same-site"},
		"a program without an Origin": nil,
	}
)

func TestBuiltInPluginsAreAddedFromTheConsolePageOnly(t *testing.T) {
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "used", "schema: 1\nname: Used\n")
	builtIn := fstest.MapFS{
		"disk/plugin.yaml": {Data: []byte("schema: 1\nname: Disk\ndescription: Free space.\ncard: {exec: {command: [sh, card.sh]}}\n")},
		"used/plugin.yaml": {Data: []byte("schema: 1\nname: Used\n")},
	}
	h := Handler(Config{
		Origin:    "http://127.0.0.1:4678",
		PluginDir: pluginDir,
		BuiltIn:   builtIn,
		Plugins:   []plugin.Plugin{{ID: "used", Name: plugin.Text{All: "Used"}}},
	})

	for name, header := range fromOtherPage {
		if resp := postFrom(h, "http://127.0.0.1:4678/api/catalog/disk", header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "disk")); err == nil {
		t.Fatal("a refused request added the plugin")
	}
	if installed, available := catalogState(t, h); !reflect.DeepEqual(available, []string{"disk"}) || len(installed) != 1 {
		t.Errorf("before adding: installed %v, available %v", installed, available)
	}

	resp := postFrom(h, "http://127.0.0.1:4678/api/catalog/disk", fromConsole)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"path"`) {
		t.Fatalf("adding disk: %d %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "disk", "plugin.yaml")); err != nil {
		t.Errorf("disk was not added: %v", err)
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/catalog/disk", fromConsole); resp.StatusCode != http.StatusConflict {
		t.Errorf("adding disk again: status %d, want 409", resp.StatusCode)
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/catalog/nope", fromConsole); resp.StatusCode != http.StatusNotFound {
		t.Errorf("adding an unknown plugin: status %d, want 404", resp.StatusCode)
	}

	installed, available := catalogState(t, h)
	want := map[string]string{"disk": "disabled=false loaded=false broken=false", "used": "disabled=false loaded=true broken=false"}
	if !reflect.DeepEqual(installed, want) || len(available) != 0 {
		t.Errorf("after adding: installed %v, available %v; want %v and nothing available", installed, available, want)
	}
}

func TestPluginsAreDisabledAndEnabledFromTheConsolePageOnly(t *testing.T) {
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "used", "schema: 1\nname: Used\n")
	writeManifest(t, pluginDir, "bad", "schema: 1\nname: Bad\n")
	h := Handler(Config{
		Origin:    "http://127.0.0.1:4678",
		PluginDir: pluginDir,
		BuiltIn:   fstest.MapFS{},
		Plugins:   []plugin.Plugin{{ID: "used", Name: plugin.Text{All: "Used"}}},
		Broken:    []plugin.Broken{{ID: "bad", Err: errors.New("bad manifest")}},
	})

	for name, header := range fromOtherPage {
		if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/used/disable", header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/used/disable", fromConsole); resp.StatusCode != http.StatusOK {
		t.Fatalf("disabling: status %d", resp.StatusCode)
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/used/disable", fromConsole); resp.StatusCode != http.StatusNotFound {
		t.Errorf("disabling twice: status %d, want 404", resp.StatusCode)
	}
	installed, _ := catalogState(t, h)
	want := map[string]string{"used": "disabled=true loaded=true broken=false", "bad": "disabled=false loaded=true broken=true"}
	if !reflect.DeepEqual(installed, want) {
		t.Errorf("after disabling: %v, want %v", installed, want)
	}

	for name, header := range fromOtherPage {
		if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/used/enable", header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("enabling from %s: status %d, want 403", name, resp.StatusCode)
		}
	}
	writeManifest(t, pluginDir, "used", "schema: 1\nname: A new one\n")
	if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/used/enable", fromConsole); resp.StatusCode != http.StatusConflict {
		t.Errorf("enabling over a new folder of that name: status %d, want 409", resp.StatusCode)
	}
	os.RemoveAll(filepath.Join(pluginDir, "used"))
	if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/used/enable", fromConsole); resp.StatusCode != http.StatusOK {
		t.Errorf("enabling: status %d", resp.StatusCode)
	}
	if installed, _ := catalogState(t, h); installed["used"] != "disabled=false loaded=true broken=false" {
		t.Errorf("after enabling: %v", installed)
	}
}

// arranged is a layout store in a temporary folder that holds l.
func arranged(t *testing.T, l layout.Layout) *layout.Store {
	t.Helper()
	s, err := layout.Open(filepath.Join(t.TempDir(), "layout.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(l); err != nil {
		t.Fatal(err)
	}
	return s
}

func ids(t *testing.T, body io.Reader, path string) []string {
	t.Helper()
	data, _ := io.ReadAll(body)
	var got []string
	for _, r := range gjson.GetBytes(data, path).Array() {
		got = append(got, r.String())
	}
	return got
}

func TestTheSidebarFollowsTheSavedOrder(t *testing.T) {
	plugins := []plugin.Plugin{
		{ID: "disk", Name: plugin.Text{All: "Disk"}, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"sh", "detail.sh"}}}},
		{ID: "grafana", Name: plugin.Text{All: "Grafana"}, Page: &plugin.Page{Link: "http://127.0.0.1:3000/"}},
		{ID: "wiki", Name: plugin.Text{All: "Wiki"}, Page: &plugin.Page{Link: "https://wiki.example/"}},
	}
	h := Handler(Config{Plugins: plugins, Layout: arranged(t, layout.Layout{Sidebar: []string{"wiki", "disk"}})})

	got := ids(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body, "#.id")
	if want := []string{"wiki", "disk", "grafana"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sidebar %v, want %v", got, want)
	}
}

func TestCardsFollowTheSavedOrderAndSayWhichAreWide(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"v": 1}`)
	}))
	defer svc.Close()
	withCard := func(id string) plugin.Plugin {
		return plugin.Plugin{ID: id, Name: plugin.Text{All: id}, Card: &plugin.Card{HTTP: &plugin.HTTP{URL: svc.URL}, Value: "v"}}
	}
	h := Handler(Config{
		Plugins: []plugin.Plugin{withCard("disk"), withCard("grafana"), withCard("ollama")},
		Broken:  []plugin.Broken{{ID: "typo", Err: errors.New("bad")}},
		Layout:  arranged(t, layout.Layout{Cards: []string{"typo", "ollama", "disk"}, Wide: []string{"disk", "gone"}}),
	})

	body, _ := io.ReadAll(get(h, "http://127.0.0.1:4678/api/cards", nil).Body)
	var got []string
	for _, c := range gjson.GetBytes(body, "cards").Array() {
		got = append(got, fmt.Sprintf("%s wide=%v", c.Get("id"), c.Get("wide").Bool()))
	}
	want := []string{"typo wide=false", "ollama wide=false", "disk wide=true", "grafana wide=false"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cards %v, want %v", got, want)
	}
}

func postLayout(h http.Handler, header map[string]string, body string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4678/api/layout", strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

func TestTheLayoutIsSavedFromTheConsolePageOnly(t *testing.T) {
	file := filepath.Join(t.TempDir(), "layout.json")
	store, err := layout.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	before := layout.Layout{Sidebar: []string{"a", "b"}, Cards: []string{"b", "a"}, Wide: []string{"a"}}
	store.Set(before)
	h := Handler(Config{Origin: "http://127.0.0.1:4678", Layout: store})

	for name, header := range fromOtherPage {
		if resp := postLayout(h, header, `{"sidebar": ["b", "a"]}`); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	if resp := postLayout(h, fromConsole, `{"sidebar": [`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("half a body: status %d, want 400", resp.StatusCode)
	}
	if got := store.Get(); !reflect.DeepEqual(got, before) {
		t.Fatalf("refused requests changed the layout to %+v", got)
	}

	// Moving one sidebar entry sends the sidebar only; the cards stay as they were.
	if resp := postLayout(h, fromConsole, `{"sidebar": ["b", "a"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	reopened, err := layout.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	want := layout.Layout{Sidebar: []string{"b", "a"}, Cards: []string{"b", "a"}, Wide: []string{"a"}}
	if got := reopened.Get(); !reflect.DeepEqual(got, want) {
		t.Errorf("saved %+v, want %+v", got, want)
	}

	// Narrowing the last wide card sends an empty list, which counts.
	postLayout(h, fromConsole, `{"wide": []}`)
	if got := store.Get().Wide; len(got) != 0 {
		t.Errorf("wide %v, want none", got)
	}
}

// Hiding drops a plugin from the sidebar and the cards and leaves it installed.
// Reset clears the order, the wide cards and what was hidden.
func TestHidingDropsAPluginFromTheSidebarAndTheCardsAndResetClearsTheLayout(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"v": 1}`)
	}))
	defer svc.Close()
	withCard := func(id string) plugin.Plugin {
		return plugin.Plugin{ID: id, Name: plugin.Text{All: id}, Page: &plugin.Page{Link: "https://" + id + ".example/"}, Card: &plugin.Card{HTTP: &plugin.HTTP{URL: svc.URL}, Value: "v"}}
	}
	file := filepath.Join(t.TempDir(), "layout.json")
	store, err := layout.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(layout.Layout{Sidebar: []string{"b", "a"}, Cards: []string{"b", "a"}, Wide: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{
		Origin:  "http://127.0.0.1:4678",
		Plugins: []plugin.Plugin{withCard("a"), withCard("b")},
		Layout:  store,
	})

	if resp := postLayout(h, fromOtherPage["an embedded service's page"], `{"hidden": ["a"]}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("hiding from another page: status %d, want 403", resp.StatusCode)
	}
	if resp := postLayout(h, fromConsole, `{"hidden": ["a"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("hiding: status %d, want 200", resp.StatusCode)
	}
	if got, want := ids(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body, "#.id"), []string{"b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sidebar %v, want %v", got, want)
	}
	if got, want := ids(t, get(h, "http://127.0.0.1:4678/api/cards", nil).Body, "cards.#.id"), []string{"b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cards %v, want %v", got, want)
	}
	reopened, err := layout.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	want := layout.Layout{Sidebar: []string{"b", "a"}, Cards: []string{"b", "a"}, Wide: []string{"a"}, Hidden: []string{"a"}}
	if got := reopened.Get(); !reflect.DeepEqual(got, want) {
		t.Errorf("saved %+v, want %+v", got, want)
	}
	if got, wantIDs := ids(t, get(h, "http://127.0.0.1:4678/api/layout", nil).Body, "hidden"), []string{"a"}; !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("GET /api/layout hidden %v, want %v", got, wantIDs)
	}

	// The page sends the sidebar it still shows. The hidden plugin keeps its place.
	if resp := postLayout(h, fromConsole, `{"sidebar": ["b"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("reordering while hidden: status %d, want 200", resp.StatusCode)
	}
	kept := layout.Layout{Sidebar: []string{"b", "a"}, Cards: []string{"b", "a"}, Wide: []string{"a"}, Hidden: []string{"a"}}
	if got := store.Get(); !reflect.DeepEqual(got, kept) {
		t.Errorf("after reordering while a is hidden %+v, want %+v", got, kept)
	}
	// A new order of the same length must not be written over the places just read.
	if err := store.Set(layout.Layout{Sidebar: []string{"c", "b", "a"}, Cards: []string{"b", "a"}, Wide: []string{"a"}, Hidden: []string{"b"}}); err != nil {
		t.Fatal(err)
	}
	if resp := postLayout(h, fromConsole, `{"sidebar": ["a", "c"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("reordering past a hidden entry: status %d, want 200", resp.StatusCode)
	}
	kept = layout.Layout{Sidebar: []string{"a", "b", "c"}, Cards: []string{"b", "a"}, Wide: []string{"a"}, Hidden: []string{"b"}}
	if got := store.Get(); !reflect.DeepEqual(got, kept) {
		t.Errorf("same-length reorder while b is hidden %+v, want %+v", got, kept)
	}

	if resp := postLayout(h, fromConsole, `{"reset": true}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("reset: status %d, want 200", resp.StatusCode)
	}
	if got := store.Get(); !reflect.DeepEqual(got, layout.Layout{}) {
		t.Errorf("after reset %+v, want an empty layout", got)
	}
	if got, want := ids(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body, "#.id"), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sidebar after reset %v, want %v", got, want)
	}
}

// reloadFrom reads pluginDir again, as hostdeck does at start.
func reloadFrom(pluginDir string) func() (Loaded, error) {
	return func() (Loaded, error) {
		plugins, broken, err := plugin.Load(pluginDir)
		return Loaded{Plugins: plugins, Broken: broken}, err
	}
}

func TestAddingDisablingAndEnablingTakeEffectAtOnce(t *testing.T) {
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "site", "schema: 1\nname: Site\npage: {link: 'https://site.example/'}\n")
	reload := reloadFrom(pluginDir)
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{
		Origin:    "http://127.0.0.1:4678",
		PluginDir: pluginDir,
		BuiltIn:   fstest.MapFS{"wiki/plugin.yaml": {Data: []byte("schema: 1\nname: Wiki\npage: {link: 'https://wiki.example/'}\n")}},
		Plugins:   loaded.Plugins,
		Reload:    reload,
	})
	sidebar := func() []string {
		t.Helper()
		return ids(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body, "#.id")
	}

	if resp := postFrom(h, "http://127.0.0.1:4678/api/catalog/wiki", fromConsole); resp.StatusCode != http.StatusOK {
		t.Fatalf("adding: status %d", resp.StatusCode)
	}
	if got := sidebar(); !reflect.DeepEqual(got, []string{"site", "wiki"}) {
		t.Errorf("after adding, the sidebar has %v", got)
	}
	if installed, _ := catalogState(t, h); installed["wiki"] != "disabled=false loaded=true broken=false" {
		t.Errorf("after adding: %v", installed)
	}

	if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/site/disable", fromConsole); resp.StatusCode != http.StatusOK {
		t.Fatalf("disabling: status %d", resp.StatusCode)
	}
	if got := sidebar(); !reflect.DeepEqual(got, []string{"wiki"}) {
		t.Errorf("after disabling, the sidebar has %v", got)
	}
	if installed, _ := catalogState(t, h); installed["site"] != "disabled=true loaded=false broken=false" {
		t.Errorf("after disabling: %v", installed)
	}

	if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/site/enable", fromConsole); resp.StatusCode != http.StatusOK {
		t.Fatalf("enabling: status %d", resp.StatusCode)
	}
	if got := sidebar(); !reflect.DeepEqual(got, []string{"site", "wiki"}) {
		t.Errorf("after enabling, the sidebar has %v", got)
	}
}

func TestAPluginEnabledAgainShowsAFreshCard(t *testing.T) {
	asked := 0
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		fmt.Fprintf(w, `{"asked": %d}`, asked)
	}))
	defer svc.Close()
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "demo", fmt.Sprintf("schema: 1\nname: Demo\ncard:\n  http: {url: '%s'}\n  value: asked\n  interval: 1h\n", svc.URL))
	reload := reloadFrom(pluginDir)
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Plugins: loaded.Plugins, Reload: reload})
	value := func() []string {
		t.Helper()
		return ids(t, get(h, "http://127.0.0.1:4678/api/cards", nil).Body, "cards.#.value")
	}

	if got := value(); !reflect.DeepEqual(got, []string{"1"}) {
		t.Fatalf("first card: %v", got)
	}
	postFrom(h, "http://127.0.0.1:4678/api/plugins/demo/disable", fromConsole)
	if got := value(); len(got) != 0 {
		t.Errorf("a disabled plugin still has a card: %v", got)
	}
	postFrom(h, "http://127.0.0.1:4678/api/plugins/demo/enable", fromConsole)
	if got := value(); !reflect.DeepEqual(got, []string{"2"}) {
		t.Errorf("after enabling, the card says %v, want it asked again", got)
	}
}

// Refresh forgets the kept card, and only the console page may ask for that.
func TestARefreshAsksTheCardAgainBeforeItsInterval(t *testing.T) {
	asked := 0
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		fmt.Fprintf(w, `{"asked": %d}`, asked)
	}))
	defer svc.Close()
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "demo", fmt.Sprintf("schema: 1\nname: Demo\ncard:\n  http: {url: '%s'}\n  value: asked\n  interval: 1h\n", svc.URL))
	reload := reloadFrom(pluginDir)
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Plugins: loaded.Plugins, Reload: reload})
	body := func() []byte {
		t.Helper()
		b, _ := io.ReadAll(get(h, "http://127.0.0.1:4678/api/cards", nil).Body)
		return b
	}

	first := body()
	if gjson.GetBytes(first, "cards.0.value").String() != "1" || gjson.GetBytes(first, "cards.0.updated").String() == "" {
		t.Fatalf("first card: %s", first)
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/cards/refresh", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("refresh from elsewhere: status %d, want 403", resp.StatusCode)
	}
	if got := gjson.GetBytes(body(), "cards.0.value").String(); got != "1" || asked != 1 {
		t.Errorf("after a refused refresh: value %s after %d requests, want the kept 1", got, asked)
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/cards/refresh", fromConsole); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("refresh: status %d, want 204", resp.StatusCode)
	}
	if got := gjson.GetBytes(body(), "cards.0.value").String(); got != "2" {
		t.Errorf("after refresh: value %s, want it asked again", got)
	}
}

func TestAFailedReloadKeepsWhatWasLoadedAndSaysWhy(t *testing.T) {
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "site", "schema: 1\nname: Site\npage: {link: 'https://site.example/'}\n")
	h := Handler(Config{
		Origin:    "http://127.0.0.1:4678",
		PluginDir: pluginDir,
		BuiltIn:   fstest.MapFS{},
		Plugins:   []plugin.Plugin{{ID: "site", Name: plugin.Text{All: "Site"}, Page: &plugin.Page{Link: "https://site.example/"}}},
		Reload:    func() (Loaded, error) { return Loaded{}, errors.New("ports.json: permission denied") },
	})

	resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/site/disable", fromConsole)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError || gjson.GetBytes(body, "error.code").String() != "reload_failed" || !strings.Contains(string(body), "permission denied") {
		t.Errorf("got %d %s, want 500 reload_failed with the reason", resp.StatusCode, body)
	}
	if got := ids(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body, "#.id"); !reflect.DeepEqual(got, []string{"site"}) {
		t.Errorf("the sidebar has %v, want what was loaded before", got)
	}
}

func postJSON(h http.Handler, url, body string, header map[string]string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result()
}

func TestAWebPageIsEmbeddedFromItsAddressAtOnce(t *testing.T) {
	pluginDir := t.TempDir()
	reload := reloadFrom(pluginDir)
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Reload: reload})

	for name, header := range fromOtherPage {
		if resp := postJSON(h, "http://127.0.0.1:4678/api/pages", `{"address": "127.0.0.1:8080"}`, header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	for body, code := range map[string]string{
		`{"address": "http://93.184.215.14/"}`: "not_local",
		`{"address": "ftp://127.0.0.1/"}`:      "not_web",
		`not json`:                             "bad_request",
	} {
		resp := postJSON(h, "http://127.0.0.1:4678/api/pages", body, fromConsole)
		got, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadRequest || gjson.GetBytes(got, "error.code").String() != code {
			t.Errorf("%s: %d %s, want 400 %s", body, resp.StatusCode, got, code)
		}
	}
	if entries, _ := os.ReadDir(pluginDir); len(entries) != 0 {
		t.Fatalf("refused requests left %d entries in the plugins folder", len(entries))
	}

	resp := postJSON(h, "http://127.0.0.1:4678/api/pages", `{"address": "127.0.0.1:8080/app/", "name": "App"}`, fromConsole)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || gjson.GetBytes(body, "id").String() != "127-0-0-1-8080" {
		t.Fatalf("adding: %d %s", resp.StatusCode, body)
	}
	got := string(mustRead(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body))
	if gjson.Get(got, "#.id").String() != `["127-0-0-1-8080"]` || gjson.Get(got, "0.name").String() != "App" || gjson.Get(got, "0.type").String() != "proxy" {
		t.Errorf("the sidebar has %s, want the page, named App, embedded", got)
	}
}

func mustRead(t *testing.T, r io.Reader) []byte {
	t.Helper()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Editing a page writes its name, icon and embed address back into plugin.yaml
// and nothing else. The remembered forwarding port stays in ports.json.
func TestEditingAPageChangesItsNameIconAndAddressAndLeavesItsCommands(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins")
	const manifest = `# kept on purpose
schema: 1
name: {en: Meter, zh-CN: 仪表}
icon: M
page:
  proxy: http://127.0.0.1:9/old
card:
  exec:
    command: [echo, detail] # leave this command
`
	writeManifest(t, pluginDir, "meter", manifest)
	portsFile := filepath.Join(root, "ports.json")
	const ports = "{\"meter\": 4679}\n"
	if err := os.WriteFile(portsFile, []byte(ports), 0o644); err != nil {
		t.Fatal(err)
	}
	reload := reloadFrom(pluginDir)
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Plugins: loaded.Plugins, Reload: reload})
	manifestPath := filepath.Join(pluginDir, "meter", "plugin.yaml")
	unchanged := func() {
		t.Helper()
		if got := readFile(t, manifestPath); got != manifest {
			t.Errorf("plugin.yaml changed:\n%s", got)
		}
		if got := readFile(t, portsFile); got != ports {
			t.Errorf("ports.json changed: %s", got)
		}
	}

	for name, header := range fromOtherPage {
		resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"name": "Nope"}`, header)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	for body, code := range map[string]string{
		`{"proxy": "http://93.184.215.14/"}`:                                "not_local",
		`{"proxy": "ftp://127.0.0.1/"}`:                                     "not_web",
		`{"proxy": "http://127.0.0.1:9/a", "link": "https://example.com/"}`: "bad_request",
		`{"name": ""}`:    "bad_request",
		`{"name": "   "}`: "bad_request",
	} {
		resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", body, fromConsole)
		got, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadRequest || gjson.GetBytes(got, "error.code").String() != code {
			t.Errorf("%s: %d %s, want 400 %s", body, resp.StatusCode, got, code)
		}
	}
	if resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/missing/page", `{"name": "Nope"}`, fromConsole); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing plugin: status %d, want 404", resp.StatusCode)
	}
	unchanged()

	resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"name": "Gauge", "icon": "G", "proxy": "http://127.0.0.1:9/now"}`, fromConsole)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editing: %d %s", resp.StatusCode, body)
	}
	got := readFile(t, manifestPath)
	for _, want := range []string{"# kept on purpose", "# leave this command", "echo", "http://127.0.0.1:9/now", "Gauge"} {
		if !strings.Contains(got, want) {
			t.Errorf("plugin.yaml missing %q:\n%s", want, got)
		}
	}
	for _, gone := range []string{"zh-CN", "仪表", "/old", "icon: M"} {
		if strings.Contains(got, gone) {
			t.Errorf("plugin.yaml still has %q:\n%s", gone, got)
		}
	}
	if portsGot := readFile(t, portsFile); portsGot != ports {
		t.Errorf("ports.json is %s, want it unchanged", portsGot)
	}
	listed := string(mustRead(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body))
	if gjson.Get(listed, "0.id").String() != "meter" || gjson.Get(listed, "0.name").String() != "Gauge" || gjson.Get(listed, "0.icon").String() != "G" || gjson.Get(listed, "0.type").String() != "proxy" {
		t.Errorf("the sidebar has %s, want meter named Gauge with icon G, still embedded", listed)
	}

	resp = postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"name": "Dial"}`, fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editing the name only: %d %s", resp.StatusCode, body)
	}
	got = readFile(t, manifestPath)
	if !strings.Contains(got, "Dial") || !strings.Contains(got, "http://127.0.0.1:9/now") || (!strings.Contains(got, "icon: G") && !strings.Contains(got, `icon: "G"`)) {
		t.Errorf("a name-only edit touched more than the name:\n%s", got)
	}
	if !strings.Contains(got, "# kept on purpose") || !strings.Contains(got, "# leave this command") {
		t.Errorf("comments were dropped:\n%s", got)
	}

	writeManifest(t, pluginDir, ".off", "schema: 1\nname: Off\npage:\n  link: https://example.com/old\n")
	resp = postJSON(h, "http://127.0.0.1:4678/api/plugins/off/page", `{"name": "Still off", "link": "https://example.com/new"}`, fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editing a disabled plugin: %d %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "off")); !os.IsNotExist(err) {
		t.Fatalf("editing a disabled plugin enabled its folder: %v", err)
	}
	if got := readFile(t, filepath.Join(pluginDir, ".off", "plugin.yaml")); !strings.Contains(got, "Still off") || !strings.Contains(got, "https://example.com/new") {
		t.Errorf("disabled plugin manifest is:\n%s", got)
	}
}

// A link replaces an embed address, and an embed address replaces a link.
// A link may point at the internet. It still has to be a web address.
func TestEditingAPageToALinkRemovesTheEmbedAddress(t *testing.T) {
	pluginDir := t.TempDir()
	const manifest = "schema: 1\nname: Meter\npage:\n  proxy: http://127.0.0.1:9/old\n"
	writeManifest(t, pluginDir, "meter", manifest)
	reload := reloadFrom(pluginDir)
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Plugins: loaded.Plugins, Reload: reload})
	manifestPath := filepath.Join(pluginDir, "meter", "plugin.yaml")

	resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"link": "ftp://files.example/meter"}`, fromConsole)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || gjson.GetBytes(body, "error.code").String() != "not_web" {
		t.Fatalf("an ftp link: %d %s, want 400 not_web", resp.StatusCode, body)
	}
	if got := readFile(t, manifestPath); got != manifest {
		t.Fatalf("a refused link changed plugin.yaml:\n%s", got)
	}

	resp = postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"link": "https://example.com/meters"}`, fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setting a link: %d %s", resp.StatusCode, body)
	}
	got := readFile(t, manifestPath)
	if !strings.Contains(got, "https://example.com/meters") || strings.Contains(got, "proxy") {
		t.Fatalf("after the link, plugin.yaml is:\n%s", got)
	}
	listed := string(mustRead(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body))
	if gjson.Get(listed, "0.type").String() != "link" || gjson.Get(listed, "0.url").String() != "https://example.com/meters" {
		t.Fatalf("the sidebar has %s, want a link to the page", listed)
	}

	resp = postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"proxy": "http://127.0.0.1:9/now"}`, fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("embedding again: %d %s", resp.StatusCode, body)
	}
	got = readFile(t, manifestPath)
	if !strings.Contains(got, "http://127.0.0.1:9/now") || strings.Contains(got, "link") || strings.Contains(got, "example.com") {
		t.Fatalf("after embedding again, plugin.yaml is:\n%s", got)
	}
	listed = string(mustRead(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body))
	if gjson.Get(listed, "0.type").String() != "proxy" {
		t.Fatalf("the sidebar has %s, want it embedded again", listed)
	}
}

// The plugins page can show a page's icon and address. It does not receive
// the plugin's commands.
func TestTheCatalogShowsAPageAddressAndNotItsCommands(t *testing.T) {
	pluginDir := t.TempDir()
	writeManifest(t, pluginDir, "meter", `schema: 1
name: Meter
icon: M
page:
  proxy: http://127.0.0.1:9/old
card:
  http: {url: 'http://127.0.0.1:9/metrics'}
  value: n
detail:
  exec:
    command: [echo, secret-command]
`)
	writeManifest(t, pluginDir, "notes", "schema: 1\nname: Notes\npage: {link: 'https://example.com/notes'}\n")
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}})

	body := string(mustRead(t, get(h, "http://127.0.0.1:4678/api/catalog", nil).Body))
	if strings.Contains(body, "secret-command") || strings.Contains(body, "echo") {
		t.Fatalf("the catalog includes a command:\n%s", body)
	}
	meter := gjson.Get(body, `installed.#(id=="meter")`)
	if meter.Get("icon").String() != "M" || meter.Get("proxy").String() != "http://127.0.0.1:9/old" || meter.Get("cardUrl").String() != "http://127.0.0.1:9/metrics" || meter.Get("link").String() != "" {
		t.Errorf("meter is %s, want its icon, embed address and card address", meter.Raw)
	}
	notes := gjson.Get(body, `installed.#(id=="notes")`)
	if notes.Get("link").String() != "https://example.com/notes" || notes.Get("proxy").String() != "" {
		t.Errorf("notes is %s, want its link and no embed address", notes.Raw)
	}
}

// The card address is the declarative card.http.url only. A command card is
// left alone, and a plugin without that URL does not gain a card.
func TestEditingACardAddressChangesOnlyADeclarativeURL(t *testing.T) {
	pluginDir := t.TempDir()
	const meter = `# kept on purpose
schema: 1
name: Meter
page:
  proxy: http://127.0.0.1:9/old
card:
  http: {url: 'http://127.0.0.1:9/metrics', headers: {Accept: text/plain}}
  value: n
`
	const tool = `schema: 1
name: Tool
page: {link: 'https://example.com/tool'}
card:
  exec:
    command: [echo, tool] # leave this command
`
	const plain = "schema: 1\nname: Plain\npage: {link: 'https://example.com/plain'}\n"
	writeManifest(t, pluginDir, "meter", meter)
	writeManifest(t, pluginDir, "tool", tool)
	writeManifest(t, pluginDir, "plain", plain)
	reload := reloadFrom(pluginDir)
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Plugins: loaded.Plugins, Reload: reload})
	file := func(id string) string {
		t.Helper()
		return readFile(t, filepath.Join(pluginDir, id, "plugin.yaml"))
	}

	resp := postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"cardUrl": "ftp://127.0.0.1/metrics"}`, fromConsole)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || gjson.GetBytes(body, "error.code").String() != "not_web" {
		t.Fatalf("an ftp card address: %d %s, want 400 not_web", resp.StatusCode, body)
	}
	if file("meter") != meter {
		t.Fatalf("a refused card address changed plugin.yaml:\n%s", file("meter"))
	}

	for _, id := range []string{"tool", "plain"} {
		resp = postJSON(h, "http://127.0.0.1:4678/api/plugins/"+id+"/page", `{"cardUrl": "http://127.0.0.1:9/now"}`, fromConsole)
		body, _ = io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadRequest || gjson.GetBytes(body, "error.code").String() != "no_card_url" {
			t.Fatalf("%s: %d %s, want 400 no_card_url", id, resp.StatusCode, body)
		}
	}
	if got := file("tool"); got != tool || !strings.Contains(got, "echo") || !strings.Contains(got, "# leave this command") {
		t.Fatalf("the command card changed:\n%s", got)
	}
	if got := file("plain"); got != plain || strings.Contains(got, "card:") {
		t.Fatalf("a page without a card gained one:\n%s", got)
	}

	resp = postJSON(h, "http://127.0.0.1:4678/api/plugins/meter/page", `{"cardUrl": "http://127.0.0.1:9/now"}`, fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editing the card address: %d %s", resp.StatusCode, body)
	}
	got := file("meter")
	for _, want := range []string{"# kept on purpose", "http://127.0.0.1:9/now", "http://127.0.0.1:9/old", "text/plain", "value: n"} {
		if !strings.Contains(got, want) {
			t.Errorf("plugin.yaml missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/metrics") {
		t.Errorf("the old card address is still there:\n%s", got)
	}
}

// Adding a link writes page.link and no command. The address need not be local.
// Leaving mode out still embeds, and still refuses an address on the internet.
func TestAddingALinkDoesNotRequireItToBeLocalAndWritesNoCommand(t *testing.T) {
	pluginDir := t.TempDir()
	reload := reloadFrom(pluginDir)
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Reload: reload})
	empty := func() {
		t.Helper()
		entries, err := os.ReadDir(pluginDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("the plugins folder holds %d entries, want none", len(entries))
		}
	}

	for name, header := range fromOtherPage {
		resp := postJSON(h, "http://127.0.0.1:4678/api/pages", `{"address": "https://example.com/notes", "mode": "link"}`, header)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	for body, code := range map[string]string{
		`{"address": "ftp://files.example/a", "mode": "link"}`: "not_web",
		`{"address": "javascript:alert(1)", "mode": "link"}`:   "not_web",
		`{"address": "https://example.com/", "mode": "nope"}`:  "bad_request",
		`{"address": "http://93.184.215.14/"}`:                 "not_local",
	} {
		resp := postJSON(h, "http://127.0.0.1:4678/api/pages", body, fromConsole)
		got, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadRequest || gjson.GetBytes(got, "error.code").String() != code {
			t.Errorf("%s: %d %s, want 400 %s", body, resp.StatusCode, got, code)
		}
	}
	empty()

	resp := postJSON(h, "http://127.0.0.1:4678/api/pages", `{"address": "https://example.com/notes", "name": "Notes", "mode": "link"}`, fromConsole)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || gjson.GetBytes(body, "id").String() != "example-com" {
		t.Fatalf("adding a link: %d %s, want 200 example-com", resp.StatusCode, body)
	}
	got := readFile(t, filepath.Join(pluginDir, "example-com", "plugin.yaml"))
	if !strings.Contains(got, "https://example.com/notes") || !strings.Contains(got, "Notes") || strings.Contains(got, "proxy") || strings.Contains(got, "command") || strings.Contains(got, "exec") {
		t.Fatalf("plugin.yaml is:\n%s", got)
	}
	listed := string(mustRead(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body))
	if gjson.Get(listed, `#(id=="example-com").type`).String() != "link" || gjson.Get(listed, `#(id=="example-com").url`).String() != "https://example.com/notes" {
		t.Fatalf("the sidebar has %s, want a link to the page", listed)
	}

	resp = postJSON(h, "http://127.0.0.1:4678/api/pages", `{"address": "127.0.0.1:9/here", "mode": "link"}`, fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || gjson.GetBytes(body, "id").String() != "127-0-0-1-9" {
		t.Fatalf("a local address as a link: %d %s, want 200 127-0-0-1-9", resp.StatusCode, body)
	}
	got = readFile(t, filepath.Join(pluginDir, "127-0-0-1-9", "plugin.yaml"))
	if !strings.Contains(got, "http://127.0.0.1:9/here") || strings.Contains(got, "proxy") {
		t.Fatalf("a local link was not stored as a link:\n%s", got)
	}

	for _, tc := range []struct{ address, wantID string }{
		{"https://内网/notes", "page"},
		{"https://内网/more", "page-2"},
		{"https://内网:8080/notes", "page-8080"},
	} {
		resp = postJSON(h, "http://127.0.0.1:4678/api/pages", fmt.Sprintf(`{"address": %q, "mode": "link"}`, tc.address), fromConsole)
		body, _ = io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || gjson.GetBytes(body, "id").String() != tc.wantID {
			t.Fatalf("%s: %d %s, want 200 %s", tc.address, resp.StatusCode, body, tc.wantID)
		}
	}
}

// Moving a plugin to the trash takes it out of the sidebar. Restoring puts
// the folder back, enabled. Purging deletes only that trash folder. The
// remembered forwarding port stays in ports.json.
func TestMovingAPluginToTheTrashDropsItUntilItIsRestoredOrPurged(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins")
	writeManifest(t, pluginDir, "meter", "schema: 1\nname: Meter\npage:\n  link: https://example.com/notes\n")
	writeManifest(t, pluginDir, "keep", "schema: 1\nname: Keep\npage:\n  link: https://example.com/keep\n")
	writeManifest(t, pluginDir, ".off", "schema: 1\nname: Off\npage:\n  link: https://example.com/off\n")
	portsFile := filepath.Join(root, "ports.json")
	const ports = "{\"meter\": 4679}\n"
	if err := os.WriteFile(portsFile, []byte(ports), 0o644); err != nil {
		t.Fatal(err)
	}
	reload := reloadFrom(pluginDir)
	loaded, err := reload()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Origin: "http://127.0.0.1:4678", PluginDir: pluginDir, BuiltIn: fstest.MapFS{}, Plugins: loaded.Plugins, Reload: reload})
	sidebar := func() []string {
		t.Helper()
		return ids(t, get(h, "http://127.0.0.1:4678/api/plugins", nil).Body, "#.id")
	}
	portsSame := func() {
		t.Helper()
		if got := readFile(t, portsFile); got != ports {
			t.Errorf("ports.json is %s, want it unchanged", got)
		}
	}
	catalog := func() string {
		t.Helper()
		return string(mustRead(t, get(h, "http://127.0.0.1:4678/api/catalog", nil).Body))
	}

	for name, header := range fromOtherPage {
		if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/meter/trash", header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "meter", "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".trash")); !os.IsNotExist(err) {
		t.Fatalf("a refused request created the trash: %v", err)
	}
	portsSame()
	if resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/nope/trash", fromConsole); resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("unknown plugin: %d %s, want 404", resp.StatusCode, body)
	}

	resp := postFrom(h, "http://127.0.0.1:4678/api/plugins/meter/trash", fromConsole)
	body, _ := io.ReadAll(resp.Body)
	folder := gjson.GetBytes(body, "folder").String()
	if resp.StatusCode != http.StatusOK || !regexp.MustCompile(`^meter-\d{8}T\d{6}\.\d{9}Z$`).MatchString(folder) {
		t.Fatalf("trashing: %d %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "meter")); !os.IsNotExist(err) {
		t.Fatalf("meter is still installed: %v", err)
	}
	if got := readFile(t, filepath.Join(pluginDir, ".trash", folder, "plugin.yaml")); !strings.Contains(got, "https://example.com/notes") {
		t.Errorf("trashed manifest is %s", got)
	}
	if got, want := sidebar(), []string{"keep"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sidebar after trash %v, want %v", got, want)
	}
	listed := catalog()
	if gjson.Get(listed, `installed.#(id=="meter")`).Exists() {
		t.Errorf("catalog still lists meter: %s", listed)
	}
	if gjson.Get(listed, "trash.#").Int() != 1 || gjson.Get(listed, "trash.0.id").String() != "meter" || gjson.Get(listed, "trash.0.folder").String() != folder || gjson.Get(listed, "trash.0.name").String() != "Meter" {
		t.Errorf("catalog trash is %s, want meter in %s", gjson.Get(listed, "trash").Raw, folder)
	}
	portsSame()

	for name, header := range fromOtherPage {
		if resp := postFrom(h, "http://127.0.0.1:4678/api/trash/"+folder+"/restore", header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("restore from %s: status %d, want 403", name, resp.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".trash", folder, "plugin.yaml")); err != nil {
		t.Fatal("a refused restore moved the trash folder")
	}

	writeManifest(t, pluginDir, "meter", "schema: 1\nname: Other\npage:\n  link: https://example.com/other\n")
	resp = postFrom(h, "http://127.0.0.1:4678/api/trash/"+folder+"/restore", fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusConflict || gjson.GetBytes(body, "error.code").String() != "name_taken" {
		t.Fatalf("restore over an existing folder: %d %s, want 409 name_taken", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".trash", folder, "plugin.yaml")); err != nil {
		t.Fatal("a refused restore removed the trash folder")
	}
	if got := readFile(t, filepath.Join(pluginDir, "meter", "plugin.yaml")); !strings.Contains(got, "https://example.com/other") {
		t.Errorf("the folder already there was changed: %s", got)
	}
	if err := os.RemoveAll(filepath.Join(pluginDir, "meter")); err != nil {
		t.Fatal(err)
	}

	resp = postFrom(h, "http://127.0.0.1:4678/api/trash/"+folder+"/restore", fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore: %d %s", resp.StatusCode, body)
	}
	if got := readFile(t, filepath.Join(pluginDir, "meter", "plugin.yaml")); !strings.Contains(got, "https://example.com/notes") {
		t.Errorf("restored manifest is %s", got)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".trash", folder)); !os.IsNotExist(err) {
		t.Fatalf("restored folder is still in the trash: %v", err)
	}
	if got, want := sidebar(), []string{"keep", "meter"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sidebar after restore %v, want %v", got, want)
	}

	resp = postFrom(h, "http://127.0.0.1:4678/api/plugins/off/trash", fromConsole)
	body, _ = io.ReadAll(resp.Body)
	offFolder := gjson.GetBytes(body, "folder").String()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(offFolder, "off-") {
		t.Fatalf("trashing a disabled plugin: %d %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".off")); !os.IsNotExist(err) {
		t.Fatalf(".off is still there: %v", err)
	}
	resp = postFrom(h, "http://127.0.0.1:4678/api/trash/"+offFolder+"/restore", fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restoring a plugin that was disabled: %d %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "off", "plugin.yaml")); err != nil {
		t.Fatalf("it did not come back enabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".off")); !os.IsNotExist(err) {
		t.Fatalf("the disabled folder is still there: %v", err)
	}
	if got, want := sidebar(), []string{"keep", "meter", "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sidebar after restoring a disabled plugin %v, want %v", got, want)
	}

	resp = postFrom(h, "http://127.0.0.1:4678/api/plugins/keep/trash", fromConsole)
	body, _ = io.ReadAll(resp.Body)
	keepFolder := gjson.GetBytes(body, "folder").String()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(keepFolder, "keep-") {
		t.Fatalf("trashing keep: %d %s", resp.StatusCode, body)
	}
	for name, header := range fromOtherPage {
		if resp := postFrom(h, "http://127.0.0.1:4678/api/trash/"+keepFolder+"/purge", header); resp.StatusCode != http.StatusForbidden {
			t.Errorf("purge from %s: status %d, want 403", name, resp.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".trash", keepFolder, "plugin.yaml")); err != nil {
		t.Fatal("a refused purge deleted the trash folder")
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/trash/meter-..-x/purge", fromConsole); resp.StatusCode != http.StatusNotFound {
		t.Errorf("purge of a name containing ..: status %d, want 404", resp.StatusCode)
	}
	if resp := postFrom(h, "http://127.0.0.1:4678/api/trash/no-such/restore", fromConsole); resp.StatusCode != http.StatusNotFound {
		t.Errorf("restore of a missing folder: status %d, want 404", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".trash", keepFolder, "plugin.yaml")); err != nil {
		t.Fatal("a refused purge deleted keep")
	}
	resp = postFrom(h, "http://127.0.0.1:4678/api/trash/"+keepFolder+"/purge", fromConsole)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("purge: %d %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".trash", keepFolder)); !os.IsNotExist(err) {
		t.Fatalf("purged folder is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "meter", "plugin.yaml")); err != nil {
		t.Fatal("purge removed a plugin that was not in the trash")
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "off", "plugin.yaml")); err != nil {
		t.Fatal("purge removed a plugin that was not in the trash")
	}
	if got, want := sidebar(), []string{"meter", "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sidebar after purge %v, want %v", got, want)
	}
	if gjson.Get(catalog(), "trash.#").Int() != 0 {
		t.Errorf("catalog still lists trash: %s", catalog())
	}
	portsSame()
}

// readFile reads path and closes it. A handle kept open until the test ends
// makes Windows refuse to rename a new copy over the file.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
