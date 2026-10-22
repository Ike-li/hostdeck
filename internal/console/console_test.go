package console

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

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
	h := Handler(nil, "plugins")

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
	h := Handler(nil, "plugins")
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
	csp := get(Handler(nil, "plugins"), "http://127.0.0.1:4678/", nil).Header.Get("Content-Security-Policy")
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

	resp := get(Handler(plugins, "/somewhere/plugins"), "http://127.0.0.1:4678/api/cards", nil)

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
