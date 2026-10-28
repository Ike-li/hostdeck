package listenerview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ike-li/hostdeck/internal/observation"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeOp struct {
	sockets      []Socket
	procs        map[int]Proc
	self         int
	signals      []string
	dieOnTerm    bool
	surviveKill  bool
	slept        time.Duration
	nextSockets  []Socket
	nextProcs    map[int]Proc
	snapN        int
	snapshotFail error
}

func (f *fakeOp) Snapshot(ctx context.Context) ([]Socket, map[int]Proc, error) {
	if f.snapshotFail != nil {
		return nil, nil, f.snapshotFail
	}
	f.snapN++
	if f.snapN > 1 && f.nextProcs != nil {
		return f.nextSockets, f.nextProcs, nil
	}
	return f.sockets, f.procs, nil
}

func (f *fakeOp) Self() int { return f.self }

func (f *fakeOp) Term(pid int) error {
	f.signals = append(f.signals, fmt.Sprintf("term %d", pid))
	return nil
}

func (f *fakeOp) Kill(pid int) error {
	f.signals = append(f.signals, fmt.Sprintf("kill %d", pid))
	return nil
}

func (f *fakeOp) Alive(pid int) bool {
	if f.dieOnTerm {
		for _, s := range f.signals {
			if s == fmt.Sprintf("term %d", pid) {
				return false
			}
		}
	}
	if !f.surviveKill {
		for _, s := range f.signals {
			if s == fmt.Sprintf("kill %d", pid) {
				return false
			}
		}
	}
	return true
}

func (f *fakeOp) Sleep(d time.Duration) { f.slept += d }

func testDirWithPorts(t *testing.T, portsJSON string) string {
	t.Helper()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "listeners")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if portsJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "ports.json"), []byte(portsJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return pluginDir
}

func TestUnknownListenersActionIsAnError(t *testing.T) {
	op := &fakeOp{}
	_, err := act(context.Background(), op, t.TempDir(), "restart", "42")
	if err == nil || !errors.Is(err, errUnknownAction) {
		t.Fatalf("err %v, want unknown action", err)
	}
}

func TestAnUnknownOrReusedProcessIsLeftAlone(t *testing.T) {
	started := time.Unix(100, 0)
	baseSockets := []Socket{{PID: 4, Port: 8080, Addr: "127.0.0.1", Command: "app"}}
	baseProcs := map[int]Proc{
		4: {PPID: 1, Name: "app", Started: started},
	}
	op := &fakeOp{sockets: baseSockets, procs: baseProcs, self: 50}
	dir := testDirWithPorts(t, "")

	for _, tc := range []struct {
		name        string
		item        string
		swap        map[int]Proc
		swapSockets []Socket
	}{
		{name: "unknown", item: "99"},
		{name: "not a number", item: "invalid"},
		{name: "pid one", item: "1"},
		{
			name:        "changed",
			item:        "4",
			swap:        map[int]Proc{4: {PPID: 1, Name: "app", Started: started.Add(time.Second)}},
			swapSockets: baseSockets,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op.signals = nil
			op.snapN = 0
			op.nextProcs = tc.swap
			op.nextSockets = tc.swapSockets
			out, err := act(context.Background(), op, dir, "stop", tc.item)
			if err != nil || len(op.signals) != 0 {
				t.Fatalf("out %s err %v signals %v", out, err, op.signals)
			}
			var got struct{ Error text }
			if err := json.Unmarshal(out, &got); err != nil || got.Error["en"] == "" || got.Error["zh-CN"] == "" {
				t.Fatalf("out %s", out)
			}
		})
	}
}

func TestTheConsoleAndItsParentAreLeftRunning(t *testing.T) {
	started := time.Unix(100, 0)
	sockets := []Socket{
		{PID: 4, Port: 9, Addr: "127.0.0.1", Command: "zsh"},
		{PID: 50, Port: 4678, Addr: "127.0.0.1", Command: "hostdeck"},
	}
	procs := map[int]Proc{
		2:  {PPID: 1, Name: "login"},
		3:  {PPID: 2, Name: "bash"},
		4:  {PPID: 3, Name: "zsh", Started: started},
		50: {PPID: 4, Name: "hostdeck", Started: started},
	}
	op := &fakeOp{sockets: sockets, procs: procs, self: 50}
	dir := testDirWithPorts(t, "")

	for _, item := range []string{"50", "4", "4-127-0-0-1-9"} {
		op.signals = nil
		op.snapN = 0
		out, err := act(context.Background(), op, dir, "stop", item)
		if err != nil || len(op.signals) != 0 || !json.Valid(out) {
			t.Fatalf("item %s out %s err %v signals %v", item, out, err, op.signals)
		}
		var got struct{ Error text }
		if err := json.Unmarshal(out, &got); err != nil || got.Error["en"] == "" {
			t.Fatalf("item %s error %+v", item, got)
		}
	}
}

