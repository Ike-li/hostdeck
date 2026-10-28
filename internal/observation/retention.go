package observation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var ErrCapacity = errors.New("observation capacity is full; active captures were retained")

const storageBudget int64 = 128 << 20

// Reserve both bounded log segments and worst-case escaped observation JSON,
// plus temporary metadata, before stopping an old service.
const captureReservation int64 = 4<<20 + 12*FragmentLimit + 128<<10

func budgetLock(root string) (*os.File, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, "budget.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Second)
	for {
		err = storageLock(f)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("observation storage is busy: %w", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return f, nil
}

type storedRecord struct {
	id, service     string
	completed       time.Time
	size            int64
	capture, active bool
}

func directorySize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("unexpected observation storage entry")
		}
		total += info.Size()
		return nil
	})
	return total, err
}
func reclaim(root string, reserve int64) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	records := map[string]*storedRecord{}
	snapshots := map[string]Snapshot{}
	snapshotSizes := map[string]int64{}
	var other int64
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			if !validID(name) {
				return errors.New("unexpected observation folder")
			}
			capture, err := ReadCapture(root, name)
			if err != nil {
				return err
			}
			size, err := directorySize(filepath.Join(root, name))
			if err != nil {
				return err
			}
			records[name] = &storedRecord{id: name, service: capture.Service, completed: capture.Completed, size: size, capture: true, active: capture.Completed.IsZero()}
			continue
		}
		if strings.HasSuffix(name, ".json") && validID(strings.TrimSuffix(name, ".json")) {
			id := strings.TrimSuffix(name, ".json")
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				return err
			}
			var snap Snapshot
			if len(data) > 4<<20 || json.Unmarshal(data, &snap) != nil || snap.ID != id || (snap.Schema != 0 && snap.Schema != 1) || snap.OperationTime.IsZero() {
				return errors.New("corrupt observation record")
			}
			snapshots[id] = snap
			snapshotSizes[id] = int64(len(data))
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		other += info.Size()
	}
	for id, snap := range snapshots {
		r := records[id]
		if r == nil {
			r = &storedRecord{id: id, service: snap.Plugin + "/" + snap.Item, completed: snap.OperationTime}
			records[id] = r
		}
		r.size += snapshotSizes[id]
	}
	sorted := []*storedRecord{}
	var total = other
	for _, r := range records {
		if r.active {
			total += max(r.size, captureReservation)
		} else {
			total += r.size
			sorted = append(sorted, r)
		}
	}
	slices.SortFunc(sorted, func(a, b *storedRecord) int { return b.completed.Compare(a.completed) })
	counts := map[string]int{}
	remove := func(r *storedRecord) error {
		// A collector can publish completion just before releasing its owner lock.
		var owner *os.File
		if r.capture {
			var err error
			owner, err = os.OpenFile(filepath.Join(root, r.id, "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				return err
			}
			if err = storageLock(owner); err != nil {
				owner.Close()
				return nil
			}
		}
		if owner != nil {
			owner.Close()
		}
		if err := os.Remove(filepath.Join(root, r.id+".json")); err != nil && !os.IsNotExist(err) {
			return err
		}
		if r.capture {
			if err := os.RemoveAll(filepath.Join(root, r.id)); err != nil {
				return err
			}
		}
		total -= r.size
		r.size = 0
		return nil
	}
	for _, r := range sorted {
		counts[r.service]++
		if counts[r.service] > 3 || time.Since(r.completed) > 7*24*time.Hour {
			if err := remove(r); err != nil {
				return err
			}
		}
	}
	for i := len(sorted) - 1; i >= 0 && total+reserve > storageBudget; i-- {
		if sorted[i].size > 0 {
			if err := remove(sorted[i]); err != nil {
				return err
			}
		}
	}
	if total+reserve > storageBudget {
		return ErrCapacity
	}
	return nil
}
func reserveCapture(root, id, service string) error {
	owner, err := budgetLock(root)
	if err != nil {
		return err
	}
	defer owner.Close()
	if err = reclaim(root, captureReservation); err != nil {
		return err
	}
	dir := filepath.Join(root, id)
	if err = os.Mkdir(dir, 0700); err != nil {
		return err
	}
	now := time.Now().UTC()
	record := Capture{Schema: 1, ID: id, Service: serviceKeyFromInput(service), State: "prepared", Started: now, Updated: now}
	if err = saveCapture(root, record); err != nil {
		os.RemoveAll(dir)
		return err
	}
	return nil
}

func serviceKeyFromInput(service string) string {
	if len(service) == 64 {
		return service
	}
	sum := sha256.Sum256([]byte(service))
	return hex.EncodeToString(sum[:])
}

// Completion maintenance must never recreate a folder removed by its owner.
func cleanupExisting(root string) {
	owner, err := os.OpenFile(filepath.Join(root, "budget.lock"), os.O_RDWR, 0600)
	if err != nil {
		return
	}
	defer owner.Close()
	if storageLock(owner) != nil {
		return
	}
	_ = reclaim(root, 0)
}
