package notify_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/notify"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

var told = []string{"HOSTDECK_ID", "HOSTDECK_NAME", "HOSTDECK_NAME_ZH_CN", "HOSTDECK_FROM", "HOSTDECK_TO", "HOSTDECK_VALUE", "HOSTDECK_TIME"}

// TestMain lets the test binary stand in for a notification command.
func TestMain(m *testing.M) {
	if os.Getenv("HOSTDECK_FAKE_NOTIFY") == "" {
		os.Exit(m.Run())
	}
	switch args := os.Args[1:]; args[0] {
	case "record":
		f, err := os.OpenFile(args[1], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			os.Exit(1)
		}
		var vars []string
		for _, k := range told {
			vars = append(vars, k+"="+os.Getenv(k))
		}
		fmt.Fprintln(f, strings.Join(vars, " "))
		f.Close()
	case "names":
		var names []string
		for _, kv := range os.Environ() {
			if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "HOSTDECK_") && k != "HOSTDECK_FAKE_NOTIFY" {
				names = append(names, k)
			}
		}
		os.WriteFile(args[1], []byte(strings.Join(names, "\n")), 0o644)
	case "fail":
		fmt.Fprintln(os.Stderr, "no notifier here")
		os.Exit(3)
	}
}

// machine is a hostdeck folder holding one plugin whose card reads a service
// the test sets, a clock the test moves, and the commands that ran.
type machine struct {
	t     *testing.T
	dir   string
	runs  string
	url   string
	mu    sync.Mutex
	state string
	reads int
	at    time.Time
}

func newMachine(t *testing.T) *machine {
	t.Setenv("HOSTDECK_FAKE_NOTIFY", "1")
	// The console's own environment must not decide what the command is told.
	t.Setenv("HOSTDECK_FROM", "inherited")
	m := &machine{t: t, dir: t.TempDir(), state: "up", at: time.Date(2026, 10, 29, 13, 0, 0, 0, time.UTC)}
	m.runs = filepath.Join(m.dir, "runs.txt")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.reads++
		if m.state == "gone" {
			http.Error(w, "not answering", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, `{"state": %q}`, m.state)
	}))
	t.Cleanup(srv.Close)
	m.url = srv.URL
	m.card("")
	m.notifications([]string{"svc"}, []string{os.Args[0], "record", m.runs})
	return m
}

// card writes the service's plugin, read every interval; "" leaves it out.
func (m *machine) card(interval string) {
	if interval != "" {
		interval = "\n  interval: " + interval
	}
	m.write("plugins/svc/plugin.yaml", fmt.Sprintf(`schema: 1
name: {en: Service, zh-CN: 服务}
card:%s
  http:
    url: %s
  value: state
  warn: {equals: slow}
  error: {equals: down}
`, interval, m.url))
}

func (m *machine) write(name, content string) {
	m.t.Helper()
	file := filepath.Join(m.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		m.t.Fatal(err)
	}
}

func (m *machine) notifications(watch, command []string) {
	m.t.Helper()
	data, err := json.Marshal(map[string]any{"watch": watch, "command": command})
	if err != nil {
		m.t.Fatal(err)
	}
	m.write("notifications.json", string(data))
}

func (m *machine) set(state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
}

func (m *machine) now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.at
}

func (m *machine) wait(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.at = m.at.Add(d)
}

// watcher reads the plugins as the console does, through one card cache on
// the test's clock.
func (m *machine) watcher() *notify.Watcher {
	m.t.Helper()
	plugins, _, err := plugin.Load(filepath.Join(m.dir, "plugins"))
	if err != nil {
		m.t.Fatal(err)
	}
	cache := card.NewCache(m.now)
	read := func(ctx context.Context, id string) (card.View, time.Duration, bool) {
		for _, p := range plugins {
			if p.ID == id && p.Card != nil {
				return cache.Get(ctx, p), time.Duration(p.Card.Interval), true
			}
		}
		return card.View{}, 0, false
	}
	w := notify.New(m.dir, read, m.now)
	if err := w.Load(); err != nil {
		m.t.Fatal(err)
	}
	return w
}

