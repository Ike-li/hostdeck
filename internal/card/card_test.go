package card

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

// With HOSTDECK_FAKE_CARD set, the test binary plays a card's command:
// "print <text>", "fail <code> <stderr>" or "hang".
func TestMain(m *testing.M) {
	if os.Getenv("HOSTDECK_FAKE_CARD") == "" {
		os.Exit(m.Run())
	}
	switch args := os.Args[1:]; args[0] {
	case "print":
		fmt.Print(args[1])
	case "fail":
		fmt.Fprintln(os.Stderr, args[2])
		code, _ := strconv.Atoi(args[1])
		os.Exit(code)
	case "hang":
		time.Sleep(time.Hour)
	}
	os.Exit(0)
}

// execCard is a card whose command is the test binary with args.
func execCard(t *testing.T, args ...string) plugin.Plugin {
	t.Setenv("HOSTDECK_FAKE_CARD", "1")
	return plugin.Plugin{
		ID:   "disk",
		Dir:  t.TempDir(),
		Name: plugin.Text{All: "Disk"},
		Card: &plugin.Card{Exec: &plugin.Exec{Command: append([]string{os.Args[0]}, args...)}},
	}
}

func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func declarative(url, path string) plugin.Plugin {
	return plugin.Plugin{
		ID:   "demo",
		Name: plugin.Text{All: "Demo"},
		Icon: "D",
		Card: &plugin.Card{
			HTTP:  &plugin.HTTP{URL: url},
			Value: path,
			Label: plugin.Text{ByLang: map[string]string{"en": "Database", "zh-CN": "数据库"}},
		},
	}
}

