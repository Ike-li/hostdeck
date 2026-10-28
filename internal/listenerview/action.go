package listenerview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ike-li/hostdeck/internal/observation"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	stopWait = 5 * time.Second
	stopTick = 50 * time.Millisecond
)

var errUnknownAction = errors.New("unknown listeners command")

type operator interface {
	Snapshot(context.Context) ([]Socket, map[int]Proc, error)
	Self() int
	Term(pid int) error
	Kill(pid int) error
	Alive(pid int) bool
	Sleep(time.Duration)
}

type hit struct {
	pid     int
	port    int
	name    string
	started time.Time
	refusal text
	noteSet bool
}

func (h hit) note() ([]byte, bool) {
	if !h.noteSet {
		return nil, false
	}
	out, _ := json.Marshal(struct {
		Error text `json:"error"`
	}{h.refusal})
	return out, true
}

func noted(reason text) hit {
	return hit{noteSet: true, refusal: reason}
}

func act(ctx context.Context, op operator, dir, action, item string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if action != "stop" {
		return nil, errUnknownAction
	}
	pid, port, err := parseItemTarget(item)
	if err != nil || pid <= 1 {
		return refused("That process is no longer listed, so it was left alone.", "这个进程已经不在列表里，所以没有动它。")
	}
	h, procs, err := confirmed(ctx, op, dir, pid, port)
	if err != nil {
		return nil, err
	}
	if refusal, ok := h.note(); ok {
		return refusal, nil
	}
	if expected, ok := observation.ExpectedIdentity(ctx); ok && !expected.Equal(observation.Identity{PID: h.pid, Started: h.started}) {
		return refused("That process changed, so it was left alone.", "这个进程已经变了，所以没有动它。")
	}
	name := h.name
	if name == "" {
		name = fmt.Sprintf("pid %d", pid)
	}
	if err := stopPIDs(ctx, op, descendantsFirst(pid, procs)); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return refused(fmt.Sprintf("Could not stop %s.", name), fmt.Sprintf("停不了 %s。", name))
	}
	return told(fmt.Sprintf("Stopped %s.", name), fmt.Sprintf("已结束 %s。", name))
}

func parseItemTarget(item string) (int, int, error) {
	item = strings.TrimSpace(item)
	if item == "" {
		return 0, 0, errors.New("empty item")
	}
	parts := strings.Split(item, "-")
	pid, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	port := 0
	if len(parts) >= 3 {
		if p, perr := strconv.Atoi(parts[len(parts)-1]); perr == nil {
			port = p
		}
	}
	return pid, port, nil
}

func confirmed(ctx context.Context, op operator, dir string, pid, port int) (hit, map[int]Proc, error) {
	h, _, err := oneHit(ctx, op, dir, pid, port)
	if err != nil || h.noteSet {
		return h, nil, err
	}
	started := h.started
	again, procs, err := oneHit(ctx, op, dir, pid, port)
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

func oneHit(ctx context.Context, op operator, dir string, pid, port int) (hit, map[int]Proc, error) {
	sockets, procs, err := op.Snapshot(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return hit{}, nil, ctx.Err()
		}
		var failure *readFailure
		if errors.As(err, &failure) {
			return noted(words(failure.en, failure.zh)), nil, nil
		}
		return noted(words("Could not list listening ports.", "读不到正在监听的端口。")), nil, nil
	}
	h, ok := findHit(sockets, procs, pid, port)
	if !ok {
		return noted(words("That process is no longer listed, so it was left alone.", "这个进程已经不在列表里，所以没有动它。")), procs, nil
	}
	if pid == op.Self() {
		return noted(words("This is the console, so it was left running.", "这是本控制台，所以没有动它。")), procs, nil
	}
	if dir != "" {
		if forwarded, _ := loadForwarded(dir); forwarded != nil {
			if _, ok := forwarded[h.port]; ok {
				return noted(words("This port is forwarded by the console, so it was left running.", "这个端口由本控制台转发，所以没有动它。")), procs, nil
			}
		}
	}
	if ancestorOf(pid, op.Self(), procs) {
		return noted(words("That process started the console, so it was left running.", "这个进程启动了本控制台，所以没有动它。")), procs, nil
	}
	if h.started.IsZero() {
		return noted(words("Could not confirm this process, so it was left running.", "确认不了这个进程，所以没有动它。")), procs, nil
	}
	return h, procs, nil
}

func findHit(sockets []Socket, procs map[int]Proc, pid, port int) (hit, bool) {
	var match *Socket
	for i := range sockets {
		s := &sockets[i]
		if s.PID != pid {
			continue
		}
		if port > 0 && s.Port == port {
			match = s
			break
		}
		if match == nil {
			match = s
		}
	}
	if match == nil {
		return hit{}, false
	}
	name := strings.TrimSpace(match.Command)
	if name == "" {
		if p, ok := procs[pid]; ok {
			name = strings.TrimSpace(p.Name)
		}
	}
	if name == "" {
		name = fmt.Sprintf("pid %d", pid)
	}
	started := time.Time{}
	if p, ok := procs[pid]; ok {
		started = p.Started
	}
	return hit{
		pid:     pid,
		port:    match.Port,
		name:    name,
		started: started,
	}, true
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
