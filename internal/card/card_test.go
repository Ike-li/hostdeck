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
