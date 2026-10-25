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
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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
	if len(v.Rows) == 0 || v.Rows[0].Name.IsZero() || !strings.HasSuffix(v.Rows[0].Value, "%") {
		t.Errorf("rows %+v, want the home folder's disk first, saying how much is used", v.Rows)
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

// dfOnAMac is what df -Pkl printed on a Mac with an internal disk holding
// Macintosh HD and PublishTmp, and an external one holding Storage and a Time
// Machine volume, whose name ends in a space, with its backups mounted.
const dfOnAMac = `Filesystem                                                  1024-blocks      Used Available Capacity  Mounted on
/dev/disk3s3s1                                                482797652  13335040 204695268     7%    /
/dev/disk3s6                                                  482797652   7340184 204695268     4%    /System/Volumes/VM
/dev/disk3s4                                                  482797652  10700332 204695268     5%    /System/Volumes/Preboot
/dev/disk1s2                                                     512000      6164    491832     2%    /System/Volumes/xarts
/dev/disk3s1                                                  482797652 245039024 204695268    55%    /System/Volumes/Data
/dev/disk3s7                                                   19531252      1040  19530212     1%    /Volumes/PublishTmp
/dev/disk7s1                                                  976557744 198252844 275636692    42%    /Volumes/Storage
/dev/disk7s2                                                  634760868 502465776 132092660    80%    /Volumes/time machine 
/dev/disk3s5                                                  482797652   1503124 204695268     1%    /Volumes/Recovery
com.apple.TimeMachine.2026-10-12-000623.backup@/dev/disk7s2   634760868 618332712 132092660    83%    /Volumes/.timemachine/8E158BD0-47E1-4487-B153-E19EDF580519/2026-10-12-000623.backup
com.apple.TimeMachine.2026-10-22-092516.local@/dev/disk3s1    482797652 237735448 204695268    54%    /Volumes/com.apple.TimeMachine.localsnapshots/Backups.backupdb/Mac/2026-10-22-092516/Macintosh HD - Data
`

// onAMac puts a df that prints dfOnAMac, and a diskutil that names the data
// volume, before the real ones.
func onAMac(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Windows scripts ask PowerShell, not df")
	}
	bin := t.TempDir()
	df := "#!/bin/sh\n" +
		"case \"$1\" in -Pkl) cat <<'EOF'\n" + dfOnAMac + "EOF\n" +
		";; *) printf '%s\\n' \"$(head -n 1 <<'EOF'\n" + dfOnAMac + "EOF\n)\" '/dev/disk3s1 482797652 245039024 204695268 55% /System/Volumes/Data' ;; esac\n"
	diskutil := "#!/bin/sh\n" +
		"[ \"$2\" = /System/Volumes/Data ] && echo '   Volume Name:               Macintosh HD - Data'\n"
	for name, script := range map[string]string{"df": df, "diskutil": diskutil} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestTheDiskCardHasARowForEachVolume(t *testing.T) {
	onAMac(t)

	v := card.Fetch(context.Background(), load(t, "disk"))

	if v.Error != nil {
		t.Fatalf("error card: %+v", v.Error)
	}
	var got []string
	for _, r := range v.Rows {
		got = append(got, r.Name.All+" "+r.Value+" "+r.Status)
	}
	want := []string{
		"Macintosh HD 195 GB · 55% ok",
		"PublishTmp 19 GB · 1% ok",
		"Storage 263 GB · 42% ok",
		"time machine 126 GB · 80% warn",
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows\n%q\nwant\n%q", got, want)
	}
	if v.Value != "195 GB" || v.Status != "warn" {
		t.Errorf("value %q, status %q; want the home folder's free space and the worst volume's status", v.Value, v.Status)
	}
}

func TestTheDiskDetailListsVolumesAndNotBackupsOrRecovery(t *testing.T) {
	onAMac(t)

	v := detail.Read(context.Background(), load(t, "disk"))

	if v.Error != nil {
		t.Fatalf("error: %+v", v.Error)
	}
	var got []string
	for _, it := range v.Items {
		got = append(got, it.ID)
	}
	want := []string{"/System/Volumes/Data", "/Volumes/PublishTmp", "/Volumes/Storage", "/Volumes/time machine"}
	if !slices.Equal(got, want) {
		t.Errorf("items %q, want %q", got, want)
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
	if want := []string{"disk", "grafana", "jobs", "ollama", "syncthing"}; !slices.Equal(ids, want) {
		t.Errorf("built in: %v, want %v", ids, want)
	}
	fs.WalkDir(Files, ".", func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".go") {
			t.Errorf("%s is built in", path)
		}
		return err
	})
}