func TestADeclarativeCardShowsTheValueAtItsPath(t *testing.T) {
	url := serve(t, 200, `{"db": {"state": "ok"}, "version": "1.2.3"}`)
	p := declarative(url, "db.state")

	got := Fetch(context.Background(), p)

	want := View{ID: "demo", Name: p.Name, Icon: "D", Value: "ok", Label: p.Card.Label}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAFailedFetchBecomesAnErrorCardSayingWhy(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	for _, tc := range []struct {
		name, url, path, mention string
	}{
		{"unreachable", closed.URL, "status", strings.TrimPrefix(closed.URL, "http://")},
		{"error status", serve(t, 500, `{"status": "ok"}`), "status", "500"},
		{"not JSON", serve(t, 200, `<html>`), "status", "not JSON"},
		{"nothing at the path", serve(t, 200, `{"state": "ok"}`), "status", `"status"`},
	} {
		got := Fetch(context.Background(), declarative(tc.url, tc.path))
		if got.Error == nil || got.Error.Code != "fetch_failed" || !strings.Contains(got.Error.Detail, tc.mention) {
			t.Errorf("%s: error = %+v, want fetch_failed mentioning %s", tc.name, got.Error, tc.mention)
		}
		if got.Value != "" {
			t.Errorf("%s: value = %q, want none", tc.name, got.Value)
		}
	}
}

func TestAnExecCardShowsWhatItsCommandPrints(t *testing.T) {
	// Windows PowerShell can start its output with a byte order mark.
	printed := "\ufeff" + `{"status": "warn", "value": 42, "label": {"en": "GB free", "zh-CN": "GB 可用"},
		"rows": [{"name": {"en": "Used", "zh-CN": "已用"}, "value": "81%", "status": "warn"}, {"name": "Total", "value": 512}]}`
	p := execCard(t, "print", printed)

	got := Fetch(context.Background(), p)

	want := View{
		ID: "disk", Name: p.Name, Status: "warn", Value: "42",
		Label: plugin.Text{ByLang: map[string]string{"en": "GB free", "zh-CN": "GB 可用"}},
		Rows: []Row{
			{Name: plugin.Text{ByLang: map[string]string{"en": "Used", "zh-CN": "已用"}}, Value: "81%", Status: "warn"},
			{Name: plugin.Text{All: "Total"}, Value: "512"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheWindowsCommandRunsOnWindowsOnly(t *testing.T) {
	p := execCard(t, "print", `{"value": "elsewhere"}`)
	p.Card.Exec.Windows = []string{os.Args[0], "print", `{"value": "windows"}`}

	want := "elsewhere"
	if runtime.GOOS == "windows" {
		want = "windows"
	}
	if got := Fetch(context.Background(), p); got.Value != want {
		t.Errorf("value %q, want %q", got.Value, want)
	}
}

func TestACommandMayReportItsOwnError(t *testing.T) {
	got := Fetch(context.Background(), execCard(t, "print", `{"error": {"en": "no disk", "zh-CN": "没有磁盘"}}`))
	want := &Error{Code: "reported", Text: plugin.Text{ByLang: map[string]string{"en": "no disk", "zh-CN": "没有磁盘"}}}
	if !reflect.DeepEqual(got.Error, want) {
		t.Errorf("error %+v, want %+v", got.Error, want)
	}
}

func TestAFailedCommandBecomesAnErrorCardSayingWhy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	missing := execCard(t)
	missing.Card.Exec.Command = []string{missing.Dir + "/no-such-program"}
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		p       plugin.Plugin
		code    string
		mention string
	}{
		{"exits with an error", context.Background(), execCard(t, "fail", "3", "disk not mounted"), "command_exit", "exit status 3: disk not mounted"},
		{"hangs", ctx, execCard(t, "hang"), "command_timeout", ""},
		{"cannot start", context.Background(), missing, "command_start", "no-such-program"},
		{"prints no JSON", context.Background(), execCard(t, "print", "42 GB"), "command_output", ""},
		{"prints an unknown field", context.Background(), execCard(t, "print", `{"valeu": "1"}`), "command_output", `"valeu"`},
		{"prints an unknown status", context.Background(), execCard(t, "print", `{"status": "purple"}`), "command_output", "purple"},
	} {
		got := Fetch(tc.ctx, tc.p)
		if got.Error == nil || got.Error.Code != tc.code || !strings.Contains(got.Error.Detail, tc.mention) {
			t.Errorf("%s: error %+v, want %s mentioning %q", tc.name, got.Error, tc.code, tc.mention)
		}
	}
}

func num(f float64) *float64             { return &f }
func val(s plugin.Scalar) *plugin.Scalar { return &s }

func TestThresholdsColourTheCardAndItsRows(t *testing.T) {
	url := serve(t, 200, `{"cpu": 85, "database": "down", "replicas": 2}`)
	p := declarative(url, "cpu")
	p.Card.Warn = &plugin.Threshold{Above: num(80)}
	p.Card.Error = &plugin.Threshold{Above: num(95)}
	p.Card.Rows = []plugin.Row{
		{Name: plugin.Text{All: "Database"}, Value: "database", Error: &plugin.Threshold{Not: val("ok")}},
		{Name: plugin.Text{All: "Replicas"}, Value: "replicas", Error: &plugin.Threshold{Equals: val("0")}},
	}

	got := Fetch(context.Background(), p)

	if got.Status != "warn" || got.Value != "85" {
		t.Errorf("card %q %q, want warn 85", got.Status, got.Value)
	}
	want := []Row{
		{Name: plugin.Text{All: "Database"}, Value: "down", Status: "error"},
		{Name: plugin.Text{All: "Replicas"}, Value: "2", Status: "ok"},
	}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows %+v, want %+v", got.Rows, want)
	}
}

func TestTheErrorThresholdWinsOverWarn(t *testing.T) {
	p := declarative(serve(t, 200, `{"free": 3}`), "free")
	p.Card.Warn = &plugin.Threshold{Below: num(20)}
	p.Card.Error = &plugin.Threshold{Below: num(5)}
	if got := Fetch(context.Background(), p); got.Status != "error" {
		t.Errorf("status %q, want error", got.Status)
	}
}

func TestANumberThresholdOnAWordIsAnError(t *testing.T) {
	p := declarative(serve(t, 200, `{"state": "busy"}`), "state")
	p.Card.Warn = &plugin.Threshold{Above: num(1)}
	got := Fetch(context.Background(), p)
	if got.Error == nil || got.Error.Code != "threshold" || !strings.Contains(got.Error.Detail, "busy") {
		t.Errorf("error %+v, want threshold mentioning busy", got.Error)
	}
}

func TestSecretsReachTheServiceButNoErrorMessage(t *testing.T) {
	t.Setenv("HOSTDECK_TEST_TOKEN", "s3cret")
	var gotHeader, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader, gotKey = r.Header.Get("Authorization"), r.URL.Query().Get("key")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p := declarative(srv.URL+"/?key=${env:HOSTDECK_TEST_TOKEN}", "v")
	p.Card.HTTP.Headers = map[string]string{"Authorization": "Bearer ${env:HOSTDECK_TEST_TOKEN}"}

	got := Fetch(context.Background(), p)

	if gotHeader != "Bearer s3cret" || gotKey != "s3cret" {
		t.Errorf("the service got %q and %q, want the token filled in", gotHeader, gotKey)
	}
	if got.Error == nil || strings.Contains(got.Error.Detail, "s3cret") || !strings.Contains(got.Error.Detail, "${env:HOSTDECK_TEST_TOKEN}") {
		t.Errorf("error %+v, want it to name the reference and not the token", got.Error)
	}

	srv.Close()
	got = Fetch(context.Background(), p)
	if got.Error == nil || strings.Contains(got.Error.Detail, "s3cret") {
		t.Errorf("unreachable: error %+v, want it without the token", got.Error)
	}
}

// A value that breaks the URL makes the parser quote the whole URL, filled in.
func TestASecretThatBreaksTheURLStaysOutOfTheError(t *testing.T) {
	t.Setenv("HOSTDECK_TEST_TOKEN", "bad\x7ftoken")
	got := Fetch(context.Background(), declarative("http://127.0.0.1:1/?key=${env:HOSTDECK_TEST_TOKEN}", "v"))
	if got.Error == nil || strings.Contains(got.Error.Detail, "token") {
		t.Errorf("error %+v, want one without the token", got.Error)
	}
}

// counting answers {"v": n}, n counting the requests, after gate lets it.
func counting(t *testing.T, gate <-chan struct{}) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if gate != nil {
			<-gate
		}
		fmt.Fprintf(w, `{"v": %d}`, n)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

func TestACardIsAskedAgainOnlyAfterItsInterval(t *testing.T) {
	url, hits := counting(t, nil)
	p := declarative(url, "v")
	p.Card.Interval = plugin.Duration(time.Minute)
	now := time.Now()
	cache := NewCache(func() time.Time { return now })

	if v := cache.Get(context.Background(), p); v.Value != "1" {
		t.Fatalf("first: %+v", v)
	}
	now = now.Add(59 * time.Second)
	if v := cache.Get(context.Background(), p); v.Value != "1" || hits.Load() != 1 {
		t.Errorf("within the interval: value %q after %d requests, want the kept 1", v.Value, hits.Load())
	}
	now = now.Add(2 * time.Second)
	if v := cache.Get(context.Background(), p); v.Value != "2" {
		t.Errorf("after the interval: value %q, want a new 2", v.Value)
	}
}

// A service that was down shows its value as soon as it is up again.
func TestAFailedCardIsNotKept(t *testing.T) {
	var up atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"v": "up"}`)
	}))
	defer srv.Close()
	cache := NewCache(time.Now)
	p := declarative(srv.URL, "v")
	p.Card.Interval = plugin.DefaultInterval

	if v := cache.Get(context.Background(), p); v.Error == nil {
		t.Fatalf("while down: %+v", v)
	}
	up.Store(true)
	if v := cache.Get(context.Background(), p); v.Value != "up" {
		t.Errorf("once up: %+v, want its value at once", v)
	}
}

func TestACardSlowerThanTheWaitIsPendingAndArrivesLater(t *testing.T) {
	gate := make(chan struct{})
	url, _ := counting(t, gate)
	p := declarative(url, "v")
	cache := NewCache(time.Now)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if v := cache.Get(ctx, p); !v.Pending || v.Value != "" || v.Error != nil || v.ID != "demo" {
		t.Fatalf("while slow: %+v, want a pending card without a value", v)
	}
	close(gate)
	deadline := time.Now().Add(2 * time.Second)
	for {
		v := cache.Get(context.Background(), p)
		if v.Value == "1" && !v.Pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the slow card never arrived: %+v", v)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAPendingCardKeepsShowingItsLastValue(t *testing.T) {
	var slow atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			time.Sleep(time.Second)
		}
		io.WriteString(w, `{"v": "old"}`)
	}))
	defer srv.Close()
	p := declarative(srv.URL, "v")
	now := time.Now()
	cache := NewCache(func() time.Time { return now })
	cache.Get(context.Background(), p)

	now = now.Add(2 * time.Duration(plugin.DefaultInterval))
	slow.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if v := cache.Get(ctx, p); !v.Pending || v.Value != "old" {
		t.Errorf("got %+v, want the last value, pending", v)
	}
}
