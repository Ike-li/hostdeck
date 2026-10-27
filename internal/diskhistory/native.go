package diskhistory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
)

type entry struct {
	mu       sync.Mutex
	dir      string
	identity os.FileInfo
	state    saved
	loadErr  error
	running  bool
	target   string
	cancel   context.CancelFunc
	stop     context.CancelFunc
	owner    *os.File
	retired  bool
	attempts map[string]time.Time
	done     chan struct{}
}

var entries = struct {
	sync.Mutex
	items map[string]*entry
}{items: map[string]*entry{}}

// Sync stops scans and timers for disabled, removed or changed native plugins
// in this console's plugin folder. Enabled tracking resumes after a restart.
func Sync(root string, plugins []plugin.Plugin) {
	root, _ = filepath.Abs(root)
	wanted := map[string]bool{}
	for _, p := range plugins {
		if p.Detail != nil && p.Detail.Exec != nil && slices.Equal(p.Detail.Exec.Args(), []string{"@hostdeck", "disk-history", "detail"}) {
			dir, _ := filepath.Abs(p.Dir)
			wanted[dir] = true
		}
	}
	entries.Lock()
	for dir, e := range entries.items {
		if filepath.Dir(dir) == root && !wanted[dir] {
			e.mu.Lock()
			e.retireLocked()
			e.mu.Unlock()
			delete(entries.items, dir)
		}
	}
	entries.Unlock()
	for dir := range wanted {
		get(dir)
	}
}

func (e *entry) retireLocked() {
	e.stop()
	e.retired = true
	if e.cancel != nil {
		e.cancel()
	}
	if !e.running && e.owner != nil {
		e.owner.Close()
		e.owner = nil
	}
}

// Suspend releases native file handles before a directory mutation. Windows
// refuses moving a folder while its history writer is locked. The returned
// callback restores the entry only if the original folder still exists, so
// callers can defer it for both successful and refused moves.
func Suspend(ctx context.Context, dir string) (func(), error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return func() {}, err
	}
	entries.Lock()
	e := entries.items[dir]
	if e == nil {
		entries.Unlock()
		return func() {}, nil
	}
	e.mu.Lock()
	e.retireLocked()
	done := e.done
	e.mu.Unlock()
	entries.Unlock()
	var once sync.Once
	resume := func() {
		once.Do(func() {
			entries.Lock()
			if entries.items[dir] != e {
				entries.Unlock()
				return
			}
			delete(entries.items, dir)
			info, err := os.Stat(dir)
			restore := err == nil && e.identity != nil && os.SameFile(info, e.identity)
			entries.Unlock()
			if restore {
				get(dir)
			}
		})
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			// The worker retains its lock until its filesystem read ends. Keep
			// the entry suspended until then; a retry cannot race an old writer.
			go func() { <-done; resume() }()
			return func() {}, ctx.Err()
		}
	}
	return resume, nil
}

func get(dir string) *entry {
	dir, _ = filepath.Abs(dir)
	entries.Lock()
	defer entries.Unlock()
	if e := entries.items[dir]; e != nil {
		info, err := os.Stat(dir)
		if err == nil && e.identity != nil && os.SameFile(info, e.identity) {
			return e
		}
		e.mu.Lock()
		e.retireLocked()
		e.mu.Unlock()
		delete(entries.items, dir)
	}
	s, err := load(dir)
	id, statErr := os.Stat(dir)
	if err == nil {
		err = statErr
	}
	ctx, stop := context.WithCancel(context.Background())
	e := &entry{dir: dir, identity: id, state: s, loadErr: err, stop: stop, attempts: map[string]time.Time{}}
	entries.items[dir] = e
	go e.loop(ctx)
	return e
}

func (e *entry) loop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		e.mu.Lock()
		if ctx.Err() == nil && e.state.Tracking && e.loadErr == nil && !e.running {
			if targets, err := roots(e.dir); err == nil {
				next := ""
				for _, target := range targets {
					scans := e.state.Scans[target]
					if (len(scans) == 0 || time.Since(scans[len(scans)-1].Time) >= interval) && time.Since(e.attempts[target]) >= interval {
						if next == "" || e.attempts[target].Before(e.attempts[next]) {
							next = target
						}
					}
				}
				if next != "" {
					if err := e.claimLocked(); err != nil {
						e.state.Error = err.Error()
					} else {
						e.startLocked(next)
					}
				}
			}
		}
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func id(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:8])
}

