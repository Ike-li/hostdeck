package processview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/run"
)

func TestAProcessReadingListsEveryPIDWithoutOfferingActions(t *testing.T) {
	cpu, memory := 125.5, uint64(32<<20)
	reader := func(context.Context, string, run.Runner, bool) ([]proc, error) {
		return []proc{{PID: 42, PPID: 1, Name: "Worker <script>", User: "example", CPU: &cpu, Memory: &memory}, {PID: 43, PPID: 42, Name: "Worker <script>"}, {PID: os.Getpid(), Name: "hostdeck"}}, nil
	}
	out, err := execute(context.Background(), "", []string{"processes", "detail"}, nil, reader)
	if err != nil {
		t.Fatal(err)
	}
	var view detail.View
	if err := run.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 3 || view.Presentation != "compact" || len(view.Groups) != 2 {
		t.Fatalf("view %+v", view)
	}
	ids := map[string]bool{}
	for _, item := range view.Items {
		if ids[item.ID] || len(item.Actions) != 0 || item.Status != "" {
			t.Fatalf("unexpected item %+v", item)
		}
		ids[item.ID] = true
	}
	item := view.Items[0]
	if item.ID != "42" || item.Name.All != "Worker <script>" || item.Group != "other" {
		t.Fatalf("item %+v", item)
	}
	fields := map[string]detail.Value{}
	for _, field := range item.Fields {
		fields[field.Name.ByLang["en"]] = field.Value
	}
	if fields["PID"].String() != "42" || fields["Parent PID"].String() != "1" || fields["User"].String() != "example" || fields["CPU"].String() != "125.5%" || fields["Memory"].String() != "32.0 MiB" {
		t.Fatalf("fields %+v", fields)
	}
	for _, field := range view.Items[1].Fields {
		if field.Name.ByLang["en"] == "CPU" || field.Name.ByLang["en"] == "Memory" || field.Name.ByLang["en"] == "User" {
			if field.Value.Text.ByLang["en"] == "" || field.Value.Text.ByLang["zh-CN"] == "" {
				t.Fatalf("unread field %+v", field)
			}
		}
	}
	if view.Items[2].Group != "hostdeck" {
		t.Fatalf("self %+v", view.Items[2])
	}
}

func TestAProcessCardCountsTheSnapshotWithoutInventingHealth(t *testing.T) {
	out, err := execute(context.Background(), "", []string{"processes", "card"}, nil, func(_ context.Context, _ string, _ run.Runner, metrics bool) ([]proc, error) {
		if metrics {
			t.Error("card requested optional metrics")
		}
		return []proc{{PID: 1, Name: "init"}, {PID: 2, Name: "helper"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Value  string
		Label  map[string]string
		Status string
		Error  any
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Value != "2" || got.Status != "" || got.Error != nil || got.Label["zh-CN"] == "" {
		t.Fatalf("card %+v", got)
	}
}

func TestAnUnreadableProcessSnapshotReportsAnErrorInsteadOfZero(t *testing.T) {
	for _, op := range []string{"card", "detail"} {
		out, err := execute(context.Background(), "", []string{"processes", op}, nil, func(context.Context, string, run.Runner, bool) ([]proc, error) {
			return nil, errors.New("snapshot failed")
		})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Error map[string]string
			Value string
			Items []any
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		if got.Error["en"] == "" || got.Error["zh-CN"] == "" || got.Value != "" || len(got.Items) != 0 {
			t.Fatalf("reply %s", out)
		}
	}
}

func TestCancelledAndUnknownProcessReadsNeverStartASnapshot(t *testing.T) {
	reader := func(context.Context, string, run.Runner, bool) ([]proc, error) {
		t.Fatal("unexpected read")
		return nil, nil
	}
	for _, args := range [][]string{nil, {"processes"}, {"processes", "action", "stop", "42"}, {"processes", "detail", "extra"}, {"wrong", "card"}} {
		if _, err := execute(context.Background(), "", args, nil, reader); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := execute(ctx, "", []string{"processes", "detail"}, nil, reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestTheNativeProcessReaderIncludesThisTestProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the real process table")
	}
	out, err := Execute(context.Background(), t.TempDir(), []string{"processes", "detail"}, func(ctx context.Context, c run.Command) ([]byte, error) { return run.Output(ctx, c.Dir, c.Args) })
	if err != nil {
		t.Fatal(err)
	}
	var view detail.View
	if err := run.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	if view.Error != nil {
		t.Fatalf("read failed: %s", out)
	}
	for _, item := range view.Items {
		if item.ID == strconv.Itoa(os.Getpid()) && strings.TrimSpace(item.Name.All) != "" {
			return
		}
	}
	t.Fatal("current process is missing")
}

func TestProcessDetailsExplainTheExecutableParentAndLifetime(t *testing.T) {
	started := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	elapsed, cpuTime, threads := time.Duration(90061)*time.Second, time.Duration(3661)*time.Second, uint32(17)
	read := func(context.Context, string, run.Runner, bool) ([]proc, error) {
		return []proc{
			{PID: 42, PPID: 1, Name: "Worker", Path: "/Applications/My Worker.app/Contents/MacOS/Worker", StartedAt: started, Elapsed: &elapsed, CPUTime: &cpuTime, Threads: &threads},
			{PID: 1, Name: "launchd"},
			{PID: 43, PPID: 999, Name: "unreadable"},
		}, nil
	}
	out, err := execute(context.Background(), "", []string{"processes", "detail"}, nil, read)
	if err != nil {
		t.Fatal(err)
	}
	var v detail.View
	if err := run.Decode(out, &v); err != nil {
		t.Fatal(err)
	}
	var found detail.Item
	for _, it := range v.Items {
		if it.ID == "42" {
			found = it
		}
	}
	values := map[string]string{}
	for _, f := range found.Fields {
		values[f.Name.ByLang["en"]] = f.Value.String()
	}
	for name, want := range map[string]string{"Executable path": "/Applications/My Worker.app/Contents/MacOS/Worker", "Parent process": "launchd", "Started": "2026-10-29 08:00:00 UTC", "Uptime": "25:01:01", "CPU time": "01:01:01", "Threads": "17"} {
		if values[name] != want {
			t.Errorf("%s = %q, want %q", name, values[name], want)
		}
	}
	for _, it := range v.Items {
		if it.ID != "43" {
			continue
		}
		for _, f := range it.Fields {
			for _, name := range []string{"Executable path", "Parent process", "Started", "Uptime", "CPU time", "Threads"} {
				if f.Name.ByLang["en"] == name && (f.Value.ByLang["en"] == "" || f.Value.ByLang["zh-CN"] == "") {
					t.Errorf("missing bilingual fallback %+v", f)
				}
			}
		}
	}
}
