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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/process"
)

const maxState = 8 << 20

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
	if s.Version != 1 || s.Tasks == nil || s.Runs == nil || s.Active == nil || len(s.Tasks) > 64 || len(s.Runs) > 1004 || len(s.Active) > 4 {
		return s, errors.New("invalid task state version or collections")
	}
	ids := map[string]Record{}
	for _, r := range s.Runs {
		if !taskID.MatchString(r.TaskID) || r.ID == "" || len(r.ID) > 128 || strings.ContainsAny(r.ID, "/\\.:\x00") || ids[r.ID].ID != "" || r.AcceptedAt.IsZero() || r.Revision == "" {
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
		for i, p := range []*string{r.Logs.Stdout, r.Logs.Stderr} {
			suffix := []string{".stdout.log", ".stderr.log"}[i]
			if p != nil && *p != "data/logs/"+r.ID+suffix {
				return s, errors.New("invalid run log path")
			}
		}
		ids[r.ID] = r
	}
	for id, identity := range s.Active {
		if ids[id].ID == "" || identity.PID < 0 || identity.Token != id {
			return s, errors.New("invalid active worker")
		}
	}
	for id, ts := range s.Tasks {
		if !taskID.MatchString(id) || ts.Revision == "" || len(ts.Digest) != 64 || len(ts.ScheduleDigest) != 64 {
			return s, errors.New("invalid runtime task")
		}
	}
	return s, nil
}

func (e *entry) saveLocked() error {
	if e.retired {
		return errors.New("task plugin is suspended")
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
	}
	return nil
}

// pruneLocked reserves two capped streams for each active or accepted run.
func (e *entry) pruneLocked(reserve int) error {
	now := time.Now().UTC()
	terminal := 0
	for _, r := range e.state.Runs {
		if !activeStatus(r.Status) {
			terminal++
		}
	}
	kept := make([]Record, 0, len(e.state.Runs))
	for _, r := range e.state.Runs {
		remove := !activeStatus(r.Status) && e.state.Active[r.ID].Token == "" && (terminal >= 1000 || r.AcceptedAt.Before(now.Add(-30*24*time.Hour)))
		if remove {
			for _, p := range []*string{r.Logs.Stdout, r.Logs.Stderr} {
				if p != nil {
					if err := e.root.Remove(*p); err != nil && !errors.Is(err, fs.ErrNotExist) {
						return err
					}
				}
			}
			terminal--
			continue
		}
		kept = append(kept, r)
	}
	e.state.Runs = kept
	var used int64
	for _, r := range e.state.Runs {
		if activeStatus(r.Status) {
			continue
		}
		for _, p := range []*string{r.Logs.Stdout, r.Logs.Stderr} {
			if p != nil {
				if st, err := e.root.Stat(*p); err == nil {
					used += st.Size()
				} else if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
		}
	}
	budget := int64(64<<20) - int64(len(e.running)+reserve)*(2<<20)
	for i := range e.state.Runs {
		if used <= budget {
			break
		}
		r := &e.state.Runs[i]
		if activeStatus(r.Status) || e.state.Active[r.ID].Token != "" {
			continue
		}
		for _, p := range []**string{&r.Logs.Stdout, &r.Logs.Stderr} {
			if *p != nil {
				st, err := e.root.Stat(**p)
				if err == nil {
					if err = e.root.Remove(**p); err != nil {
						return err
					}
					used -= st.Size()
				} else if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				*p = nil
			}
		}
	}
	if used > budget {
		return errors.New("task log storage is full")
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
