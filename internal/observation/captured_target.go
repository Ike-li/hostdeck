package observation

import (
	"os"
	"time"
)

func (s *Store) Root() string { return s.root }
func (s *Store) CaptureTarget(id string) (Target, bool, error) {
	record, err := ReadCapture(s.root, id)
	if os.IsNotExist(err) {
		return Target{}, false, nil
	}
	if err != nil {
		return Target{}, true, err
	}
	target := Target{Identity: record.Identity, State: "unknown", ExitCode: record.ExitCode, Signal: record.Signal, Reason: "captured", SourceID: record.ID}
	if record.State == "exited" {
		target.State = "idle"
	} else if record.State == "running" && record.Identity.Equal(ProcessIdentity(record.Identity.PID)) {
		target.State = "running"
	}
	if record.Error != "" || record.State == "failed" || record.State == "abandoned" {
		target.Reason = "capture_failed"
		target.State = "unknown"
	}
	if record.Completed.IsZero() && (time.Since(record.Updated) > 5*time.Second || !record.Worker.Equal(ProcessIdentity(record.Worker.PID))) {
		target.Reason = "capture_failed"
		target.State = "unknown"
	}
	target.Text, err = CaptureText(s.root, id)
	return target, true, err
}
func (s *Store) FindCapture(identity Identity) (Target, bool, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return Target{}, false, nil
	}
	if err != nil {
		return Target{}, false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		record, err := ReadCapture(s.root, entry.Name())
		if err != nil {
			return Target{}, false, err
		}
		if record.Identity.Equal(identity) {
			return s.CaptureTarget(record.ID)
		}
	}
	return Target{}, false, nil
}
func (s *Store) ApplyCapture(id string, snap *Snapshot) error {
	target, ok, err := s.CaptureTarget(id)
	if err == nil && !ok && snap.CaptureID != "" {
		target, ok, err = s.CaptureTarget(snap.CaptureID)
	}
	if err != nil || !ok {
		return err
	}
	snap.CollectedAt = time.Now().UTC()
	snap.Identity = target.Identity
	snap.State = target.State
	snap.ExitCode = target.ExitCode
	snap.Signal = target.Signal
	snap.Sources = []Source{{ID: target.SourceID, State: target.Reason}}
	return nil
}
func (s *Store) collectCompleted() error {
	owner, err := budgetLock(s.root)
	if err != nil {
		return err
	}
	defer owner.Close()
	return reclaim(s.root, 0)
}

func (s *Store) SessionCaptureTarget(id string) (Target, bool, error) {
	s.mu.Lock()
	v, err := s.load(id)
	linked := ""
	if err == nil {
		linked = v.snap.CaptureID
	}
	s.mu.Unlock()
	if err != nil {
		return Target{}, false, err
	}
	target, ok, err := s.CaptureTarget(id)
	if err == nil && !ok && linked != "" {
		return s.CaptureTarget(linked)
	}
	return target, ok, err
}
