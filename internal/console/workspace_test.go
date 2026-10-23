package console

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/ports"
)

func TestASidebarMonitorReadsOnlyItsOwnCardAndSharesTheOverviewCache(t *testing.T) {
	var hits atomic.Int32
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `{"models":[{},{}]}`)
	}))
	defer svc.Close()
	h := Handler(Config{Plugins: []plugin.Plugin{
		{ID: "ollama", Name: plugin.Text{All: "Ollama"}, Card: &plugin.Card{Interval: plugin.Duration(time.Minute), HTTP: &plugin.HTTP{URL: svc.URL}, Value: "models.#"}},
		{ID: "other", Name: plugin.Text{All: "Other"}, Card: &plugin.Card{Interval: plugin.Duration(time.Minute), HTTP: &plugin.HTTP{URL: svc.URL}, Value: "models.#"}},
		{ID: "page", Name: plugin.Text{All: "Page"}, Page: &plugin.Page{Link: "https://example.test/"}},
	}})
	read := func() string { return string(mustRead(t, get(h, "http://127.0.0.1:4678/api/cards/ollama", nil).Body)) }
	if body := read(); !strings.Contains(body, `"value":"2"`) || hits.Load() != 1 {
		t.Fatalf("monitor %s, hits %d; want only Ollama's reading", body, hits.Load())
	}
	read()
	if hits.Load() != 1 {
		t.Fatalf("monitor ignored cache: %d reads", hits.Load())
	}
	get(h, "http://127.0.0.1:4678/api/cards", nil).Body.Close()
	if hits.Load() != 2 {
		t.Fatalf("overview did not share monitor cache: %d reads", hits.Load())
	}
	for _, id := range []string{"missing", "page"} {
		resp := get(h, "http://127.0.0.1:4678/api/cards/"+id, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: HTTP %d, want 404", id, resp.StatusCode)
		}
	}
}

func TestConnectionsWithoutMonitoringKeepTheirDestinationsAndErrors(t *testing.T) {
	h := Handler(Config{Plugins: []plugin.Plugin{
		{ID: "page", Name: plugin.Text{All: "Page"}, Page: &plugin.Page{Proxy: "http://127.0.0.1:3000/"}},
		{ID: "detail", Name: plugin.Text{All: "Detail"}, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"unused"}}}},
		{ID: "stuck", Name: plugin.Text{All: "Stuck"}, Page: &plugin.Page{Proxy: "http://127.0.0.1:3001/"}},
	}, Served: map[string]Served{"stuck": {Err: &ports.InUse{ID: "stuck", Port: 4680}}}})
	resp := get(h, "http://127.0.0.1:4678/api/cards", nil)
	want := parseJSON(`{"pluginDir":"","cards":[
		{"id":"page","name":"Page","destination":"proxy","address":"http://127.0.0.1:3000/"},
		{"id":"detail","name":"Detail","destination":"detail"},
		{"id":"stuck","name":"Stuck","destination":"proxy","address":"http://127.0.0.1:3001/","error":{"code":"port_unavailable","detail":"4680"}}
	]}`)
	if got := decodeJSON(t, resp); !reflect.DeepEqual(got, want) {
		t.Errorf("shortcuts: got %v, want %v", got, want)
	}
}
