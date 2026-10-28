package manualview

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Ike-li/hostdeck/internal/observation"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAnUnknownOrReusedProcessIsLeftAlone(t *testing.T) {
	started := time.Unix(100, 0)
	base := handProc(started)
	op := &fakeOp{listeners: map[int]Listener{4: {Command: "mitmweb", Ports: []int{8081}}}, procs: base, self: 50}
	for _, tc := range []struct {
		name string
		item string
		swap map[int]Proc
	}{
		{name: "unknown", item: "99"},
		{name: "not a number", item: "mitmweb"},
		{name: "changed", item: "4", swap: handProc(started.Add(time.Second))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op.signals = nil
			op.snapN = 0
			op.next = tc.swap
			out, err := act(context.Background(), op, "stop", tc.item)
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
	procs := handProc(started)
	procs[50] = Proc{PPID: 4, Name: "hostdeck"}
	op := &fakeOp{
		listeners: map[int]Listener{
			4:  {Command: "zsh", Ports: []int{9}},
			50: {Command: "hostdeck", Ports: []int{4678}},
		},
		procs: procs, self: 50,
	}
	// The shell is an ancestor of the console, so it is not stopped even though
	// it listens and has a shell parent of its own.
	procs[3] = Proc{PPID: 2, Name: "bash"}
	procs[4] = Proc{PPID: 3, Name: "zsh", Started: started, KnowsDir: true, Dir: "/tmp", KnowsArgs: true, Args: []string{"zsh"}, KnowsEnv: true, Env: []string{"A=b"}}
	for _, item := range []string{"50", "4"} {
		op.signals = nil
		out, err := act(context.Background(), op, "stop", item)
		if err != nil || len(op.signals) != 0 || !json.Valid(out) {
			t.Fatalf("item %s out %s err %v signals %v", item, out, err, op.signals)
		}
	}
}

func TestStopSignalsTheProcessAndItsChildrenButNotTheShell(t *testing.T) {
	op := readyOp(t)
	op.dieOnTerm = true
	out, err := act(context.Background(), op, "stop", "4")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Message text }
	if err := json.Unmarshal(out, &got); err != nil || got.Message["en"] != "Stopped mitmweb." || got.Message["zh-CN"] != "已结束 mitmweb。" {
		t.Fatalf("out %s", out)
	}
	if !reflect.DeepEqual(op.signals, []string{"term 6", "term 5", "term 4"}) {
		t.Fatalf("signals %v", op.signals)
	}
}

func TestAProcessThatSurvivesTermIsKilled(t *testing.T) {
	op := readyOp(t)
	op.surviveKill = true
	out, err := act(context.Background(), op, "stop", "4")
	if err != nil || len(op.spawned) != 0 {
		t.Fatalf("out %s err %v spawned %d", out, err, len(op.spawned))
	}
	var got struct{ Error text }
	if err := json.Unmarshal(out, &got); err != nil || got.Error["en"] != "Could not stop mitmweb." {
		t.Fatalf("out %s", out)
	}
	if op.slept < stopWait {
		t.Fatalf("waited %s", op.slept)
	}
	if !reflect.DeepEqual(op.signals[len(op.signals)-3:], []string{"kill 6", "kill 5", "kill 4"}) {
		t.Fatalf("signals %v", op.signals)
	}
}

func TestRestartLeavesABareExecutableRunning(t *testing.T) {
	op := readyOp(t)
	p := op.procs[4]
	p.Exe = "mitmweb"
	p.Args = []string{"mitmweb", "--listen", "8081"}
	op.procs[4] = p
	out, err := act(context.Background(), op, "restart", "4")
	if err != nil || len(op.signals) != 0 || len(op.spawned) != 0 {
		t.Fatalf("out %s err %v signals %v spawned %d", out, err, op.signals, len(op.spawned))
	}
}

func TestRestartUsesTheRecordedCommandDirectoryAndEnvironment(t *testing.T) {
	op := readyOp(t)
	op.dieOnTerm = true
	out, err := act(context.Background(), op, "restart", "4")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Message text }
	if err := json.Unmarshal(out, &got); err != nil || got.Message["zh-CN"] != "已重新启动 mitmweb。" {
		t.Fatalf("out %s", out)
	}
	if len(op.spawned) != 1 {
		t.Fatalf("spawned %+v", op.spawned)
	}
	gotSpawn := op.spawned[0]
	want := spawnCall{
		exe: recordedProgram(), args: []string{"mitmweb", "--listen", "8081"},
		dir: "/tmp/proxy", env: []string{},
	}
	if !reflect.DeepEqual(gotSpawn, want) {
		t.Fatalf("spawn %+v", gotSpawn)
	}
	if len(op.signals) == 0 || op.spawnedAfter != len(op.signals) {
		t.Fatalf("spawned at %d signals %v", op.spawnedAfter, op.signals)
	}
}

func TestAFailedRestartSaysTheProcessWasStopped(t *testing.T) {
	op := readyOp(t)
	op.dieOnTerm = true
	op.spawnErr = errors.New("no")
	out, err := act(context.Background(), op, "restart", "4")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Error text }
	if err := json.Unmarshal(out, &got); err != nil || got.Error["en"] != "Stopped mitmweb, but it could not be started again." || got.Error["zh-CN"] != "已结束 mitmweb，但没有重新启动。" {
		t.Fatalf("out %s", out)
	}
}

