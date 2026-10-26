package diskhistory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

// ErrQuery distinguishes a refused saved-history selection from unreadable data.
var ErrQuery = errors.New("invalid history selection")

type Query struct{ Target, Path, Range, From, To string }
type Target struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Point struct {
	Time      time.Time `json:"time"`
	Bytes     *int64    `json:"bytes"`
	Connected bool      `json:"connected"`
}
type Change struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Bytes *int64    `json:"bytes"`
}
type Directory struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Bytes   *int64 `json:"bytes"`
	Change  *int64 `json:"change"`
	Partial bool   `json:"partial"`
}
type ReportAction struct {
	Item  string      `json:"item"`
	ID    string      `json:"id"`
	Label plugin.Text `json:"label"`
}

// Report is internal console data, not a plugin-program or saved-file contract.
type Report struct {
	Targets    []Target       `json:"targets"`
	Target     string         `json:"target"`
	Path       string         `json:"path"`
	Range      string         `json:"range"`
	Tracking   bool           `json:"tracking"`
	Running    bool           `json:"running"`
	Active     string         `json:"active"`
	Failure    string         `json:"failure"`
	Count      int            `json:"count"`
	Unreadable int            `json:"unreadable"`
	Partial    bool           `json:"partial"`
	Truncated  bool           `json:"truncated"`
	Depth      int            `json:"depth"`
	Old        bool           `json:"old"`
	First      *time.Time     `json:"first"`
	Latest     *time.Time     `json:"latest"`
	From       *time.Time     `json:"from"`
	To         *time.Time     `json:"to"`
	Bytes      *int64         `json:"bytes"`
	Change     *int64         `json:"change"`
	Points     []Point        `json:"points"`
	Periods    []Change       `json:"periods"`
	Children   []Directory    `json:"children"`
	Actions    []ReportAction `json:"actions"`
}

func IsNative(p plugin.Plugin) bool {
	return p.Detail != nil && p.Detail.Exec != nil && slices.Equal(p.Detail.Exec.Args(), []string{"@hostdeck", "disk-history", "detail"})
}

// ReadReport never registers a worker or claims a writer. It uses a live entry
// when the console already owns one, and otherwise reads only the saved files.
func ReadReport(ctx context.Context, dir string, q Query) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Report{}, err
	}
	targets, err := roots(dir)
	if err != nil {
		return Report{}, err
	}
	entries.Lock()
	e := entries.items[dir]
	entries.Unlock()
	if e != nil {
		e.mu.Lock()
		defer e.mu.Unlock()
		info, statErr := os.Stat(dir)
		if statErr == nil && e.identity != nil && os.SameFile(info, e.identity) && !e.retired {
			if e.owner != nil || e.running {
				return e.report(targets, q, time.Now())
			}
		}
	}
	s, err := load(dir)
	if err != nil {
		return Report{}, err
	}
	return (&entry{state: s}).report(targets, q, time.Now())
}

func invalidSelection() error {
	return fmt.Errorf("%w: choose an available target, directory and time range", ErrQuery)
}

func observation(s snapshot, path string) *int64 {
	if m, ok := s.Sizes[path]; ok {
		if m.Partial && m.Missed == "" {
			return nil
		}
		v := m.Bytes
		return &v
	}
	if s.Truncated || s.Sizes["."].Partial {
		return nil
	}
	v := int64(0)
	return &v
}

func knownChange(a, b snapshot, path string) *int64 {
	if d, ok := difference(a, b, path); ok {
		return &d
	}
	return nil
}

