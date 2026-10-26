package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/process"
)

type running struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	reason string
}
type entry struct {
	mu                            sync.Mutex
	dir                           string
	root                          *os.Root
	identity                      os.FileInfo
	owner                         *os.File
	cfg                           Config
	valid                         bool
	configErr, stateErr, ownerErr error
	state                         saved
	running                       map[string]*running
	stopping, retired             bool
}

var registry = struct {
	sync.Mutex
	items map[string]*entry
}{items: map[string]*entry{}}

func IsNative(p plugin.Plugin) bool {
	return p.Detail != nil && p.Detail.Exec != nil && slices.Equal(p.Detail.Exec.Args(), []string{"@hostdeck", "tasks", "detail"})
}
func lookup(dir string) *entry {
	dir, _ = filepath.Abs(dir)
	registry.Lock()
	defer registry.Unlock()
	return registry.items[dir]
}

// Sync is called only on startup or explicit plugin reload, never by a read.
func Sync(root string, plugins []plugin.Plugin) error {
	root, _ = filepath.Abs(root)
	wanted := map[string]bool{}
	for _, p := range plugins {
		if IsNative(p) {
			dir, _ := filepath.Abs(p.Dir)
			wanted[dir] = true
		}
	}
	registry.Lock()
	var old []*entry
	for dir, e := range registry.items {
		if filepath.Dir(dir) == root && !wanted[dir] {
			old = append(old, e)
		}
	}
	registry.Unlock()
	for _, e := range old {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := stopEntry(ctx, e, "plugin_disabled")
		cancel()
		if err != nil {
			return err
		}
		registry.Lock()
		delete(registry.items, e.dir)
		registry.Unlock()
	}
	for dir := range wanted {
		e := lookup(dir)
		if e == nil {
			r, err := os.OpenRoot(dir)
			if err != nil {
				return err
			}
			info, err := os.Stat(dir)
			if err != nil {
				r.Close()
				return err
			}
			e = &entry{dir: dir, root: r, identity: info, state: emptyState(), running: map[string]*running{}}
			registry.Lock()
			registry.items[dir] = e
			registry.Unlock()
		}
		e.mu.Lock()
		e.refreshLocked()
		e.mu.Unlock()
	}
	return nil
}

func (e *entry) refreshLocked() {
	c, err := Load(e.dir)
	e.configErr = err
	if err == nil {
		ids := map[string]bool{}
		for _, t := range c.Tasks {
			ids[t.ID] = true
		}
		for id, r := range e.running {
			if !ids[id] {
				r.reason = "task_removed"
				r.cancel()
			}
		}
		e.cfg = c
		e.valid = true
	}
	if e.owner == nil {
		e.state, e.stateErr = loadState(e.root)
		e.ownerErr = nil
	}
}

func stopEntry(ctx context.Context, e *entry, reason string) error {
	e.mu.Lock()
	if e.retired {
		e.mu.Unlock()
		return nil
	}
	e.stopping = true
	var wait []chan struct{}
	for _, r := range e.running {
		if r.reason == "" {
			r.reason = reason
		}
		r.cancel()
		wait = append(wait, r.done)
	}
	e.mu.Unlock()
	for _, done := range wait {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.retired {
		return nil
	}
	e.retired = true
	if e.owner != nil {
		e.owner.Close()
		e.owner = nil
	}
	return e.root.Close()
}

func Close(ctx context.Context, root string) error {
	root, _ = filepath.Abs(root)
	registry.Lock()
	var found []*entry
	for dir, e := range registry.items {
		if filepath.Dir(dir) == root {
			found = append(found, e)
		}
	}
	registry.Unlock()
	for _, e := range found {
		if err := stopEntry(ctx, e, "shutdown"); err != nil {
			return err
		}
		registry.Lock()
		delete(registry.items, e.dir)
		registry.Unlock()
	}
	return nil
}

func Suspend(ctx context.Context, dir string) (func(), error) {
	e := lookup(dir)
	if e == nil {
		return func() {}, nil
	}
	if err := stopEntry(ctx, e, "plugin_disabled"); err != nil {
		return func() {}, err
	}
	registry.Lock()
	delete(registry.items, e.dir)
	registry.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			info, err := os.Stat(e.dir)
			if err != nil || !os.SameFile(info, e.identity) {
				return
			}
			root, err := os.OpenRoot(e.dir)
			if err != nil {
				return
			}
			restored := &entry{dir: e.dir, root: root, identity: info, state: emptyState(), running: map[string]*running{}}
			restored.refreshLocked()
			registry.Lock()
			if registry.items[e.dir] == nil {
				registry.items[e.dir] = restored
			} else {
				root.Close()
			}
			registry.Unlock()
		})
	}, nil
}

