package manualview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ike-li/hostdeck/internal/observation"
	"sort"
	"strconv"
	"time"
)

const (
	stopWait = 5 * time.Second
	stopTick = 50 * time.Millisecond
)

// operator is the process boundary an action uses. Snapshot includes the
// directory, command and environment when they could be read. Spawn starts a
// process that outlives the action.
type operator interface {
	Snapshot(context.Context) (map[int]Listener, map[int]Proc, error)
	Self() int
	Term(pid int) error
	Kill(pid int) error
	Alive(pid int) bool
	Sleep(time.Duration)
	Spawn(exe string, args []string, cmdline, dir string, env []string) error
}

func act(ctx context.Context, op operator, action, item string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if action != "stop" && action != "restart" {
		return nil, errors.New("unknown manual command")
	}
	pid, err := strconv.Atoi(item)
	if err != nil || pid <= 1 {
		return refused("That service is no longer listed, so it was left alone.", "这个服务已经不在列表里，所以没有动它。")
	}
	h, procs, err := confirmed(ctx, op, pid)
	if err != nil {
		return nil, err
	}
	if refusal, ok := h.note(); ok {
		return refusal, nil
	}
	if expected, ok := observation.ExpectedIdentity(ctx); ok && !expected.Equal(observation.Identity{PID: h.pid, Started: h.started}) {
		return refused("That process changed, so it was left alone.", "这个进程已经变了，所以没有动它。")
	}
	if action == "restart" && !canRestart(h) {
		return refused("Could not read the command, directory, or environment, so it was left running.", "读不到命令、目录或环境，所以没有动它。")
	}
	argv := append([]string(nil), h.args...)
	env := append([]string(nil), h.env...)
	if h.knowsEnv && env == nil {
		env = []string{}
	}
	exe, dir, cmdline, name := launchExe(h), h.dir, h.cmdline, h.command
	var prepared *observation.Prepared
	if action == "restart" {
		prepared, err = observation.PrepareFrom(ctx, observation.Launch{Executable: exe, Arguments: argv, CommandLine: cmdline, Directory: dir, Environment: env})
		if err != nil {
			if errors.Is(err, observation.ErrCapacity) {
				return refused("Capture storage has reached its 128 MiB budget. Active runs were retained and the original service was left running.", "日志采集存储已达到 128 MiB 上限。已保留活动记录，原服务仍保持运行。")
			}
			return refused("Capture could not be prepared; the original service was left running. "+err.Error(), "日志采集准备失败，原服务仍保持运行。"+err.Error())
		}
		if prepared == nil {
			if _, real := op.(osOperator); real {
				return refused("A capture session is required; the service was left running.", "缺少日志采集会话，原服务仍保持运行。")
			}
		}
		defer prepared.Close()
	}
	// Preparation can take time. Reconfirm the original identity before signals.
	again, latest, checkErr := confirmed(ctx, op, pid)
	if checkErr != nil {
		return nil, checkErr
	}
	if refusal, ok := again.note(); ok {
		return refusal, nil
	}
	if !again.started.Equal(h.started) {
		return refused("That process changed, so it was left alone.", "这个进程已经变了，所以没有动它。")
	}
	procs = latest
	if err := stopPIDs(ctx, op, descendantsFirst(pid, procs)); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return refused(fmt.Sprintf("Could not stop %s.", name), fmt.Sprintf("停不了 %s。", name))
	}
	if action == "stop" {
		return told(fmt.Sprintf("Stopped %s.", name), fmt.Sprintf("已结束 %s。", name))
	}
	var spawnErr error
	if prepared != nil {
		_, spawnErr = prepared.Start()
	} else {
		spawnErr = op.Spawn(exe, argv, cmdline, dir, env)
	}
	if errors.Is(spawnErr, observation.ErrStartupUnknown) {
		return refused("The old service stopped, but the new startup result is unknown. Check Recent operations before trying again.", "原服务已结束，新进程的启动结果未知。再次操作前先查看最近操作。")
	}
	if spawnErr != nil {
		return refused(
			fmt.Sprintf("Stopped %s, but it could not be started again.", name),
			fmt.Sprintf("已结束 %s，但没有重新启动。", name),
		)
	}
	return told(fmt.Sprintf("Restarted %s.", name), fmt.Sprintf("已重新启动 %s。", name))
}