// withLaunchctl puts a fake launchctl on PATH. The real one would read this
// machine's jobs, and the test says which jobs the card is about.
func withLaunchctl(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Windows script asks PowerShell for services, not launchctl")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "launchctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestTheJobsCardCountsOnlyAJobThatExitedWithAnError(t *testing.T) {
	withLaunchctl(t, `#!/bin/sh
uid=$(id -u)
case "$2" in
"gui/$uid")
cat <<'EOF'
	services = {
		123      - 	com.example.running
		0        0 	com.example.idle
		0        - 	com.example.once
		0        1 	com.example.failed
		1        0 	com.apple.Finder
		2        - 	application.com.example.App
	}
EOF
;;
"gui/$uid/com.example.running")
printf '%s\n' '	state = running' '	last exit code = 255' ;;
"gui/$uid/com.example.idle")
printf '%s\n' '	state = not running' '	last exit code = 0' ;;
"gui/$uid/com.example.once")
printf '%s\n' '	state = not running' ;;
"gui/$uid/com.example.failed")
printf '%s\n' '	state = not running' '	last exit code = 1' ;;
*) echo "unexpected: $*" >&2; exit 2 ;;
esac
`)

	v := card.Fetch(context.Background(), load(t, "jobs"))

	if v.Error != nil {
		t.Fatalf("error card: %+v", v.Error)
	}
	if v.Status != "error" || v.Value != "1" {
		t.Errorf("status %q, value %q; want one job that needs attention", v.Status, v.Value)
	}
	if v.Label.ByLang["en"] != "need attention" || v.Label.ByLang["zh-CN"] != "需要处理" {
		t.Errorf("label %+v", v.Label)
	}
	if len(v.Rows) != 1 || v.Rows[0].Name.All != "com.example.failed" || v.Rows[0].Status != "error" || v.Rows[0].Value != "not running · exit 1" {
		t.Errorf("rows %+v, want only the job that exited 1", v.Rows)
	}
}

func TestTheJobsCardCountsAJobStuckInAnotherState(t *testing.T) {
	withLaunchctl(t, `#!/bin/sh
uid=$(id -u)
case "$2" in
"gui/$uid")
printf '%s\n' '	services = {' '		0        0 	com.example.stuck' '	}' ;;
"gui/$uid/com.example.stuck")
printf '%s\n' '	state = spawn scheduled' '	last exit code = 0' ;;
*) echo "unexpected: $*" >&2; exit 2 ;;
esac
`)

	v := card.Fetch(context.Background(), load(t, "jobs"))

	if v.Error != nil {
		t.Fatalf("error card: %+v", v.Error)
	}
	if v.Status != "error" || v.Value != "1" || len(v.Rows) != 1 || v.Rows[0].Value != "spawn scheduled" {
		t.Errorf("status %q, value %q, rows %+v; want the stuck job even though its last exit was 0", v.Status, v.Value, v.Rows)
	}
}

func TestTheJobsCardReadsTheUserDomainWhenTheGuiDomainIsMissing(t *testing.T) {
	withLaunchctl(t, `#!/bin/sh
uid=$(id -u)
case "$2" in
gui/*) exit 1 ;;
"user/$uid")
printf '%s\n' '	services = {' '		0        2 	com.example.failed' '	}' ;;
"user/$uid/com.example.failed")
printf '%s\n' '	state = not running' '	last exit code = 2' ;;
*) echo "unexpected: $*" >&2; exit 2 ;;
esac
`)

	v := card.Fetch(context.Background(), load(t, "jobs"))

	if v.Error != nil {
		t.Fatalf("error card: %+v", v.Error)
	}
	if v.Status != "error" || v.Value != "1" || len(v.Rows) != 1 || v.Rows[0].Name.All != "com.example.failed" || v.Rows[0].Value != "not running · exit 2" {
		t.Errorf("status %q, value %q, rows %+v; want the user-domain job", v.Status, v.Value, v.Rows)
	}
}

func TestTheJobsCardSaysWhenNeitherDomainCanBeRead(t *testing.T) {
	withLaunchctl(t, "#!/bin/sh\nexit 1\n")

	v := card.Fetch(context.Background(), load(t, "jobs"))

	if v.Error == nil || v.Error.Code != "reported" {
		t.Fatalf("error %+v, want the card to say the jobs could not be listed", v.Error)
	}
	if v.Error.Text.ByLang["en"] != "Could not list launchd jobs for this user." || v.Error.Text.ByLang["zh-CN"] != "读不到当前用户的 launchd 任务。" {
		t.Errorf("text %+v", v.Error.Text)
	}
}

func TestTheJobsExampleReadsThisMachinesJobs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	v := card.Fetch(ctx, load(t, "jobs"))

	if v.Error != nil {
		t.Fatalf("error card: %+v", v.Error)
	}
	if !slices.Contains([]string{"ok", "warn", "error"}, v.Status) {
		t.Errorf("status %q, want ok, warn or error", v.Status)
	}
	n, err := strconv.Atoi(v.Value)
	if err != nil || n < 0 {
		t.Errorf("value %q, want how many jobs need attention", v.Value)
	}
	if v.Label.ByLang["zh-CN"] != "需要处理" {
		t.Errorf("label %+v", v.Label)
	}
}