// told returns one line per command run, of the HOSTDECK_ variables it got.
func (m *machine) told() []string {
	m.t.Helper()
	data, err := os.ReadFile(m.runs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		m.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestAWatchedCardSeenRedTwiceInARowRunsTheCommandWithWhatChanged(t *testing.T) {
	m := newMachine(t)
	m.set("down")
	w := m.watcher()

	w.Check(context.Background())
	if got := m.told(); got != nil {
		t.Fatalf("one reading ran the command: %q", got)
	}
	m.wait(time.Minute)
	w.Check(context.Background())

	want := "HOSTDECK_ID=svc HOSTDECK_NAME=Service HOSTDECK_NAME_ZH_CN=服务 HOSTDECK_FROM= HOSTDECK_TO=error HOSTDECK_VALUE=down HOSTDECK_TIME=2026-10-29T13:01:00Z"
	if got := m.told(); len(got) != 1 || got[0] != want {
		t.Fatalf("told %q, want one run with\n%s", got, want)
	}
}

func TestAWatchedCardFirstSeenFineIsNotToldButItsNextChangeIsToldFromThere(t *testing.T) {
	m := newMachine(t)
	w := m.watcher()
	for range 2 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); got != nil {
		t.Fatalf("a card that was fine from the start ran the command: %q", got)
	}

	m.set("down")
	for range 2 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 1 || !strings.Contains(got[0], "HOSTDECK_FROM=ok HOSTDECK_TO=error ") {
		t.Fatalf("told %q, want one run from ok to error", got)
	}
}

func TestACardThatFlapsBetweenReadingsIsNotTold(t *testing.T) {
	m := newMachine(t)
	w := m.watcher()
	for _, state := range []string{"up", "up", "down", "up", "down", "up", "slow", "up"} {
		m.set(state)
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); got != nil {
		t.Fatalf("a flapping card ran the command: %q", got)
	}
}

func TestARecoveryIsToldOnceTwoReadingsInARowSeeIt(t *testing.T) {
	m := newMachine(t)
	w := m.watcher()
	for _, state := range []string{"down", "down", "up"} {
		m.set(state)
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 1 {
		t.Fatalf("told %q before the recovery was seen twice, want only the change to error", got)
	}
	w.Check(context.Background())
	if got := m.told(); len(got) != 2 || !strings.Contains(got[1], "HOSTDECK_FROM=error HOSTDECK_TO=ok ") {
		t.Fatalf("told %q, want the recovery from error to ok second", got)
	}
}

func TestWhatWasToldIsNotToldAgainAfterARestart(t *testing.T) {
	m := newMachine(t)
	m.set("down")
	w := m.watcher()
	for range 2 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 1 {
		t.Fatalf("told %q before the restart, want one run", got)
	}

	restarted := m.watcher()
	for range 3 {
		restarted.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 1 {
		t.Fatalf("told %q, want the change told before the restart and nothing after it", got)
	}
	m.set("up")
	for range 2 {
		restarted.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 2 || !strings.Contains(got[1], "HOSTDECK_FROM=error HOSTDECK_TO=ok ") {
		t.Fatalf("told %q, want the recovery told from what was told before the restart", got)
	}
}

func TestACommandThatFailsIsRunAgainAtTheNextReading(t *testing.T) {
	m := newMachine(t)
	m.notifications([]string{"svc"}, []string{os.Args[0], "fail"})
	m.set("down")
	w := m.watcher()
	for range 2 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}

	m.notifications([]string{"svc"}, []string{os.Args[0], "record", m.runs})
	if err := w.Load(); err != nil {
		t.Fatal(err)
	}
	w.Check(context.Background())
	if got := m.told(); len(got) != 1 || !strings.Contains(got[0], "HOSTDECK_FROM= HOSTDECK_TO=error ") {
		t.Fatalf("told %q, want the change the failed command missed", got)
	}
}

func TestACardThatCannotBeReadIsToldApartFromOneThatReadsRed(t *testing.T) {
	m := newMachine(t)
	w := m.watcher()
	for _, state := range []string{"up", "up", "gone", "gone"} {
		m.set(state)
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 1 || !strings.Contains(got[0], "HOSTDECK_FROM=ok HOSTDECK_TO=unreadable HOSTDECK_VALUE= ") {
		t.Fatalf("told %q, want one run from ok to unreadable with no value", got)
	}
}

func TestAWatchedCardIsReadOnceEveryIntervalOfItsOwn(t *testing.T) {
	m := newMachine(t)
	m.card("5m")
	m.set("gone")
	w := m.watcher()
	for range 5 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	m.mu.Lock()
	reads := m.reads
	m.mu.Unlock()
	if reads != 1 {
		t.Fatalf("read %d times in the first five minutes, want once", reads)
	}
	w.Check(context.Background())
	m.mu.Lock()
	reads = m.reads
	m.mu.Unlock()
	if reads != 2 {
		t.Fatalf("read %d times by the fifth minute, want twice", reads)
	}
}

func TestABrokenNotificationsFileIsRefusedNamingTheProblemAndWhatWasLoadedKeepsWatching(t *testing.T) {
	m := newMachine(t)
	m.set("down")
	w := m.watcher()
	for _, c := range []struct{ file, problem string }{
		{`{"watch": ["svc"], "comand": ["notify"]}`, `unknown field "comand"`},
		{`{"watch": ["svc"]}`, "a command"},
		{`{"watch": ["svc"], "command": ["@hostdeck", "tasks"]}`, "@hostdeck"},
	} {
		m.write("notifications.json", c.file)
		if err := w.Load(); err == nil || !strings.Contains(err.Error(), "notifications.json") || !strings.Contains(err.Error(), c.problem) {
			t.Errorf("%s: loading said %v, want notifications.json and %q", c.file, err, c.problem)
		}
	}
	for range 2 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 1 {
		t.Fatalf("told %q, want the change told by the command loaded before", got)
	}
}

func TestAnUnreadableRecordOfWhatWasToldIsRefusedRatherThanToldAgain(t *testing.T) {
	m := newMachine(t)
	for _, record := range []string{`{"version": 1, "cards": `, `{"version": 2, "cards": {}}`} {
		m.write("data/notifications/sent.json", record)
		w := notify.New(m.dir, func(context.Context, string) (card.View, time.Duration, bool) { return card.View{}, 0, false }, m.now)
		if err := w.Load(); err == nil || !strings.Contains(err.Error(), "sent.json") {
			t.Errorf("%s: loading said %v, want sent.json named", record, err)
		}
	}
}

// The README is where the command's contract is written down.
func TestTheReadmesNameEveryVariableTheCommandGetsAndEveryKeyOfNotificationsJSON(t *testing.T) {
	m := newMachine(t)
	// The test's own HOSTDECK_ variables, such as HOSTDECK_E2E, are not the
	// command's to document.
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "HOSTDECK_") && k != "HOSTDECK_FAKE_NOTIFY" && k != "HOSTDECK_FROM" {
			os.Unsetenv(k)
			t.Cleanup(func() { os.Setenv(k, v) })
		}
	}
	got := filepath.Join(m.dir, "names.txt")
	m.notifications([]string{"svc"}, []string{os.Args[0], "names", got})
	m.set("down")
	w := m.watcher()
	for range 2 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Split(string(data), "\n")
	for f := range reflect.TypeFor[notify.Config]().Fields() {
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		names = append(names, key)
	}
	for _, readme := range []string{"README.md", "README.zh-CN.md"} {
		text, err := os.ReadFile(filepath.Join("..", "..", readme))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if !strings.Contains(string(text), "`"+name+"`") {
				t.Errorf("%s does not name `%s`", readme, name)
			}
		}
	}
}

func TestTheStatusSaysWhenEachWatchedCardWasCheckedAndWhatItWasLastToldAlsoAfterARestart(t *testing.T) {
	m := newMachine(t)
	m.set("down")
	w := m.watcher()
	if got := w.Status().Cards["svc"]; !got.Checked.IsZero() || got.Told != "" {
		t.Fatalf("before any reading the status said %+v", got)
	}
	w.Check(context.Background())
	m.wait(time.Minute)
	w.Check(context.Background())

	read := time.Date(2026, 10, 29, 13, 1, 0, 0, time.UTC)
	got := w.Status().Cards["svc"]
	if !got.Checked.Equal(read) || got.Told != "error" || !got.ToldAt.Equal(read) {
		t.Fatalf("status %+v, want checked and told error at %s", got, read)
	}
	got = m.watcher().Status().Cards["svc"]
	if got.Told != "error" || !got.ToldAt.Equal(read) {
		t.Fatalf("after a restart the status was %+v, want told error at %s", got, read)
	}
}

func TestTheStatusSaysWhenAndWhyTheCommandLastFailedUntilItSucceeds(t *testing.T) {
	m := newMachine(t)
	m.notifications([]string{"svc"}, []string{os.Args[0], "fail"})
	m.set("down")
	w := m.watcher()
	w.Check(context.Background())
	m.wait(time.Minute)
	w.Check(context.Background())

	failed := w.Status().Cards["svc"].Failed
	if failed == nil || !failed.At.Equal(time.Date(2026, 10, 29, 13, 1, 0, 0, time.UTC)) || !strings.Contains(failed.Detail, "no notifier here") {
		t.Fatalf("failure %+v, want one at 13:01 saying what the command wrote to stderr", failed)
	}

	m.notifications([]string{"svc"}, []string{os.Args[0], "record", m.runs})
	if err := w.Load(); err != nil {
		t.Fatal(err)
	}
	m.wait(time.Minute)
	w.Check(context.Background())
	if got := w.Status().Cards["svc"]; got.Failed != nil || got.Told != "error" {
		t.Fatalf("after the command succeeded the status was %+v, want it told and no failure", got)
	}
}

func TestTheStatusNamesWatchedCardsThatAreNotThereAndWhyNotificationsJSONWasRefused(t *testing.T) {
	m := newMachine(t)
	m.notifications([]string{"svc", "gone"}, []string{os.Args[0], "record", m.runs})
	w := m.watcher()
	w.Check(context.Background())
	if got := w.Status().Missing; !slices.Equal(got, []string{"gone"}) {
		t.Fatalf("missing %q, want [gone]", got)
	}

	m.write("notifications.json", `{"watch": ["svc"]}`)
	w.Load()
	if got := w.Status().Error; !strings.Contains(got, "notifications.json") || !strings.Contains(got, "a command") {
		t.Fatalf("error %q, want why notifications.json was refused", got)
	}
	m.notifications([]string{"svc"}, []string{os.Args[0], "record", m.runs})
	if err := w.Load(); err != nil {
		t.Fatal(err)
	}
	if got := w.Status().Error; got != "" {
		t.Fatalf("error %q after the file was put right, want none", got)
	}
}

func (m *machine) notificationsFile() notify.Config {
	m.t.Helper()
	data, err := os.ReadFile(filepath.Join(m.dir, "notifications.json"))
	if err != nil {
		m.t.Fatal(err)
	}
	var cfg notify.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		m.t.Fatal(err)
	}
	return cfg
}

