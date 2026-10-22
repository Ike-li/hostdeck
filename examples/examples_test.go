// The disk example runs on the machine at hand; the services are answered with
// responses copied from their documentation, or from their source where the
// documentation shows none.
package examples

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

const apiKey = "key-from-the-syncthing-settings"

func load(t *testing.T, id string) plugin.Plugin {
	t.Helper()
	t.Setenv("SYNCTHING_API_KEY", apiKey)
	plugins, broken, err := plugin.Load(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range broken {
		t.Errorf("example %s does not load: %v", b.ID, b.Err)
	}
	i := slices.IndexFunc(plugins, func(p plugin.Plugin) bool { return p.ID == id })
	if i < 0 {
		t.Fatalf("no example %s", id)
	}
	return plugins[i]
}

func TestTheDiskExampleShowsTheFreeSpaceOfThisMachine(t *testing.T) {
	// Generous: Windows PowerShell took 6.4 s to start for the first time on
	// an idle CI runner, and longer while other test packages ran.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	v := card.Fetch(ctx, load(t, "disk"))

	if v.Error != nil {
		t.Fatalf("error card: %+v", v.Error)
	}
	if !strings.HasSuffix(v.Value, " GB") || !slices.Contains([]string{"ok", "warn", "error"}, v.Status) {
		t.Errorf("value %q, status %q; want a size in GB and a status", v.Value, v.Status)
	}
	// Chinese text survives the trip through the command's output, which on
	// Windows depends on the script choosing UTF-8.
	if zh := v.Label.ByLang["zh-CN"]; !strings.HasPrefix(zh, "可用") {
		t.Errorf("Chinese label %q, want it to start with 可用", zh)
	}
	if len(v.Rows) != 1 || v.Rows[0].Name.ByLang["zh-CN"] != "已用" || !strings.HasSuffix(v.Rows[0].Value, "%") {
		t.Errorf("rows %+v, want one row saying how much is used", v.Rows)
	}
}

// The open action itself is not run here: it would open a window on the
// desktop of whoever runs the tests.
func TestTheDiskExampleListsTheHomeFoldersDiskWithAnOpenAction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	v := detail.Read(ctx, load(t, "disk"))

	if v.Error != nil {
		t.Fatalf("error: %+v", v.Error)
	}
	i := slices.IndexFunc(v.Items, func(it detail.Item) bool { return strings.Contains(it.Name.ByLang["zh-CN"], "主目录") })
	if i < 0 {
		t.Fatalf("items %+v, want the home folder's disk, named in Chinese", v.Items)
	}
	home := v.Items[i]
	if !slices.ContainsFunc(home.Fields, func(f detail.Field) bool {
		return f.Name.ByLang["zh-CN"] == "已用" && strings.HasSuffix(string(f.Value), "%")
	}) {
		t.Errorf("fields %+v, want how much is used", home.Fields)
	}
	if len(home.Actions) != 1 || home.Actions[0].ID != "open" {
		t.Errorf("actions %+v, want open", home.Actions)
	}
}

