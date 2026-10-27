// The disk example runs on the machine at hand; the services are answered with
// responses copied from their documentation, or from their source where the
// documentation shows none.
package examples

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
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
	"github.com/Ike-li/hostdeck/internal/diskhistory"
	"github.com/Ike-li/hostdeck/internal/dockerview"
	"github.com/Ike-li/hostdeck/internal/manualview"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func init() {
	run.Register("tasks", tasks.Execute)
	run.Register("disk-history", diskhistory.Execute)
	run.Register("docker", dockerview.Execute)
	run.Register("manual", manualview.Execute)
}

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
	if testing.Short() {
		t.Skip("reads the real machine")
	}
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
	if len(v.Rows) == 0 || v.Rows[0].Name.IsZero() || !strings.HasSuffix(v.Rows[0].Value.All, "%") {
		t.Errorf("rows %+v, want the home folder's disk first, saying how much is used", v.Rows)
	}
}

// The open action itself is not run here: it would open a window on the
// desktop of whoever runs the tests.
func TestTheDiskExampleListsTheHomeFoldersDiskWithAnOpenAction(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the real machine")
	}
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
	if home.Label.ByLang["zh-CN"] != "可用" || home.Label.ByLang["en"] != "free" || !strings.HasSuffix(home.Value.String(), " GB") {
		t.Errorf("value %q label %+v, want the free space in GB, named free and 可用", home.Value, home.Label)
	}
	if !slices.ContainsFunc(home.Fields, func(f detail.Field) bool {
		return f.Name.ByLang["zh-CN"] == "已用" && strings.Contains(f.Value.String(), "GB") && strings.HasSuffix(f.Value.String(), "%")
	}) {
		t.Errorf("fields %+v, want how many GB are used, and the percent", home.Fields)
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
		got = append(got, r.Name.All+" "+r.Value.All+" "+r.Status)
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

// The detail page's large figure is free space. Without a name beside it, and
// with Used printed only as a percent, the two cannot be told apart.
func TestTheDiskDetailNamesTheFreeSpaceAndTheGigabytesUsed(t *testing.T) {
	onAMac(t)

	v := detail.Read(context.Background(), load(t, "disk"))

	if v.Error != nil {
		t.Fatalf("error: %+v", v.Error)
	}
	want := []struct{ id, free, used string }{
		{"/System/Volumes/Data", "195 GB", "234 GB · 55%"},
		{"/Volumes/PublishTmp", "19 GB", "0 GB · 1%"},
		{"/Volumes/Storage", "263 GB", "189 GB · 42%"},
		{"/Volumes/time machine", "126 GB", "479 GB · 80%"},
	}
	if len(v.Items) != len(want) {
		t.Fatalf("%d items, want %d", len(v.Items), len(want))
	}
	for i, it := range v.Items {
		w := want[i]
		if it.ID != w.id || it.Value.String() != w.free || it.Label.ByLang["en"] != "free" || it.Label.ByLang["zh-CN"] != "可用" {
			t.Errorf("item %d: id %q value %q label %+v, want %s free %s named free/可用", i, it.ID, it.Value, it.Label, w.id, w.free)
		}
		var used string
		for _, f := range it.Fields {
			if f.Name.ByLang["zh-CN"] == "已用" {
				used = f.Value.String()
			}
		}
		if used != w.used {
			t.Errorf("%s used %q, want %s", it.ID, used, w.used)
		}
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
	if want := []string{"disk", "disk-history", "docker", "grafana", "jobs", "manual", "ollama", "scheduled-tasks", "syncthing"}; !slices.Equal(ids, want) {
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
	if len(v.Rows) != 1 || v.Rows[0].Name.All != "com.example.failed" || v.Rows[0].Status != "error" || v.Rows[0].Value.All != "not running · exit 1" {
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
	if v.Status != "error" || v.Value != "1" || len(v.Rows) != 1 || v.Rows[0].Value.All != "spawn scheduled" {
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
	if v.Status != "error" || v.Value != "1" || len(v.Rows) != 1 || v.Rows[0].Name.All != "com.example.failed" || v.Rows[0].Value.All != "not running · exit 2" {
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
	if testing.Short() {
		t.Skip("reads the real machine")
	}
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

func TestTheJobsDetailListsLoadedJobsAndTheActionEachOneOffers(t *testing.T) {
	withLaunchctl(t, `#!/bin/sh
uid=$(id -u)
case "$2" in
"gui/$uid")
cat <<'EOF'
	services = {
		10       - 	com.example.running
		0        0 	com.example.idle
		0        1 	com.example.failed
		0        0 	com.example/evil
		1        0 	com.apple.Finder
		2        - 	application.com.example.App
	}
EOF
;;
"gui/$uid/com.example.running")
printf '%s\n' '	state = running' '	last exit code = 255' '	program = /usr/bin/ssh' '	runs = 4' '	stdout path = /tmp/running.log' ;;
"gui/$uid/com.example.idle")
printf '%s\n' '	state = not running' '	last exit code = 0' '	program = /bin/true' '	runs = 1' ;;
"gui/$uid/com.example.failed")
printf '%s\n' '	state = not running' '	last exit code = 1' '	program = /bin/false' ;;
"gui/$uid/com.example/evil")
printf '%s\n' '	state = not running' '	last exit code = 0' '	program = /bin/echo' ;;
*) echo "unexpected: $*" >&2; exit 2 ;;
esac
`)

	v := detail.Read(context.Background(), load(t, "jobs"))

	if v.Error != nil {
		t.Fatalf("error: %+v", v.Error)
	}
	var ids []string
	for _, it := range v.Items {
		ids = append(ids, it.ID)
	}
	want := []string{"com.example.running", "com.example.idle", "com.example.failed", "com.example/evil"}
	if !slices.Equal(ids, want) {
		t.Fatalf("items %q, want %q", ids, want)
	}
	running, idle, failed, evil := v.Items[0], v.Items[1], v.Items[2], v.Items[3]
	if running.Group != "running" || idle.Group != "idle" || failed.Group != "attention" {
		t.Fatalf("groups: %+v", v.Items)
	}
	if running.Status != "ok" || running.Value.String() != "Running" || len(running.Actions) != 1 || running.Actions[0].ID != "restart" || running.Actions[0].Label.ByLang["zh-CN"] != "重启" {
		t.Errorf("running %+v, want a restart", running)
	}
	if idle.Status != "ok" || idle.Value.String() != "Not running" || len(idle.Actions) != 1 || idle.Actions[0].ID != "start" || idle.Actions[0].Label.ByLang["en"] != "Run" {
		t.Errorf("idle %+v, want a run", idle)
	}
	if failed.Status != "error" || failed.Value.String() != "not running · exit 1" || len(failed.Actions) != 1 || failed.Actions[0].ID != "start" {
		t.Errorf("failed %+v, want it started", failed)
	}
	if len(evil.Actions) != 0 {
		t.Errorf("actions %+v, want none for a name that could point at another job", evil.Actions)
	}
	if !slices.ContainsFunc(running.Fields, func(f detail.Field) bool {
		return f.Name.ByLang["zh-CN"] == "程序" && f.Value.String() == "/usr/bin/ssh"
	}) || !slices.ContainsFunc(running.Fields, func(f detail.Field) bool {
		return f.Name.ByLang["zh-CN"] == "上次退出" && f.Value.String() == "255"
	}) || !slices.ContainsFunc(running.Fields, func(f detail.Field) bool {
		return f.Name.ByLang["zh-CN"] == "运行次数" && f.Value.String() == "4"
	}) || !slices.ContainsFunc(running.Fields, func(f detail.Field) bool {
		return f.Name.ByLang["zh-CN"] == "标准输出" && f.Value.String() == "/tmp/running.log"
	}) {
		t.Errorf("fields %+v, want the program, last exit, runs and stdout path", running.Fields)
	}
}

func jobsCalls(t *testing.T) string {
	t.Helper()
	record := filepath.Join(t.TempDir(), "calls")
	withLaunchctl(t, fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
uid=$(id -u)
case "$1" in
kickstart)
	if [ "${JOBS_KICK_FAIL:-}" = 1 ]; then echo "Operation not permitted" >&2; exit 1; fi
	exit 0 ;;
esac
case "$2" in
"gui/$uid")
	printf '%%s\n' '	services = {' '		10       - 	com.example.running' '		0        0 	com.example.idle' '	}' ;;
"gui/$uid/com.example.running")
	printf '%%s\n' '	state = running' '	last exit code = 0' '	program = /usr/bin/ssh' ;;
"gui/$uid/com.example.idle")
	printf '%%s\n' '	state = not running' '	last exit code = 0' '	program = /bin/true' ;;
*) echo "unexpected: $*" >&2; exit 2 ;;
esac
`, record))
	return record
}

func TestTheJobsRestartKicksTheRunningJob(t *testing.T) {
	record := jobsCalls(t)

	msg, err := detail.Act(context.Background(), load(t, "jobs"), "com.example.running", "restart")

	if err != nil || msg.ByLang["en"] != "Restarted. Running." || msg.ByLang["zh-CN"] != "已重启。正在运行。" {
		t.Fatalf("message %+v, error %+v; want the job's state after the restart", msg, err)
	}
	got, readErr := os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	uid := strconv.Itoa(os.Getuid())
	want := "kickstart -k gui/" + uid + "/com.example.running\n"
	if !strings.Contains(string(got), want) {
		t.Errorf("launchctl calls:\n%s\nwant a line %q", got, want)
	}
	if strings.Contains(string(got), "kickstart gui/") {
		t.Errorf("launchctl calls:\n%s\nrestart must pass -k", got)
	}
}

func TestTheJobsStartRunsAnIdleJobWithoutKillingIt(t *testing.T) {
	record := jobsCalls(t)

	msg, err := detail.Act(context.Background(), load(t, "jobs"), "com.example.idle", "start")

	if err != nil || msg.ByLang["en"] != "Ran. Not running." || msg.ByLang["zh-CN"] != "已运行。未在运行。" {
		t.Fatalf("message %+v, error %+v; want the job's state after it was run", msg, err)
	}
	got, readErr := os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	uid := strconv.Itoa(os.Getuid())
	if !strings.Contains(string(got), "kickstart gui/"+uid+"/com.example.idle\n") || strings.Contains(string(got), "kickstart -k") {
		t.Errorf("launchctl calls:\n%s\nwant kickstart without -k", got)
	}
}

func TestTheJobsActionSaysWhenTheJobCannotBeReadAgain(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "printed")
	withLaunchctl(t, fmt.Sprintf(`#!/bin/sh
uid=$(id -u)
case "$1" in
kickstart) exit 0 ;;
esac
case "$2" in
"gui/$uid")
	printf '%%s\n' '	services = {' '		10       - 	com.example.running' '	}' ;;
"gui/$uid/com.example.running")
	if [ -f %q ]; then exit 1; fi
	touch %q
	printf '%%s\n' '	state = running' ;;
*) echo "unexpected: $*" >&2; exit 2 ;;
esac
`, seen, seen))

	msg, err := detail.Act(context.Background(), load(t, "jobs"), "com.example.running", "restart")

	if err != nil || msg.ByLang["en"] != "Restarted, but the job could not be read again." || msg.ByLang["zh-CN"] != "已重启，但没能再读到这条任务。" {
		t.Fatalf("message %+v, error %+v", msg, err)
	}
}

func TestTheJobsActionReportsTheLaunchctlError(t *testing.T) {
	t.Setenv("JOBS_KICK_FAIL", "1")
	jobsCalls(t)

	_, err := detail.Act(context.Background(), load(t, "jobs"), "com.example.running", "restart")

	if err == nil || err.Code != "reported" || !strings.Contains(err.Text.ByLang["en"], "Operation not permitted") || !strings.Contains(err.Text.ByLang["zh-CN"], "Operation not permitted") {
		t.Fatalf("error %+v, want the launchctl line in both languages", err)
	}
}

func TestTheJobsActionRefusesANameThatCouldPointSomewhereElse(t *testing.T) {
	record := filepath.Join(t.TempDir(), "calls")
	withLaunchctl(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexit 1\n", record))

	out, err := run.Output(context.Background(), load(t, "jobs").Dir, append(load(t, "jobs").Actions.Exec.Args(), "restart", "com.example/evil"))

	if err != nil {
		t.Fatalf("output %q, error %v; want the refusal on stdout", out, err)
	}
	if !strings.Contains(string(out), "cannot be passed to launchctl") || !strings.Contains(string(out), "不能传给 launchctl") {
		t.Errorf("output %s", out)
	}
	if _, statErr := os.Stat(record); !os.IsNotExist(statErr) {
		got, _ := os.ReadFile(record)
		t.Errorf("launchctl was called:\n%s", got)
	}
}

func TestTheJobsActionRejectsAnUnknownActionBeforeCallingLaunchctl(t *testing.T) {
	record := filepath.Join(t.TempDir(), "calls")
	withLaunchctl(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexit 1\n", record))

	_, err := run.Output(context.Background(), load(t, "jobs").Dir, append(load(t, "jobs").Actions.Exec.Args(), "nope", "com.example.running"))

	var exit *run.ExitError
	if !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("error %v, want exit 2", err)
	}
	if _, statErr := os.Stat(record); !os.IsNotExist(statErr) {
		got, _ := os.ReadFile(record)
		t.Errorf("launchctl was called:\n%s", got)
	}
}

func TestTheJobsExampleListsThisMachinesJobs(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the real machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	v := detail.Read(ctx, load(t, "jobs"))

	if v.Error != nil {
		t.Fatalf("error: %+v", v.Error)
	}
	seen := map[string]bool{}
	for _, it := range v.Items {
		if it.ID == "" || seen[it.ID] || !slices.Contains([]string{"ok", "warn", "error"}, it.Status) {
			t.Errorf("item %+v, want a unique id and a status", it)
		}
		seen[it.ID] = true
		if strings.HasPrefix(it.ID, "com.apple.") || strings.HasPrefix(it.ID, "application.") {
			t.Errorf("item %s, want Apple's agents and ordinary applications left out", it.ID)
		}
		for _, a := range it.Actions {
			if a.ID != "start" && a.ID != "restart" {
				t.Errorf("item %s action %s, want start or restart", it.ID, a.ID)
			}
		}
	}
}

func TestTheJobsRestartOnWindowsStopsThenStartsTheService(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the macOS script calls launchctl")
	}
	record := filepath.Join(t.TempDir(), "calls")
	bin := t.TempDir()
	src := filepath.Join(t.TempDir(), "sc.go")
	if err := os.WriteFile(src, []byte(windowsSC), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(bin, "sc.exe"), src)
	cmd.Env = append(os.Environ(), "GOOS=windows")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build sc.exe: %v\n%s", err, out)
	}
	t.Setenv("JOBS_SC_RECORD", record)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := load(t, "jobs")

	runAction := func(action, name string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return run.Output(ctx, p.Dir, append(p.Actions.Exec.Args(), action, name))
	}
	out, err := runAction("restart", "ExampleService")
	if err != nil || !strings.Contains(string(out), "Restarted") || !strings.Contains(string(out), "已重启") {
		t.Fatalf("output %q, error %v; want the service's state after the restart", out, err)
	}
	got, readErr := os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "stop ExampleService\nquery ExampleService\nstart ExampleService\nquery ExampleService\n" {
		t.Errorf("sc.exe calls:\n%s", got)
	}

	if err := os.WriteFile(record, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runAction("start", "ExampleService")
	if err != nil || !strings.Contains(string(out), "Ran") || !strings.Contains(string(out), "已运行") {
		t.Fatalf("start output %q, error %v", out, err)
	}
	got, readErr = os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "start ExampleService\nquery ExampleService\n" {
		t.Errorf("sc.exe calls:\n%s", got)
	}
	for _, action := range []string{"start", "restart"} {
		t.Run(action+" failure", func(t *testing.T) {
			t.Setenv("JOBS_SC_FAIL", "1")
			out, err := runAction(action, "ExampleService")
			if err != nil || !strings.Contains(string(out), "Access is denied") {
				t.Fatalf("output %s, error %v", out, err)
			}
			var result struct {
				Error plugin.Text `json:"error"`
			}
			if err := run.Decode(out, &result); err != nil || result.Error.ByLang["en"] == "" || result.Error.ByLang["zh-CN"] == "" {
				t.Fatalf("reported error %+v: %v", result, err)
			}
		})
	}
	for _, tc := range []struct{ action, name string }{{"start", "unsafe/name"}, {"start", "valid\n"}, {"delete", "ExampleService"}} {
		if err := os.WriteFile(record, nil, 0600); err != nil {
			t.Fatal(err)
		}
		out, err := runAction(tc.action, tc.name)
		if tc.action == "start" && (err != nil || !strings.Contains(string(out), "cannot be passed")) {
			t.Fatalf("refusal %s: %v", out, err)
		}
		if tc.action == "delete" && err == nil {
			t.Fatal("unknown action succeeded")
		}
		if got, err := os.ReadFile(record); err != nil || len(got) != 0 {
			t.Fatalf("refused action called sc.exe: %s %v", got, err)
		}
	}
	// Advance only the OS clock boundary so the shipped 15-second timeout
	// branch is exercised without waiting 15 seconds on every runner.
	scriptPath, err := filepath.Abs(filepath.Join("jobs", "action.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "timeout.ps1")
	body := "$script:clock = [datetime]'2026-01-01T00:00:00Z'\nfunction Get-Date { $script:clock = $script:clock.AddSeconds(20); return $script:clock }\n. '" + strings.ReplaceAll(scriptPath, "'", "''") + "' @args\n"
	if err := os.WriteFile(wrapper, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	p.Actions.Exec.Windows = []string{"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", wrapper}
	t.Setenv("JOBS_SC_PENDING", "1")
	out, err = runAction("restart", "ExampleService")
	if err != nil || !strings.Contains(string(out), "did not stop within 15 seconds") {
		t.Fatalf("timeout output %s, error %v", out, err)
	}
}

// windowsSC is a stand-in for sc.exe. It records its arguments and answers a
// query with STOPPED so a restart does not wait.
const windowsSC = `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	f, err := os.OpenFile(os.Getenv("JOBS_SC_RECORD"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		os.Exit(3)
	}
	fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
	f.Close()
	if os.Getenv("JOBS_SC_FAIL") == "1" {
		fmt.Println("Access is denied")
		os.Exit(5)
	}
	if len(os.Args) > 1 && os.Args[1] == "query" {
		if os.Getenv("JOBS_SC_PENDING") == "1" { fmt.Println("        STATE              : 4  RUNNING") } else { fmt.Println("        STATE              : 1  STOPPED") }
	}
}
`
