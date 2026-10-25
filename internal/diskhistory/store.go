// Package diskhistory implements the native disk-history plugin. It stores
// observations inside the installed plugin and never launches a program.
package diskhistory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	maxState  = 64 << 20
	maxScans  = 744
	maxDirs   = 256
	retention = 30 * 24 * time.Hour
	interval  = time.Hour
)

type measurement struct {
	Bytes   int64 `json:"bytes"`
	Partial bool  `json:"partial,omitempty"`
	// Missed is a digest of the unreadable entries below a partial directory,
	// so two readings that missed the same ones can compare what they read.
	// It is empty when the directory itself could not be read.
	Missed string `json:"missed,omitempty"`
}

type snapshot struct {
	Time      time.Time              `json:"time"`
	Sizes     map[string]measurement `json:"sizes"`
	Skipped   int                    `json:"unreadable,omitempty"`
	Truncated bool                   `json:"truncated,omitempty"`
	// Allocated readings count disk space. Earlier readings counted file
	// lengths, and the two are never compared.
	Allocated bool `json:"allocated,omitempty"`
}

type saved struct {
	Version  int                   `json:"version"`
	Tracking bool                  `json:"tracking"`
	Scans    map[string][]snapshot `json:"scans"`
	Error    string                `json:"error,omitempty"`
}

func readJSON(dir, name string, limit int64, value any) error {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := r.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	return json.Unmarshal(data, value)
}

func load(dir string) (saved, error) {
	var s saved
	err := readJSON(dir, "data/history.json", maxState, &s)
	if errors.Is(err, fs.ErrNotExist) {
		return saved{Version: 1, Scans: map[string][]snapshot{}}, nil
	}
	if err != nil {
		return saved{}, err
	}
	if s.Version != 1 || s.Scans == nil {
		return saved{}, errors.New("invalid disk history version or scans")
	}
	for root, scans := range s.Scans {
		if !filepath.IsAbs(root) || len(scans) > maxScans {
			return saved{}, errors.New("invalid disk history root or scan count")
		}
		for _, scan := range scans {
			if scan.Time.IsZero() || len(scan.Sizes) > maxDirs {
				return saved{}, errors.New("invalid disk history snapshot")
			}
			if _, ok := scan.Sizes["."]; !ok {
				return saved{}, errors.New("disk history has no root total")
			}
			for name, m := range scan.Sizes {
				if !filepath.IsLocal(name) || m.Bytes < 0 {
					return saved{}, errors.New("invalid disk history measurement")
				}
			}
		}
	}
	return s, nil
}

func save(dir string, s saved) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(data) > maxState {
		return errors.New("disk history exceeds its 64 MiB storage limit")
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.MkdirAll("data", 0700); err != nil {
		return err
	}
	f, err := r.OpenFile("data/history.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, fs.ErrExist) {
		// A previous interrupted writer may leave only this unpublished file.
		if err := r.Remove("data/history.tmp"); err != nil {
			return err
		}
		f, err = r.OpenFile("data/history.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	if err != nil {
		return err
	}
	defer r.Remove("data/history.tmp")
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = r.Rename("data/history.tmp", "data/history.json")
		if err == nil || !errors.Is(err, fs.ErrPermission) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func roots(dir string) ([]string, error) {
	var config struct {
		Roots []string `json:"roots"`
	}
	err := readJSON(dir, "config.json", 64<<10, &config)
	if errors.Is(err, fs.ErrNotExist) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		config.Roots = []string{home}
	} else if err != nil {
		return nil, err
	}
	if len(config.Roots) == 0 || len(config.Roots) > 8 {
		return nil, errors.New("config.json needs 1 to 8 absolute roots")
	}
	seen := map[string]bool{}
	var result []string
	for _, path := range config.Roots {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("scan root must be absolute: %s", path)
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	return result, nil
}
