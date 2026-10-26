package tasks

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/process"
)

const maxState = 8 << 20

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type taskState struct {
	Revision       string     `json:"revision"`
	Digest         string     `json:"digest"`
	ScheduleDigest string     `json:"schedule_digest"`
	Paused         bool       `json:"paused"`
	Anchor         *time.Time `json:"anchor_at"`
	Cursor         time.Time  `json:"cursor_at"`
	Missed         uint64     `json:"missed"`
	MissedFirst    *time.Time `json:"missed_first"`
	MissedLast     *time.Time `json:"missed_last"`
}
type logs struct {
	Stdout          *string `json:"stdout"`
	Stderr          *string `json:"stderr"`
	StdoutTruncated bool    `json:"stdout_truncated"`
	StderrTruncated bool    `json:"stderr_truncated"`
}
type Record struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	Revision    string     `json:"revision"`
	Trigger     string     `json:"trigger"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	AcceptedAt  time.Time  `json:"accepted_at"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Status      string     `json:"status"`
	ExitCode    *int       `json:"exit_code"`
	Reason      *string    `json:"reason"`
	Logs        logs       `json:"logs"`
	Diagnostic  string     `json:"diagnostic,omitempty"`
}
type saved struct {
	Version int                         `json:"version"`
	Tasks   map[string]taskState        `json:"tasks"`
	Runs    []Record                    `json:"runs"`
	Active  map[string]process.Identity `json:"active"`
}

func emptyState() saved {
	return saved{Version: 1, Tasks: map[string]taskState{}, Runs: []Record{}, Active: map[string]process.Identity{}}
}
func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func str(s string) *string       { return &s }
func activeStatus(s string) bool { return s == "starting" || s == "running" }

func loadState(root *os.Root) (saved, error) {
	s := emptyState()
	f, err := root.Open("data/state.json")
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxState+1))
	if err != nil {
		return s, err
	}
	if len(b) > maxState {
		return s, errors.New("task state exceeds 8 MiB")
	}
	if err := strictJSON(b, &s, true); err != nil {
		return s, err
	}
	if err := stateShape(b); err != nil {
		return s, err
	}
	if s.Version != 1 || s.Tasks == nil || s.Runs == nil || s.Active == nil || len(s.Tasks) > 68 || len(s.Runs) > 1004 || len(s.Active) > 4 {
		return s, errors.New("invalid task state version or collections")
	}
	ids := map[string]Record{}
	for _, r := range s.Runs {
		if !taskID.MatchString(r.TaskID) || !runIDPattern.MatchString(r.ID) || ids[r.ID].ID != "" || r.AcceptedAt.IsZero() || strings.TrimSpace(r.Revision) == "" || len(r.Revision) > 128 || len(r.Diagnostic) > 1024 {
			return s, errors.New("invalid run identity or time")
		}
		if r.Trigger != "manual" && r.Trigger != "scheduled" {
			return s, errors.New("invalid run trigger")
		}
		if (r.Trigger == "scheduled") != (r.ScheduledAt != nil) {
			return s, errors.New("invalid scheduled time")
		}
		switch r.Status {
		case "starting", "running", "succeeded", "failed", "timed_out", "cancelled", "interrupted", "skipped":
		default:
			return s, errors.New("invalid run status")
		}
		if activeStatus(r.Status) && r.FinishedAt != nil || !activeStatus(r.Status) && r.FinishedAt == nil {
			return s, errors.New("invalid run completion")
		}
		if r.Status == "running" && r.StartedAt == nil || r.Status == "starting" && r.StartedAt != nil {
			return s, errors.New("invalid run start")
		}
		if activeStatus(r.Status) || r.Status == "succeeded" {
			if r.Reason != nil {
				return s, errors.New("unexpected run reason")
			}
		} else {
			allowed := map[string][]string{"failed": {"exit", "start", "environment", "log_io"}, "timed_out": {"timeout"}, "cancelled": {"stop", "task_removed", "plugin_disabled", "shutdown"}, "interrupted": {"recovery", "cleanup"}, "skipped": {"overlap", "capacity"}}
			if r.Reason == nil || !slices.Contains(allowed[r.Status], *r.Reason) {
				return s, errors.New("invalid run reason")
			}
		}
		if r.Status == "succeeded" && (r.StartedAt == nil || r.ExitCode == nil || *r.ExitCode != 0) {
			return s, errors.New("invalid successful result")
		}
		if r.Reason != nil && *r.Reason == "exit" && (r.ExitCode == nil || *r.ExitCode == 0) {
			return s, errors.New("invalid exit failure")
		}
		if r.Status == "skipped" && (r.StartedAt != nil || r.ExitCode != nil || r.Logs.Stdout != nil || r.Logs.Stderr != nil) {
			return s, errors.New("skipped run has process output")
		}
		for i, p := range []*string{r.Logs.Stdout, r.Logs.Stderr} {
			suffix := []string{".stdout.log", ".stderr.log"}[i]
			if p != nil && *p != "data/logs/"+r.ID+suffix {
				return s, errors.New("invalid run log path")
			}
		}
		ids[r.ID] = r
	}
	for id, identity := range s.Active {
		r := ids[id]
		if r.ID == "" || identity.PID < 0 || identity.Token != id || s.Tasks[r.TaskID].Revision == "" || (!activeStatus(r.Status) && r.Status != "interrupted") || (r.Status == "running" && identity.PID == 0) {
			return s, errors.New("invalid active worker")
		}
	}
	for _, r := range s.Runs {
		if activeStatus(r.Status) && s.Active[r.ID].Token == "" {
			return s, errors.New("active run has no worker identity")
		}
	}
	for id, ts := range s.Tasks {
		if !taskID.MatchString(id) || ts.Revision == "" || len(ts.Digest) != 64 || len(ts.ScheduleDigest) != 64 {
			return s, errors.New("invalid runtime task")
		}
		if _, err := hex.DecodeString(ts.Digest + ts.ScheduleDigest); err != nil {
			return s, errors.New("invalid task digest")
		}
	}
	return s, nil
}

