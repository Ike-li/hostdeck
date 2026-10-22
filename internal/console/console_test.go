package console

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
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
	var want any
	json.Unmarshal([]byte(`{
		"pluginDir": "/somewhere/plugins",
		"cards": [{"id": "demo", "name": "Demo", "icon": "D", "value": "ok",
		           "label": {"en": "Database", "zh-CN": "数据库"}}]
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
	}
	served := map[string]Served{
		"grafana": {URL: "http://127.0.0.1:4679/grafana/"},
		"stuck":   {Err: errors.New("port 4680, which stuck keeps, is in use by another program")},
	}

	resp := get(Handler(Config{Plugins: plugins, Served: served}), "http://127.0.0.1:4678/api/plugins", nil)

	want := parseJSON(`[
		{"id": "grafana", "name": "Grafana", "icon": "G", "type": "proxy", "url": "http://127.0.0.1:4679/grafana/"},
		{"id": "stuck", "name": "Stuck", "type": "proxy",
		 "error": {"code": "port_unavailable", "detail": "port 4680, which stuck keeps, is in use by another program"}},
		{"id": "wiki", "name": {"en": "Wiki", "zh-CN": "维基"}, "type": "link", "url": "https://wiki.example/"}
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
	h := Handler(Config{Plugins: plugins, Served: served, Tokens: tokens})

	var got struct{ URL string }
	if err := json.NewDecoder(post(h, "http://127.0.0.1:4678/api/clear/grafana").Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	prefix := "http://127.0.0.1:4679" + proxy.ClearPath + "?token="
	token, ok := strings.CutPrefix(got.URL, prefix)
	if !ok || !tokens.Redeem("grafana", token) {
		t.Errorf("clearing URL %q, want %s and a token for grafana", got.URL, prefix)
	}

	for _, id := range []string{"stuck", "wiki", "nothing"} {
		if resp := post(h, "http://127.0.0.1:4678/api/clear/"+id); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", id, resp.StatusCode)
		}
	}
}