func (e *entry) report(targets []string, q Query, now time.Time) (Report, error) {
	r := Report{Tracking: e.state.Tracking, Running: e.running, Active: e.target, Failure: e.state.Error, Targets: []Target{}, Points: []Point{}, Periods: []Change{}, Children: []Directory{}, Actions: []ReportAction{}}
	for _, root := range targets {
		name := root
		if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == root {
			name = "~"
		}
		r.Targets = append(r.Targets, Target{id(root), name})
	}
	index := 0
	if q.Target != "" {
		index = slices.IndexFunc(targets, func(root string) bool { return id(root) == q.Target })
	}
	if index < 0 {
		return r, invalidSelection()
	}
	target := targets[index]
	r.Target = id(target)
	add := func(item, actionID string, label plugin.Text) {
		r.Actions = append(r.Actions, ReportAction{item, actionID, label})
	}
	if e.running {
		add(id(targets[0]), "cancel", text("Cancel scan", "取消扫描"))
	} else {
		add(r.Target, "scan", text("Scan now", "立即扫描"))
	}
	if r.Tracking {
		add(id(targets[0]), "pause", text("Pause hourly tracking", "暂停每小时跟踪"))
	} else if !e.running {
		add(id(targets[0]), "track", text("Start hourly tracking", "开始每小时跟踪"))
	}
	if q.Range == "" {
		q.Range = "24h"
	}
	span, ok := map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}[q.Range]
	if !ok {
		return r, invalidSelection()
	}
	r.Range = q.Range
	if q.Path == "" {
		q.Path = "."
	}
	// The HTTP representation uses slash-separated summary keys on both OSes.
	if strings.Contains(q.Path, "\\") || strings.HasPrefix(q.Path, "/") || filepath.ToSlash(filepath.Clean(filepath.FromSlash(q.Path))) != q.Path {
		return r, invalidSelection()
	}
	for _, part := range strings.Split(q.Path, "/") {
		if part == ".." {
			return r, invalidSelection()
		}
	}
	path := filepath.FromSlash(q.Path)
	r.Path = q.Path
	if path != "." {
		r.Depth = len(strings.Split(q.Path, "/"))
	}
	scans := current(e.state.Scans[target])
	r.Count = len(scans)
	if len(scans) == 0 {
		if path != "." || q.From != "" || q.To != "" {
			return r, invalidSelection()
		}
		return r, nil
	}
	if path != "." && !slices.ContainsFunc(scans, func(s snapshot) bool { _, ok := s.Sizes[path]; return ok }) {
		return r, invalidSelection()
	}
	latest := scans[len(scans)-1]
	r.First = &scans[0].Time
	r.Latest = &latest.Time
	r.Unreadable = latest.Skipped
	r.Truncated = latest.Truncated
	r.Old = r.Tracking && now.Sub(latest.Time) > 2*interval
	r.Bytes = observation(latest, path)
	r.Partial = latest.Sizes[path].Partial
	a, b := 0, len(scans)-1
	cutoff := latest.Time.Add(-span)
	for i, s := range scans {
		if !s.Time.After(cutoff) {
			a = i
		}
	}
	// Keep the curve for the complete chosen range when a bar is selected.
	for i := a; i <= b; i++ {
		connected := false
		if i > a {
			_, connected = difference(scans[i-1], scans[i], path)
		}
		r.Points = append(r.Points, Point{scans[i].Time, observation(scans[i], path), connected})
	}
	bucket := time.Hour
	if q.Range == "7d" {
		bucket = 6 * time.Hour
	}
	if q.Range == "30d" {
		bucket = 24 * time.Hour
	}
	start := a
	for i := a + 1; i <= b; i++ {
		if i < b && scans[i].Time.Truncate(bucket).Equal(scans[i+1].Time.Truncate(bucket)) {
			continue
		}
		r.Periods = append(r.Periods, Change{scans[start].Time, scans[i].Time, knownChange(scans[start], scans[i], path)})
		start = i
	}
	if q.From != "" || q.To != "" {
		rangeStart := a
		from, errA := time.Parse(time.RFC3339Nano, q.From)
		to, errB := time.Parse(time.RFC3339Nano, q.To)
		if errA != nil || errB != nil || !from.Before(to) {
			return r, invalidSelection()
		}
		a = slices.IndexFunc(scans, func(s snapshot) bool { return s.Time.Equal(from) })
		b = slices.IndexFunc(scans, func(s snapshot) bool { return s.Time.Equal(to) })
		if a < rangeStart || b <= a {
			return r, invalidSelection()
		}
	}
	r.From = &scans[a].Time
	r.To = &scans[b].Time
	if a != b {
		r.Change = knownChange(scans[a], scans[b], path)
	}
	names := map[string]bool{}
	for _, s := range []snapshot{scans[a], scans[b]} {
		for name := range s.Sizes {
			if name != "." && filepath.Dir(name) == path {
				names[name] = true
			}
		}
	}
	for name := range names {
		var change *int64
		if a != b {
			change = knownChange(scans[a], scans[b], name)
		}
		r.Children = append(r.Children, Directory{filepath.ToSlash(name), filepath.Base(name), observation(scans[b], name), change, scans[b].Sizes[name].Partial})
	}
	slices.SortFunc(r.Children, func(a, b Directory) int {
		abs := func(v *int64) float64 {
			if v == nil {
				return -1
			}
			return math.Abs(float64(*v))
		}
		if abs(a.Change) > abs(b.Change) {
			return -1
		}
		if abs(a.Change) < abs(b.Change) {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return r, nil
}