// Execute returns the same JSON as an external plugin program. The reserved
// @hostdeck command selects only these compiled operations, never a shell.
func Execute(ctx context.Context, dir string, args []string, _ run.Runner) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) < 2 || args[0] != "disk-history" {
		return nil, errors.New("unknown native disk-history command")
	}
	e := get(dir)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	respond := func(value any) ([]byte, error) { return json.Marshal(value) }
	fail := func(err error) ([]byte, error) {
		return respond(map[string]any{"error": text("Disk history: "+err.Error(), "磁盘变化："+err.Error())})
	}
	if e.retired {
		return fail(errors.New("plugin is no longer enabled"))
	}
	if e.owner == nil && !e.running {
		s, err := load(dir)
		e.loadErr = err
		if err != nil {
			return fail(err)
		}
		e.state = s
	}
	if e.loadErr != nil {
		return fail(e.loadErr)
	}
	targets, err := roots(dir)
	if err != nil {
		return fail(err)
	}
	switch {
	case len(args) == 2 && args[1] == "card":
		return respond(e.card(targets, time.Now()))
	case len(args) == 2 && args[1] == "detail":
		return respond(e.view(targets))
	case len(args) == 4 && args[1] == "action":
		i := slices.IndexFunc(targets, func(root string) bool { return id(root) == args[3] })
		if i < 0 {
			return fail(errors.New("unknown scan target"))
		}
		if !slices.Contains([]string{"scan", "track", "pause", "cancel"}, args[2]) {
			return fail(errors.New("unknown scan action"))
		}
		if err := e.claimLocked(); err != nil {
			return fail(err)
		}
		switch args[2] {
		case "scan", "track":
			if e.running {
				return fail(errors.New("a scan is already running"))
			}
			if args[2] == "track" {
				e.state.Tracking = true
				if err := save(e.dir, e.state); err != nil {
					e.state.Tracking = false
					return fail(err)
				}
			}
			e.startLocked(targets[i])
			return respond(map[string]any{"message": text("Scan started. Refresh to see progress and results.", "扫描已开始，请刷新查看进度和结果。")})
		case "pause":
			e.state.Tracking = false
			if err := save(e.dir, e.state); err != nil {
				e.state.Tracking = true
				return fail(err)
			}
			return respond(map[string]any{"message": text("Hourly tracking paused. Saved history is retained.", "已暂停每小时跟踪，保留已有历史。")})
		case "cancel":
			if !e.running {
				return fail(errors.New("no scan is running"))
			}
			wasTracking := e.state.Tracking
			e.state.Tracking = false
			if err := save(e.dir, e.state); err != nil {
				e.state.Tracking = wasTracking
				return fail(err)
			}
			e.cancel()
			return respond(map[string]any{"message": text("Cancelling the scan and pausing tracking. The previous snapshot is retained.", "正在取消扫描并暂停跟踪，保留上次快照。")})
		}
	}
	return nil, errors.New("unknown native disk-history operation")
}

func (e *entry) claimLocked() error {
	if e.owner != nil {
		return nil
	}
	f, err := claim(e.dir)
	if err != nil {
		return err
	}
	s, err := load(e.dir)
	if err != nil {
		f.Close()
		return err
	}
	e.owner, e.state = f, s
	return nil
}

func (e *entry) startLocked(target string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	e.cancel, e.running = cancel, true
	e.target = target
	e.attempts[target] = time.Now()
	done := make(chan struct{})
	e.done = done
	go func() {
		defer close(done)
		defer cancel()
		result, err := collect(ctx, target, e.dir)
		e.mu.Lock()
		defer e.mu.Unlock()
		if err == nil {
			err = ctx.Err()
		}
		e.running = false
		if e.retired {
			if e.owner != nil {
				e.owner.Close()
				e.owner = nil
			}
			return
		}
		info, statErr := os.Stat(e.dir)
		if statErr != nil || !os.SameFile(info, e.identity) {
			return
		}
		if err != nil {
			e.state.Error = err.Error()
			if saveErr := save(e.dir, e.state); saveErr != nil {
				e.state.Error += "; " + saveErr.Error()
			}
			return
		}
		before := e.state.Scans[target]
		scans := append(slices.Clone(before), result)
		scans = slices.DeleteFunc(scans, func(s snapshot) bool { return s.Time.Before(result.Time.Add(-retention)) })
		if len(scans) > maxScans {
			scans = scans[len(scans)-maxScans:]
		}
		e.state.Scans[target], e.state.Error = scans, ""
		if err := save(e.dir, e.state); err != nil {
			e.state.Scans[target], e.state.Error = before, fmt.Sprintf("history was not saved: %v", err)
		}
	}()
}