func Issue(dir string) plugin.Text {
	e := lookup(dir)
	if e == nil {
		return plugin.Text{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, err := range []error{e.configErr, e.stateErr, e.ownerErr} {
		if err != nil {
			return text("Scheduled tasks need attention: "+err.Error(), "定时任务需要处理："+err.Error())
		}
	}
	return plugin.Text{}
}

func (e *entry) task(id string) (Task, bool) {
	for _, t := range e.cfg.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}
func (e *entry) runtimeLocked(t Task) taskState {
	ts, ok := e.state.Tasks[t.ID]
	d := digest(t)
	sd := digest(t.Schedule)
	if !ok {
		ts = taskState{Revision: randomID("rev-"), Digest: d, ScheduleDigest: sd}
	}
	if ts.Digest != d {
		ts.Revision = randomID("rev-")
		ts.Digest = d
	}
	if ts.ScheduleDigest != sd {
		ts.ScheduleDigest = sd
		ts.Anchor = nil
		ts.Cursor = time.Time{}
	}
	e.state.Tasks[t.ID] = ts
	return ts
}

func (e *entry) action(op, id string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping || e.retired || !e.valid {
		return "", errors.New("task plugin is not active")
	}
	t, ok := e.task(id)
	if !ok {
		return "", errors.New("task no longer exists")
	}
	if strings.HasPrefix(op, "stop:") {
		r := e.running[id]
		if r == nil || op != "stop:"+r.id || r.ctx.Err() != nil {
			return "", errors.New("this run is no longer stoppable")
		}
		r.reason = "stop"
		r.cancel()
		return r.id, nil
	}
	if op != "run" {
		return "", errors.New("unknown task action")
	}
	return e.startLocked(t, "manual", nil)
}

func (e *entry) startLocked(t Task, trigger string, due *time.Time) (string, error) {
	if e.stateErr != nil {
		return "", e.stateErr
	}
	if e.ownerErr != nil {
		return "", e.ownerErr
	}
	if !*t.Enabled || e.state.Tasks[t.ID].Paused {
		return "", errors.New("task is disabled or paused")
	}
	for _, r := range e.state.Runs {
		if r.TaskID == t.ID && e.state.Active[r.ID].Token != "" && !activeStatus(r.Status) {
			return "", errors.New("the previous process tree needs local inspection before recovery")
		}
	}
	if e.running[t.ID] != nil || len(e.running) >= 4 {
		return "", errors.New("task is running or all four execution slots are busy")
	}
	if err := e.claimLocked(); err != nil {
		e.ownerErr = err
		return "", err
	}
	// Ownership can reveal interrupted state written by the previous owner.
	if e.state.Tasks[t.ID].Paused {
		return "", errors.New("task was interrupted and is paused")
	}
	if err := e.pruneLocked(1); err != nil {
		return "", err
	}
	ts := e.runtimeLocked(t)
	id := randomID("run-")
	if err := e.root.MkdirAll("data/logs", 0700); err != nil {
		return "", err
	}
	outPath, errPath := "data/logs/"+id+".stdout.log", "data/logs/"+id+".stderr.log"
	out, err := e.root.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	errout, err := e.root.OpenFile(errPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		out.Close()
		e.root.Remove(outPath)
		return "", err
	}
	now := time.Now().UTC()
	e.state.Runs = append(e.state.Runs, Record{ID: id, TaskID: t.ID, Revision: ts.Revision, Trigger: trigger, ScheduledAt: due, AcceptedAt: now, Status: "starting", Logs: logs{Stdout: &outPath, Stderr: &errPath}})
	e.state.Active[id] = process.Identity{Token: id}
	if err = e.saveLocked(); err != nil {
		e.state.Runs = e.state.Runs[:len(e.state.Runs)-1]
		delete(e.state.Active, id)
		out.Close()
		errout.Close()
		e.root.Remove(outPath)
		e.root.Remove(errPath)
		e.stateErr = err
		return "", err
	}
	duration, _ := time.ParseDuration(t.Timeout)
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	r := &running{id: id, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	e.running[t.ID] = r
	go e.execute(t, r, out, errout)
	return id, nil
}

func environment(t Task) ([]string, []string, error) {
	env := os.Environ()
	secrets := []string{}
	for key, reference := range t.Exec.Env {
		match := envRef.FindStringSubmatch(reference)
		value, ok := os.LookupEnv(match[1])
		if !ok {
			return nil, secrets, fmt.Errorf("environment reference %s is missing", match[1])
		}
		filtered := env[:0]
		for _, entry := range env {
			k, _, _ := strings.Cut(entry, "=")
			same := k == key
			if runtime.GOOS == "windows" {
				same = strings.EqualFold(k, key)
			}
			if !same {
				filtered = append(filtered, entry)
			}
		}
		env = append(filtered, key+"="+value)
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return env, secrets, nil
}

func (e *entry) execute(t Task, r *running, out, errout *os.File) {
	defer r.cancel()
	env, secrets, envErr := environment(t)
	stdout, stderr := newLog(out, secrets), newLog(errout, secrets)
	result := process.Outcome{Error: envErr, Clean: true}
	executable, exeErr := os.Executable()
	if envErr == nil && exeErr == nil {
		command := t.Exec.Command
		if runtime.GOOS == "windows" && t.Exec.Windows != nil {
			command = t.Exec.Windows
		}
		result = process.Run(r.ctx, executable, process.Spec{Command: command, Dir: localDir(e.dir, t.Exec.Dir), Env: env, Token: r.id}, process.Hooks{Stdout: stdout, Stderr: stderr, Prepared: func(identity process.Identity) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.state.Active[r.id] = identity
			return e.saveLocked()
		}, Started: func(pid int) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			record := e.record(r.id)
			now := time.Now().UTC()
			record.StartedAt = &now
			record.Status = "running"
			return e.saveLocked()
		}})
	} else if exeErr != nil {
		result.Error = exeErr
	}
	logErr := errors.Join(stdout.Close(), stderr.Close())
	e.mu.Lock()
	defer e.mu.Unlock()
	defer close(r.done)
	record := e.record(r.id)
	now := time.Now().UTC()
	record.FinishedAt = &now
	record.ExitCode = result.Code
	record.Status = "succeeded"
	record.Logs.StdoutTruncated = stdout.Truncated
	record.Logs.StderrTruncated = stderr.Truncated
	switch {
	case !result.Clean:
		record.Status = "interrupted"
		record.Reason = str("cleanup")
		ts := e.state.Tasks[t.ID]
		ts.Paused = true
		e.state.Tasks[t.ID] = ts
	case errors.Is(result.Error, context.DeadlineExceeded):
		record.Status = "timed_out"
		record.Reason = str("timeout")
	case r.reason != "":
		record.Status = "cancelled"
		record.Reason = str(r.reason)
	case envErr != nil:
		record.Status = "failed"
		record.Reason = str("environment")
	case logErr != nil:
		record.Status = "failed"
		record.Reason = str("log_io")
	case result.Error != nil:
		record.Status = "failed"
		record.Reason = str("start")
		var ioErr *logIOError
		if errors.As(result.Error, &ioErr) {
			record.Reason = str("log_io")
		} else if result.Code != nil {
			record.Reason = str("exit")
		}
	}
	if result.Error != nil {
		record.Diagnostic = redact(result.Error.Error(), secrets)
	}
	if logErr != nil {
		record.Diagnostic = redact(logErr.Error(), secrets)
	}
	if len(record.Diagnostic) > 1024 {
		record.Diagnostic = record.Diagnostic[:1024]
	}
	if result.Clean {
		delete(e.state.Active, r.id)
	}
	delete(e.running, t.ID)
	if err := e.pruneLocked(0); err != nil {
		e.stateErr = err
	}
	if err := e.saveLocked(); err != nil {
		e.stateErr = err
	}
}

func marshalAction(id string, err error) ([]byte, error) {
	if err != nil {
		return json.Marshal(map[string]any{"error": text("Task action failed: "+err.Error(), "任务操作失败："+err.Error())})
	}
	return json.Marshal(map[string]any{"message": text("Accepted task operation for run "+id, "已接受任务操作，运行编号 "+id)})
}
