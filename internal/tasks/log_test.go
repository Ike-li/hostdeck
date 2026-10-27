package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/process"
)

func TestASecretSplitAcrossWritesAndTheCaptureBoundaryIsStillRedacted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "stream")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := newLog(f, []string{"abcdefghijklmnop"})
	w.Write([]byte(strings.Repeat("x", (1<<20)-7)))
	for _, b := range []byte("abcdefghijklmnop") {
		w.Write([]byte{b})
	}
	w.Write([]byte("tail-more"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != (1<<20) || !w.Truncated || strings.Contains(string(b), "abcdef") || !strings.Contains(string(b), "***") {
		t.Fatal("capture or cross-write redaction failed")
	}
}

func TestTaskAndScriptRunsRecordTheSameOutcomeAndOnlyTasksPause(t *testing.T) {
	zero, seven := 0, 7
	for _, tc := range []struct {
		name    string
		outcome process.Outcome
		status  string
		reason  string
		pauses  bool
	}{
		{
			name:    "succeeded",
			outcome: process.Outcome{Code: &zero, Clean: true},
			status:  "succeeded",
		},
		{
			name:    "timeout",
			outcome: process.Outcome{Error: context.DeadlineExceeded, Clean: true},
			status:  "timed_out",
			reason:  "timeout",
		},
		{
			name:    "nonzero exit",
			outcome: process.Outcome{Code: &seven, Error: errors.New("exit status 7"), Clean: true},
			status:  "failed",
			reason:  "exit",
		},
		{
			name:    "log write failure",
			outcome: process.Outcome{Error: &logIOError{errors.New("disk full")}, Clean: true},
			status:  "failed",
			reason:  "log_io",
		},
		{
			name:    "cleanup failure",
			outcome: process.Outcome{Error: errors.New("process tree still alive"), Clean: false},
			status:  "interrupted",
			reason:  "cleanup",
			pauses:  true,
		},
		{
			name:    "uncertain worker loss",
			outcome: process.Outcome{Error: errors.New("worker pipe closed"), Clean: true, Uncertain: true},
			status:  "interrupted",
			reason:  "recovery",
			pauses:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newEntry := func(t *testing.T) *entry {
				t.Helper()
				dir := t.TempDir()
				root, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { root.Close() })
				info, err := os.Stat(dir)
				if err != nil {
					t.Fatal(err)
				}
				if err := root.MkdirAll("data/logs", 0700); err != nil {
					t.Fatal(err)
				}
				every := "1m"
				enabled := true
				task := Task{ID: "sample", Name: "Sample", Enabled: &enabled, Schedule: Schedule{Every: &every}, Exec: Execution{Command: []string{"true"}}, Timeout: "5s"}
				e := &entry{
					dir:           dir,
					root:          root,
					identity:      info,
					cfg:           Config{Version: 1, Tasks: []Task{task}},
					valid:         true,
					state:         emptyState(),
					running:       map[string]*running{},
					scriptRunning: map[string]*running{},
					clock:         realClock{},
					next:          map[string]time.Time{},
				}
				if err := e.claimLocked(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if e.owner != nil {
						e.owner.Close()
					}
				})
				e.runtimeLocked(task)
				return e
			}
			openLogs := func(t *testing.T, e *entry, id string) (*logWriter, *logWriter, logs) {
				t.Helper()
				outPath, errPath := "data/logs/"+id+".stdout.log", "data/logs/"+id+".stderr.log"
				out, err := e.root.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				errout, err := e.root.OpenFile(errPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					out.Close()
					t.Fatal(err)
				}
				return newLog(out, nil), newLog(errout, nil), logs{Stdout: &outPath, Stderr: &errPath}
			}

			taskEntry := newEntry(t)
			now := taskEntry.now()
			stdout, stderr, taskLogs := openLogs(t, taskEntry, "run-task")
			taskEntry.state.Runs = append(taskEntry.state.Runs, Record{
				ID:         "run-task",
				TaskID:     "sample",
				Revision:   taskEntry.state.Tasks["sample"].Revision,
				Trigger:    "manual",
				AcceptedAt: now,
				StartedAt:  &now,
				Status:     "running",
				Logs:       taskLogs,
			})
			taskEntry.state.Active["run-task"] = process.Identity{PID: os.Getpid(), Token: "run-task"}
			taskRun := &running{id: "run-task", done: make(chan struct{})}
			taskEntry.running["sample"] = taskRun
			taskEntry.finish(taskRun, stdout, stderr, nil, tc.outcome, finishTarget{
				running:   taskEntry.running,
				key:       "sample",
				pauseTask: "sample",
			})
			taskSaved, err := loadState(taskEntry.root)
			if err != nil {
				t.Fatalf("task state: %v", err)
			}
			gotTask := taskSaved.Runs[0]

			scriptEntry := newEntry(t)
			stdout, stderr, scriptLogs := openLogs(t, scriptEntry, "run-script")
			scriptEntry.state.Scripts = append(scriptEntry.state.Scripts, ScriptRun{
				ID:         "run-script",
				File:       "job.py",
				Command:    []string{"python3", "scripts/job.py"},
				AcceptedAt: now,
				StartedAt:  &now,
				Status:     "running",
				Logs:       scriptLogs,
			})
			scriptEntry.state.Active["run-script"] = process.Identity{PID: os.Getpid(), Token: "run-script"}
			scriptRun := &running{id: "run-script", done: make(chan struct{})}
			scriptEntry.scriptRunning["job.py"] = scriptRun
			scriptEntry.finish(scriptRun, stdout, stderr, nil, tc.outcome, finishTarget{
				running: scriptEntry.scriptRunning,
				key:     "job.py",
			})
			scriptSaved, err := loadState(scriptEntry.root)
			if err != nil {
				t.Fatalf("script state: %v", err)
			}
			gotScript := scriptSaved.Scripts[0]

			var taskReason, scriptReason string
			if gotTask.Reason != nil {
				taskReason = *gotTask.Reason
			}
			if gotScript.Reason != nil {
				scriptReason = *gotScript.Reason
			}
			if gotTask.Status != tc.status || taskReason != tc.reason {
				t.Fatalf("task got (%q, %q), want (%q, %q)", gotTask.Status, taskReason, tc.status, tc.reason)
			}
			if gotScript.Status != gotTask.Status || scriptReason != taskReason {
				t.Fatalf("script got (%q, %q), task got (%q, %q)", gotScript.Status, scriptReason, gotTask.Status, taskReason)
			}
			if taskSaved.Tasks["sample"].Paused != tc.pauses {
				t.Fatalf("task paused = %v, want %v", taskSaved.Tasks["sample"].Paused, tc.pauses)
			}
			if scriptSaved.Tasks["sample"].Paused {
				t.Fatal("script outcome paused a task")
			}
			if len(taskEntry.running) != 0 || len(scriptEntry.scriptRunning) != 0 {
				t.Fatalf("running maps not cleared: task=%d script=%d", len(taskEntry.running), len(scriptEntry.scriptRunning))
			}
		})
	}
}