func TestWatchingACardWritesOnlyTheWatchListAndTakesEffectAtOnce(t *testing.T) {
	m := newMachine(t)
	command := []string{os.Args[0], "record", m.runs}
	written, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	m.write("notifications.json", fmt.Sprintf(`{"watch": [], "command": %s, "windows": ["notify.exe"]}`, written))
	m.set("down")
	w := m.watcher()

	if err := w.Watch("svc", true); err != nil {
		t.Fatal(err)
	}
	if got := m.notificationsFile(); !slices.Equal(got.Watch, []string{"svc"}) || !slices.Equal(got.Command, command) || !slices.Equal(got.Windows, []string{"notify.exe"}) {
		t.Fatalf("notifications.json became %+v, want svc watched and the commands kept", got)
	}
	for range 2 {
		w.Check(context.Background())
		m.wait(time.Minute)
	}
	if got := m.told(); len(got) != 1 {
		t.Fatalf("told %q, want the watched card's change told without a reload", got)
	}

	if err := w.Watch("svc", false); err != nil {
		t.Fatal(err)
	}
	if got := m.notificationsFile(); len(got.Watch) != 0 || !slices.Equal(got.Command, command) {
		t.Fatalf("notifications.json became %+v, want nothing watched and the command kept", got)
	}
	if _, ok := w.Status().Cards["svc"]; ok {
		t.Fatal("the status still has the card no longer watched")
	}
}

func TestWatchingIsRefusedWithoutACommandAndNeverOverwritesAFileThatCannotBeRead(t *testing.T) {
	m := newMachine(t)
	file := filepath.Join(m.dir, "notifications.json")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	w := m.watcher()
	if err := w.Watch("svc", true); !errors.Is(err, notify.ErrNoCommand) {
		t.Fatalf("watching with no notifications.json said %v, want ErrNoCommand", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("watching created notifications.json: %v", err)
	}

	for _, broken := range []string{`{"watch": []}`, `{"watch": ["svc"], "comand": ["notify"]}`} {
		m.write("notifications.json", broken)
		err := w.Watch("svc", true)
		if broken == `{"watch": []}` && !errors.Is(err, notify.ErrNoCommand) {
			t.Errorf("%s: watching said %v, want ErrNoCommand", broken, err)
		}
		if err == nil {
			t.Errorf("%s: watching was not refused", broken)
		}
		if got, _ := os.ReadFile(file); string(got) != broken {
			t.Errorf("%s: watching rewrote the file as %s", broken, got)
		}
	}
}