func TestACancelledActionDoesNotSignal(t *testing.T) {
	op := readyOp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := act(ctx, op, "stop", "4")
	if !errors.Is(err, context.Canceled) || out != nil || len(op.signals) != 0 {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
}

func TestRestartDoesNotStopAProcessWhoseEnvironmentWasNotRead(t *testing.T) {
	op := readyOp(t)
	op.procs[4] = without(op.procs[4], func(p *Proc) { p.KnowsEnv = false; p.Env = nil })
	out, err := act(context.Background(), op, "restart", "4")
	if err != nil || len(op.signals) != 0 || len(op.spawned) != 0 {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
	var got struct{ Error text }
	if err := json.Unmarshal(out, &got); err != nil || got.Error["zh-CN"] != "读不到命令、目录或环境，所以没有动它。" {
		t.Fatalf("out %s", out)
	}
}

// filepath.IsAbs follows the OS this test runs on. A Unix path is not
// absolute to the Windows build, so the fixture uses one that is.
func recordedProgram() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files\mitmweb\mitmweb.exe`
	}
	return "/usr/local/bin/mitmweb"
}

func handProc(started time.Time) map[int]Proc {
	return map[int]Proc{
		4: {
			PPID: 3, Name: "mitmweb", Uptime: time.Minute, Started: started,
			Dir: "/tmp/proxy", Exe: recordedProgram(),
			Args: []string{"mitmweb", "--listen", "8081"},
			Env:  []string{}, KnowsDir: true, KnowsArgs: true, KnowsEnv: true,
		},
		5: {PPID: 4, Name: "child"},
		6: {PPID: 5, Name: "grandchild"},
		3: {PPID: 2, Name: "zsh"},
		2: {PPID: 1, Name: "Code Helper (Plugin)"},
	}
}

func readyOp(t *testing.T) *fakeOp {
	t.Helper()
	return &fakeOp{
		listeners: map[int]Listener{4: {Command: "mitmweb", Ports: []int{8081}}},
		procs:     handProc(time.Unix(100, 0)),
		self:      50,
		alive:     map[int]bool{4: true, 5: true, 6: true, 3: true},
	}
}

func without(p Proc, edit func(*Proc)) Proc {
	edit(&p)
	return p
}

type spawnCall struct {
	exe, cmdline, dir string
	args, env         []string
}

type fakeOp struct {
	listeners    map[int]Listener
	procs        map[int]Proc
	next         map[int]Proc
	self         int
	snapN        int
	alive        map[int]bool
	dieOnTerm    bool
	surviveKill  bool
	signals      []string
	spawned      []spawnCall
	spawnedAfter int
	spawnErr     error
	slept        time.Duration
}

func (f *fakeOp) Snapshot(context.Context) (map[int]Listener, map[int]Proc, error) {
	f.snapN++
	procs := f.procs
	if f.snapN > 1 && f.next != nil {
		procs = f.next
	}
	return f.listeners, procs, nil
}
func (f *fakeOp) Self() int { return f.self }
func (f *fakeOp) Term(pid int) error {
	f.signals = append(f.signals, "term "+itoa(pid))
	if f.dieOnTerm {
		f.alive[pid] = false
	}
	return nil
}
func (f *fakeOp) Kill(pid int) error {
	f.signals = append(f.signals, "kill "+itoa(pid))
	if !f.surviveKill {
		f.alive[pid] = false
	}
	return nil
}
func (f *fakeOp) Alive(pid int) bool    { return f.alive[pid] }
func (f *fakeOp) Sleep(d time.Duration) { f.slept += d }
func (f *fakeOp) Spawn(exe string, args []string, cmdline, dir string, env []string) error {
	f.spawnedAfter = len(f.signals)
	f.spawned = append(f.spawned, spawnCall{exe: exe, args: args, cmdline: cmdline, dir: dir, env: env})
	return f.spawnErr
}

func itoa(n int) string {
	return jsonNumber(n)
}

func jsonNumber(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestAnObservationCannotStopAReplacementAlreadyPresentAtSubmission(t *testing.T) {
	op := readyOp(t)
	op.dieOnTerm = true
	ctx := observation.WithIdentity(context.Background(), observation.Identity{PID: 4, Started: time.Unix(1, 0)})
	out, err := act(ctx, op, "stop", "4")
	if err != nil || len(op.signals) != 0 || !strings.Contains(string(out), "changed") {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
}

func TestCapturePreparationFailureLeavesTheOriginalServiceRunning(t *testing.T) {
	op := readyOp(t)
	op.dieOnTerm = true
	store := observation.New(t.TempDir())
	id, _ := store.Create("manual", "4", "restart", observation.Target{})
	out, err := act(observation.WithCapture(context.Background(), store.Root(), id), op, "restart", "4")
	if err != nil || len(op.signals) != 0 || len(op.spawned) != 0 || !strings.Contains(string(out), "left running") {
		t.Fatalf("out %s err %v signals %v", out, err, op.signals)
	}
}