// confirmed reads the process twice and keeps it only when it is still the
// same listener. A refusal is returned as a hit whose note is set.
func confirmed(ctx context.Context, op operator, pid int) (hit, map[int]Proc, error) {
	h, _, err := oneHit(ctx, op, pid)
	if err != nil || h.noteSet {
		return h, nil, err
	}
	started := h.started
	again, procs, err := oneHit(ctx, op, pid)
	if err != nil || again.noteSet {
		return again, procs, err
	}
	if !again.started.Equal(started) {
		again.refusal = words("That process changed, so it was left alone.", "这个进程已经变了，所以没有动它。")
		again.noteSet = true
		return again, nil, nil
	}
	return again, procs, nil
}

func oneHit(ctx context.Context, op operator, pid int) (hit, map[int]Proc, error) {
	listeners, procs, err := op.Snapshot(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return hit{}, nil, ctx.Err()
		}
		var failure *readFailure
		if errors.As(err, &failure) {
			return noted(words(failure.en, failure.zh)), nil, nil
		}
		return noted(words("Could not list services started by hand.", "读不到手动启动的服务。")), nil, nil
	}
	h, ok := findHit(selectHits(listeners, procs), pid)
	if !ok {
		return noted(words("That service is no longer listed, so it was left alone.", "这个服务已经不在列表里，所以没有动它。")), procs, nil
	}
	switch {
	case pid == op.Self():
		return noted(words("This is the console, so it was left running.", "这是本控制台，所以没有动它。")), procs, nil
	case ancestorOf(pid, op.Self(), procs):
		return noted(words("That process started the console, so it was left running.", "这个进程启动了本控制台，所以没有动它。")), procs, nil
	case h.started.IsZero():
		return noted(words("Could not confirm this process, so it was left running.", "确认不了这个进程，所以没有动它。")), procs, nil
	}
	return h, procs, nil
}

func noted(reason text) hit { return hit{noteSet: true, refusal: reason} }

func (h hit) note() ([]byte, bool) {
	if !h.noteSet {
		return nil, false
	}
	out, _ := json.Marshal(struct {
		Error text `json:"error"`
	}{h.refusal})
	return out, true
}

func findHit(hits []hit, pid int) (hit, bool) {
	for _, h := range hits {
		if h.pid == pid {
			return h, true
		}
	}
	return hit{}, false
}

func ancestorOf(pid, self int, procs map[int]Proc) bool {
	if self <= 0 || pid == self {
		return false
	}
	seen := map[int]bool{}
	cur := self
	for {
		proc, ok := procs[cur]
		if !ok || proc.PPID <= 1 || seen[proc.PPID] {
			return false
		}
		if proc.PPID == pid {
			return true
		}
		seen[proc.PPID] = true
		cur = proc.PPID
	}
}

func descendantsFirst(root int, procs map[int]Proc) []int {
	kids := map[int][]int{}
	for pid, proc := range procs {
		kids[proc.PPID] = append(kids[proc.PPID], pid)
	}
	for ppid := range kids {
		sort.Ints(kids[ppid])
	}
	var out []int
	var walk func(int, map[int]bool)
	walk = func(pid int, seen map[int]bool) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		for _, kid := range kids[pid] {
			walk(kid, seen)
		}
		out = append(out, pid)
	}
	walk(root, map[int]bool{})
	return out
}

func stopPIDs(ctx context.Context, op operator, pids []int) error {
	signal := func(fn func(int) error) {
		for _, pid := range pids {
			if pid <= 1 || pid == op.Self() {
				continue
			}
			_ = fn(pid)
		}
	}
	signal(op.Term)
	if waitUntilDead(ctx, op, pids, stopWait) {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	signal(op.Kill)
	if waitUntilDead(ctx, op, pids, time.Second) {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("still running")
}

func waitUntilDead(ctx context.Context, op operator, pids []int, limit time.Duration) bool {
	var waited time.Duration
	for {
		if !anyAlive(op, pids) {
			return true
		}
		if waited >= limit || ctx.Err() != nil {
			return false
		}
		step := stopTick
		if waited+step > limit {
			step = limit - waited
		}
		op.Sleep(step)
		waited += step
	}
}

func anyAlive(op operator, pids []int) bool {
	for _, pid := range pids {
		if pid <= 1 || pid == op.Self() {
			continue
		}
		if op.Alive(pid) {
			return true
		}
	}
	return false
}

func refused(en, zh string) ([]byte, error) {
	return json.Marshal(struct {
		Error text `json:"error"`
	}{words(en, zh)})
}

func told(en, zh string) ([]byte, error) {
	return json.Marshal(struct {
		Message text `json:"message"`
	}{words(en, zh)})
}