func TestForwardedPortIsLeftRunning(t *testing.T) {
	started := time.Unix(100, 0)
	sockets := []Socket{
		{PID: 42, Port: 18080, Addr: "127.0.0.1", Command: "app"},
	}
	procs := map[int]Proc{
		42: {PPID: 1, Name: "app", Started: started},
	}
	op := &fakeOp{sockets: sockets, procs: procs, self: 50}
	dir := testDirWithPorts(t, `{"grafana": 18080}`)

	out, err := act(context.Background(), op, dir, "stop", "42-127-0-0-1-18080")
	if err != nil || len(op.signals) != 0 {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
	var got struct{ Error text }
	if err := json.Unmarshal(out, &got); err != nil || got.Error["en"] != "This port is forwarded by the console, so it was left running." {
		t.Fatalf("got error %+v", got)
	}
}

func TestUnconfirmedPIDWithoutStartedIsRefused(t *testing.T) {
	sockets := []Socket{{PID: 4, Port: 8080, Addr: "127.0.0.1", Command: "app"}}
	procs := map[int]Proc{
		4: {PPID: 1, Name: "app"}, // Started is zero
	}
	op := &fakeOp{sockets: sockets, procs: procs, self: 50}
	dir := testDirWithPorts(t, "")

	out, err := act(context.Background(), op, dir, "stop", "4")
	if err != nil || len(op.signals) != 0 {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
	var got struct{ Error text }
	if err := json.Unmarshal(out, &got); err != nil || got.Error["zh-CN"] != "确认不了这个进程，所以没有动它。" {
		t.Fatalf("out %s", out)
	}
}

func TestStopSignalsTheProcessAndItsChildren(t *testing.T) {
	started := time.Unix(100, 0)
	sockets := []Socket{{PID: 4, Port: 8080, Addr: "127.0.0.1", Command: "app"}}
	procs := map[int]Proc{
		4: {PPID: 1, Name: "app", Started: started},
		5: {PPID: 4, Name: "worker1"},
		6: {PPID: 5, Name: "subworker"},
	}
	op := &fakeOp{sockets: sockets, procs: procs, self: 50, dieOnTerm: true}
	dir := testDirWithPorts(t, "")

	out, err := act(context.Background(), op, dir, "stop", "4-127-0-0-1-8080")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Message text }
	if err := json.Unmarshal(out, &got); err != nil || got.Message["en"] != "Stopped app." || got.Message["zh-CN"] != "已结束 app。" {
		t.Fatalf("out %s", out)
	}
	wantSignals := []string{"term 6", "term 5", "term 4"}
	if !reflect.DeepEqual(op.signals, wantSignals) {
		t.Fatalf("signals %v, want %v", op.signals, wantSignals)
	}
}

func TestAProcessThatSurvivesTermIsKilled(t *testing.T) {
	started := time.Unix(100, 0)
	sockets := []Socket{{PID: 4, Port: 8080, Addr: "127.0.0.1", Command: "app"}}
	procs := map[int]Proc{
		4: {PPID: 1, Name: "app", Started: started},
	}
	op := &fakeOp{sockets: sockets, procs: procs, self: 50, surviveKill: true}
	dir := testDirWithPorts(t, "")

	out, err := act(context.Background(), op, dir, "stop", "4")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Error text }
	if err := json.Unmarshal(out, &got); err != nil || got.Error["en"] != "Could not stop app." || got.Error["zh-CN"] != "停不了 app。" {
		t.Fatalf("out %s", out)
	}
	if op.slept < stopWait {
		t.Fatalf("waited %s, want at least %s", op.slept, stopWait)
	}
	if len(op.signals) < 2 || op.signals[len(op.signals)-1] != "kill 4" {
		t.Fatalf("signals %v", op.signals)
	}
}

func TestACancelledActionDoesNotSignal(t *testing.T) {
	started := time.Unix(100, 0)
	sockets := []Socket{{PID: 4, Port: 8080, Addr: "127.0.0.1", Command: "app"}}
	procs := map[int]Proc{
		4: {PPID: 1, Name: "app", Started: started},
	}
	op := &fakeOp{sockets: sockets, procs: procs, self: 50}
	dir := testDirWithPorts(t, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := act(ctx, op, dir, "stop", "4")
	if !errors.Is(err, context.Canceled) || out != nil || len(op.signals) != 0 {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
}

func TestAnObservationCannotStopAReplacementAlreadyPresentAtSubmission(t *testing.T) {
	op := &fakeOp{sockets: []Socket{{PID: 4, Port: 8080, Addr: "127.0.0.1", Command: "app"}}, procs: map[int]Proc{4: {PPID: 1, Name: "app", Started: time.Unix(100, 0)}}, self: 50, dieOnTerm: true}
	ctx := observation.WithIdentity(context.Background(), observation.Identity{PID: 4, Started: time.Unix(1, 0)})
	out, err := act(ctx, op, testDirWithPorts(t, ""), "stop", "4-127-0-0-1-8080")
	if err != nil || len(op.signals) != 0 || !strings.Contains(string(out), "changed") {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
}
