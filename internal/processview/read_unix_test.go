//go:build unix

package processview

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/run"
)

func TestTheUnixReaderPreservesSpacedNamesAndReportsCPUAndResidentMemory(t *testing.T) {
	t.Setenv("TZ", "Asia/Shanghai")
	out, err := Execute(context.Background(), t.TempDir(), []string{"processes", "detail"}, func(_ context.Context, c run.Command) ([]byte, error) {
		if c.Args[0] == "/usr/bin/top" {
			return []byte("Processes: 2 total\nPID #TH\n42 17/1\n43 2\n"), nil
		}
		if !reflect.DeepEqual(c.Args, []string{"/bin/ps", "-axo", "pid=,ppid=,user=,%cpu=,rss=,lstart=,etime=,time=,comm="}) {
			t.Fatalf("command %v", c.Args)
		}
		found := false
		timezones := 0
		for _, e := range c.Env {
			if strings.HasPrefix(e, "TZ=") {
				timezones++
				if e != "TZ=UTC" {
					t.Fatalf("date output timezone %s", e)
				}
			}
			if e == "LC_ALL=C" {
				found = true
			}
		}
		if timezones != 1 {
			t.Fatalf("timezone count %d", timezones)
		}
		if !found {
			t.Fatal("numeric output locale is not pinned")
		}
		return []byte(" 42 1 example 125.5 32768 Thu Oct 29 08:00:00 2026 1-01:01:01 61:01.99 /Applications/My  Worker.app/Contents/MacOS/My  Worker\n 43 42 root 0.0 0 Thu Oct 29 08:00:00 2026 00:00 0:00.00 /usr/bin/other\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var view detail.View
	if err := run.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	if view.Error != nil || len(view.Items) != 2 || view.Items[0].Name.All != "My  Worker" {
		t.Fatalf("reply %s", out)
	}
	if view.Items[0].Fields[3].Value.String() != "125.5%" || view.Items[0].Fields[4].Value.String() != "32.0 MiB" {
		t.Fatalf("fields %+v", view.Items[0].Fields)
	}
	fields := map[string]string{}
	for _, f := range view.Items[0].Fields {
		fields[f.Name.ByLang["en"]] = f.Value.String()
	}
	for name, want := range map[string]string{"Executable path": "/Applications/My  Worker.app/Contents/MacOS/My  Worker", "Started": "2026-10-29 08:00:00 UTC", "Uptime": "25:01:01", "CPU time": "01:01:01", "Threads": "17"} {
		if fields[name] != want {
			t.Errorf("%s=%q, want %q", name, fields[name], want)
		}
	}

}

func TestAnInvalidOrFailedUnixProcessTableNeverBecomesAPartialList(t *testing.T) {
	for _, body := range []string{"", "42 1 user 0.0 20 /bin/good\nbroken\n", "42 1 user NaN 20 /bin/bad\n", "42 1 user -1 20 /bin/bad\n", "42 1 user 0.0 -1 /bin/bad\n", "42 1 user 0.0 18446744073709551615 /bin/bad\n", "42 1 user 0.0 20 /bin/good\n42 1 user 0.0 20 /bin/duplicate\n"} {
		out, err := Execute(context.Background(), "", []string{"processes", "card"}, func(context.Context, run.Command) ([]byte, error) { return []byte(body), nil })
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Error map[string]string
			Value string
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		if got.Error["en"] == "" || got.Value != "" {
			t.Fatalf("accepted %q: %s", body, out)
		}
	}
	out, err := Execute(context.Background(), "", []string{"processes", "card"}, func(context.Context, run.Command) ([]byte, error) {
		return []byte("42 1 user 0.0 20 /bin/good\n"), &run.ExitError{Code: 1}
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Error map[string]string }
	json.Unmarshal(out, &got)
	if got.Error["en"] == "" {
		t.Fatalf("accepted failed command: %s", out)
	}
}

func TestOptionalThreadFailuresKeepTheProcessSnapshot(t *testing.T) {
	for _, body := range []string{"", "PID #TH\n42 invalid\n", "PID #TH\n42 2\n42 5\n", "PID #TH\n42 9999999999999999\n", "PID #TH\n42 2\nbroken\n"} {
		for _, failed := range []bool{false, true} {
			out, err := Execute(context.Background(), "", []string{"processes", "detail"}, func(ctx context.Context, c run.Command) ([]byte, error) {
				if c.Args[0] == "/bin/ps" {
					return []byte("42 1 user 1.0 1024 Thu Oct 29 08:00:00 2026 01:00 0:30.00 /bin/worker\n"), nil
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("optional thread query has no deadline")
				}
				if failed {
					return nil, errors.New("top refused")
				}
				return []byte(body), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var view detail.View
			if err := run.Decode(out, &view); err != nil {
				t.Fatal(err)
			}
			if len(view.Items) != 1 {
				t.Fatalf("lost process: %s", out)
			}
			for _, f := range view.Items[0].Fields {
				if f.Name.ByLang["en"] == "Threads" && f.Value.ByLang["zh-CN"] == "" {
					t.Fatalf("invented threads from %q: %+v", body, f)
				}
			}
		}
	}
}

func TestInvalidOptionalTimesDoNotRemoveTheProcess(t *testing.T) {
	out, err := Execute(context.Background(), "", []string{"processes", "detail"}, func(_ context.Context, c run.Command) ([]byte, error) {
		if c.Args[0] == "/bin/ps" {
			return []byte("42 1 user 1.0 1024 Bad Oct 29 08:00:00 2026 99:99 0:1m /bin/worker\n"), nil
		}
		return nil, errors.New("unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	var view detail.View
	if err := run.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("lost process: %s", out)
	}
	for _, f := range view.Items[0].Fields {
		for _, name := range []string{"Started", "Uptime", "CPU time"} {
			if f.Name.ByLang["en"] == name && f.Value.ByLang["zh-CN"] == "" {
				t.Fatalf("invented %s: %+v", name, f)
			}
		}
	}
}

func TestTheProcessOverviewReadsOnlyOnePSCountSnapshot(t *testing.T) {
	calls := 0
	out, err := Execute(context.Background(), "", []string{"processes", "card"}, func(_ context.Context, c run.Command) ([]byte, error) {
		calls++
		if !reflect.DeepEqual(c.Args, []string{"/bin/ps", "-axo", "pid=,ppid=,user=,%cpu=,rss=,comm="}) {
			t.Fatalf("overview requested enrichment %v", c.Args)
		}
		return []byte("42 1 user 1.0 1024 /bin/worker\n"), nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("%d calls, err %v", calls, err)
	}
	var card struct{ Value string }
	if err := json.Unmarshal(out, &card); err != nil || card.Value != "1" {
		t.Fatalf("count %s: %v", out, err)
	}
}