// fetchAnswered reads p's card from a local server that answers with status
// and body in place of the service, and returns the request it got.
func fetchAnswered(t *testing.T, p plugin.Plugin, status int, body string) (card.View, *http.Request) {
	t.Helper()
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(p.Card.HTTP.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = strings.TrimPrefix(srv.URL, "http://")
	c, h := *p.Card, *p.Card.HTTP
	h.URL = u.String()
	c.HTTP = &h
	p.Card = &c
	v := card.Fetch(context.Background(), p)
	if got == nil {
		t.Fatalf("the card asked nothing; view %+v", v)
	}
	return v, got
}

// https://grafana.com/docs/grafana/latest/developers/http_api/other/#health-api
const grafanaHealthy = `{
  "commit": "087143285",
  "database": "ok",
  "version": "5.1.3"
}`

// Grafana's apiHealthHandler, in pkg/api/http_server.go: the version is left
// out when hide_version is set, and a failing database answers 503.
const grafanaFailing = `{
  "database": "failing"
}`

func TestTheGrafanaExampleShowsWhetherItsDatabaseIsHealthy(t *testing.T) {
	p := load(t, "grafana")
	if p.Page == nil || p.Page.Proxy == "" {
		t.Errorf("page %+v, want Grafana embedded in the sidebar", p.Page)
	}

	v, r := fetchAnswered(t, p, http.StatusOK, grafanaHealthy)
	if r.URL.Path != "/api/health" {
		t.Errorf("asked %s, want /api/health", r.URL)
	}
	if v.Error != nil || v.Value != "ok" || v.Status != "ok" {
		t.Errorf("healthy: %+v, want ok and a green card", v)
	}

	v, _ = fetchAnswered(t, p, http.StatusServiceUnavailable, grafanaFailing)
	if v.Error == nil {
		t.Errorf("failing database: %+v, want an error card", v)
	}
}

// https://docs.syncthing.net/rest/db-completion-get.html
const syncthingSyncing = `{
  "completion": 99.9937565835,
  "globalBytes": 156793013575,
  "needBytes": 9789241,
  "globalItems": 7823,
  "needItems": 412,
  "needDeletes": 0,
  "remoteState": "valid",
  "sequence": 12
}`

// The same answer once nothing is left to do.
const syncthingInSync = `{
  "completion": 100,
  "globalBytes": 156793013575,
  "needBytes": 0,
  "globalItems": 7823,
  "needItems": 0,
  "needDeletes": 0,
  "remoteState": "valid",
  "sequence": 12
}`

func TestTheSyncthingExampleSendsItsAPIKeyAndCountsWhatIsLeftToSync(t *testing.T) {
	p := load(t, "syncthing")
	if p.Page == nil || p.Page.Proxy == "" {
		t.Errorf("page %+v, want Syncthing embedded in the sidebar", p.Page)
	}

	v, r := fetchAnswered(t, p, http.StatusOK, syncthingSyncing)
	if r.URL.Path != "/rest/db/completion" || r.URL.RawQuery != "" {
		t.Errorf("asked %s, want /rest/db/completion for all folders", r.URL)
	}
	if k := r.Header.Get("X-API-Key"); k != apiKey {
		t.Errorf("X-API-Key %q, want the key from SYNCTHING_API_KEY", k)
	}
	if v.Error != nil || v.Value != "412" || v.Status != "warn" {
		t.Errorf("syncing: %+v, want 412 items and a yellow card", v)
	}
	if len(v.Rows) == 0 {
		t.Errorf("syncing: no rows")
	}

	v, _ = fetchAnswered(t, p, http.StatusOK, syncthingInSync)
	if v.Error != nil || v.Value != "0" || v.Status != "ok" {
		t.Errorf("in sync: %+v, want 0 items and a green card", v)
	}
}

// https://github.com/ollama/ollama/blob/main/docs/api.md#list-running-models
const ollamaOneModel = `{
  "models": [
    {
      "name": "mistral:latest",
      "model": "mistral:latest",
      "size": 5137025024,
      "digest": "2ae6f6dd7a3dd734790bbbf58b8909a606e0e7e97e94b7604e0aa7ae4490e6d8",
      "details": {
        "parent_model": "",
        "format": "gguf",
        "family": "llama",
        "families": ["llama"],
        "parameter_size": "7.2B",
        "quantization_level": "Q4_0"
      },
      "expires_at": "2024-06-04T14:38:31.83753-07:00",
      "size_vram": 5137025024
    }
  ]
}`

// Ollama's PsHandler, in server/routes.go, starts from an empty list, so no
// model loaded is [] and not null.
const ollamaNoModel = `{"models":[]}`

func TestTheOllamaExampleCountsTheModelsInMemory(t *testing.T) {
	p := load(t, "ollama")

	v, r := fetchAnswered(t, p, http.StatusOK, ollamaOneModel)
	if r.URL.Path != "/api/ps" {
		t.Errorf("asked %s, want /api/ps", r.URL)
	}
	if v.Error != nil || v.Value != "1" {
		t.Errorf("one model: %+v, want 1", v)
	}

	v, _ = fetchAnswered(t, p, http.StatusOK, ollamaNoModel)
	if v.Error != nil || v.Value != "0" {
		t.Errorf("no model: %+v, want 0 and no error", v)
	}
}

// The plugins page lists what is built in, so every example says what it does
// in both languages, and nothing but the examples themselves is built in.
func TestEveryBuiltInExampleDescribesItself(t *testing.T) {
	entries, err := catalog.List(Files, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.ID)
		if e.Name.IsZero() || e.Description.ByLang["en"] == "" || e.Description.ByLang["zh-CN"] == "" {
			t.Errorf("%s: name %+v, description %+v; want a name and a description in en and zh-CN", e.ID, e.Name, e.Description)
		}
	}
	if want := []string{"disk", "grafana", "ollama", "syncthing"}; !slices.Equal(ids, want) {
		t.Errorf("built in: %v, want %v", ids, want)
	}
	fs.WalkDir(Files, ".", func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".go") {
			t.Errorf("%s is built in", path)
		}
		return err
	})
}