func stateShape(b []byte) error {
	object := func(raw json.RawMessage, required, nullable []string) (map[string]json.RawMessage, error) {
		var o map[string]json.RawMessage
		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, err
		}
		for _, key := range required {
			v, ok := o[key]
			if !ok || (!slices.Contains(nullable, key) && string(v) == "null") {
				return nil, fmt.Errorf("missing or null state field %s", key)
			}
		}
		return o, nil
	}
	root, err := object(b, []string{"version", "tasks", "runs", "active"}, nil)
	if err != nil {
		return err
	}
	var tasks map[string]json.RawMessage
	json.Unmarshal(root["tasks"], &tasks)
	for _, raw := range tasks {
		if _, err := object(raw, []string{"revision", "digest", "schedule_digest", "paused", "anchor_at", "cursor_at", "missed", "missed_first", "missed_last"}, []string{"anchor_at", "missed_first", "missed_last"}); err != nil {
			return err
		}
	}
	var records []json.RawMessage
	json.Unmarshal(root["runs"], &records)
	for _, raw := range records {
		o, err := object(raw, []string{"id", "task_id", "revision", "trigger", "scheduled_at", "accepted_at", "started_at", "finished_at", "status", "exit_code", "reason", "logs"}, []string{"scheduled_at", "started_at", "finished_at", "exit_code", "reason"})
		if err != nil {
			return err
		}
		if _, err := object(o["logs"], []string{"stdout", "stderr", "stdout_truncated", "stderr_truncated"}, []string{"stdout", "stderr"}); err != nil {
			return err
		}
	}
	var active map[string]json.RawMessage
	json.Unmarshal(root["active"], &active)
	for _, raw := range active {
		if _, err := object(raw, []string{"pid", "token"}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (e *entry) saveLocked() error {
	if e.retired || e.owner == nil {
		return errors.New("task plugin is suspended")
	}
	if e.stateErr != nil {
		return e.stateErr
	}
	if _, err := loadState(e.root); err != nil {
		return fmt.Errorf("refusing to overwrite invalid durable task state: %w", err)
	}
	info, err := os.Stat(e.dir)
	if err != nil || !os.SameFile(info, e.identity) {
		return errors.New("installed task folder changed")
	}
	b, err := json.Marshal(e.state)
	if err != nil {
		return err
	}
	if len(b) > maxState {
		return errors.New("task state exceeds 8 MiB")
	}
	if err := e.root.MkdirAll("data", 0700); err != nil {
		return err
	}
	f, err := e.root.OpenFile("data/state.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, fs.ErrExist) {
		if err = e.root.Remove("data/state.tmp"); err == nil {
			f, err = e.root.OpenFile("data/state.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		}
	}
	if err != nil {
		return err
	}
	defer e.root.Remove("data/state.tmp")
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	until := time.Now().Add(2 * time.Second)
	for {
		err = e.root.Rename("data/state.tmp", "data/state.json")
		if err == nil || !errors.Is(err, fs.ErrPermission) || time.Now().After(until) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *entry) claimLocked() error {
	if e.owner != nil {
		return nil
	}
	if e.stateErr != nil {
		return e.stateErr
	}
	if err := e.root.MkdirAll("data", 0700); err != nil {
		return err
	}
	f, err := e.root.OpenFile("data/owner.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = lock(f); err != nil {
		f.Close()
		return fmt.Errorf("another console owns these tasks: %w", err)
	}
	s, err := loadState(e.root)
	if err != nil {
		f.Close()
		e.stateErr = err
		return err
	}
	e.owner = f
	e.state = s
	return e.recoverLocked()
}

func (e *entry) recoverLocked() error {
	changed := false
	for i := range e.state.Runs {
		r := &e.state.Runs[i]
		if !activeStatus(r.Status) {
			continue
		}
		now := time.Now().UTC()
		r.Status = "interrupted"
		r.Reason = str("recovery")
		r.FinishedAt = &now
		ts := e.state.Tasks[r.TaskID]
		ts.Paused = true
		e.state.Tasks[r.TaskID] = ts
		changed = true
	}
	for id, identity := range e.state.Active {
		if identity.PID == 0 || process.Gone(identity) {
			delete(e.state.Active, id)
			changed = true
		}
	}
	if changed {
		if err := e.saveLocked(); err != nil {
			e.stateErr = err
			return err
		}
	}
	return nil
}

// pruneLocked reserves two capped streams for each active or accepted run.
func (e *entry) pruneLocked(reserve int) error {
	if err := e.sweepLogsLocked(); err != nil {
		return err
	}
	previous := e.state
	e.state.Tasks = maps.Clone(e.state.Tasks)
	now := time.Now().UTC()
	terminal := 0
	changed := false
	remove := []string{}
	for _, r := range e.state.Runs {
		if !activeStatus(r.Status) {
			terminal++
		}
	}
	kept := make([]Record, 0, len(e.state.Runs))
	for _, r := range e.state.Runs {
		expired := !activeStatus(r.Status) && e.state.Active[r.ID].Token == "" && (terminal > 1000-reserve || r.AcceptedAt.Before(now.Add(-30*24*time.Hour)))
		if expired {
			for _, p := range []*string{r.Logs.Stdout, r.Logs.Stderr} {
				if p != nil {
					remove = append(remove, *p)
				}
			}
			terminal--
			changed = true
			continue
		}
		kept = append(kept, r)
	}
	e.state.Runs = kept
	var used int64
	for i := range e.state.Runs {
		r := &e.state.Runs[i]
		if e.state.Active[r.ID].Token != "" {
			continue
		}
		for _, p := range []**string{&r.Logs.Stdout, &r.Logs.Stderr} {
			if *p == nil {
				continue
			}
			info, err := e.root.Stat(**p)
			if errors.Is(err, fs.ErrNotExist) {
				*p = nil
				changed = true
				continue
			}
			if err != nil {
				e.state = previous
				return err
			}
			used += info.Size()
		}
	}
	budget := int64(64<<20) - int64(len(e.state.Active)+reserve)*(2<<20)
	for i := range e.state.Runs {
		if used <= budget {
			break
		}
		r := &e.state.Runs[i]
		if e.state.Active[r.ID].Token != "" {
			continue
		}
		for _, p := range []**string{&r.Logs.Stdout, &r.Logs.Stderr} {
			if *p == nil {
				continue
			}
			info, err := e.root.Stat(**p)
			if err != nil {
				e.state = previous
				return err
			}
			used -= info.Size()
			remove = append(remove, **p)
			*p = nil
			changed = true
		}
	}
	if used > budget {
		e.state = previous
		return errors.New("task log storage is full")
	}
	current := map[string]bool{}
	for _, t := range e.cfg.Tasks {
		current[t.ID] = true
	}
	for _, r := range e.state.Runs {
		if e.state.Active[r.ID].Token != "" {
			current[r.TaskID] = true
		}
	}
	for id := range e.state.Tasks {
		if !current[id] {
			delete(e.state.Tasks, id)
			changed = true
		}
	}
	if changed {
		if err := e.saveLocked(); err != nil {
			e.state = previous
			return err
		}
	}
	// Publish expired references before deleting their files. A refused state
	// write must not erase logs still referenced by the durable prior history.
	for _, path := range remove {
		if err := e.root.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (e *entry) sweepLogsLocked() error {
	dir, err := e.root.Open("data/logs")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	files, err := dir.ReadDir(2017)
	if err != nil && err != io.EOF {
		return err
	}
	if len(files) >= 2017 {
		return errors.New("task log file count exceeds its bound")
	}
	referenced := map[string]bool{}
	for _, r := range e.state.Runs {
		for _, p := range []*string{r.Logs.Stdout, r.Logs.Stderr} {
			if p != nil {
				referenced[*p] = true
			}
		}
	}
	for _, file := range files {
		path := "data/logs/" + file.Name()
		id := strings.TrimSuffix(strings.TrimSuffix(file.Name(), ".stdout.log"), ".stderr.log")
		if !runIDPattern.MatchString(id) || id == file.Name() || !file.Type().IsRegular() {
			return errors.New("unexpected file in task logs; inspect it locally")
		}
		if !referenced[path] {
			if err := e.root.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *entry) record(id string) *Record {
	for i := range e.state.Runs {
		if e.state.Runs[i].ID == id {
			return &e.state.Runs[i]
		}
	}
	return nil
}
func localDir(dir, work string) string {
	if filepath.IsAbs(work) {
		return work
	}
	return filepath.Join(dir, work)
}
