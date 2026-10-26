package tasks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

type clock struct {
	mu        sync.Mutex
	now       time.Time
	callbacks map[int]func()
	next      int
}

func newClock(s string) *clock {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &clock{now: v, callbacks: map[int]func(){}}
}
func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Every(_ time.Duration, callback func()) func() {
	c.mu.Lock()
	id := c.next
	c.next++
	c.callbacks[id] = callback
	c.mu.Unlock()
	return func() { c.mu.Lock(); delete(c.callbacks, id); c.mu.Unlock() }
}
func (c *clock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	var calls []func()
	for _, f := range c.callbacks {
		calls = append(calls, f)
	}
	c.mu.Unlock()
	for _, f := range calls {
		f()
	}
}
func (c *clock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }
func lastRecord(t *testing.T, dir string, n int, status string) map[string]any {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for {
		s := readState(t, dir)
		records := s["runs"].([]any)
		if len(records) == n && n > 0 {
			r := records[n-1].(map[string]any)
			if r["status"] == status {
				return r
			}
		}
		if time.Now().After(until) {
			t.Fatalf("wanted %d records ending in %s: %+v", n, status, s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func changeSchedule(t *testing.T, dir string, p plugin.Plugin, schedule map[string]any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	json.Unmarshal(b, &c)
	c["tasks"].([]any)[0].(map[string]any)["schedule"] = schedule
	b, _ = json.Marshal(c)
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
}

func TestAnIntervalRunsAtItsAnchorAndReadsNeverTriggerIt(t *testing.T) {
	c := newClock("2026-01-01T00:00:00Z")
	dir, _ := fixture(t, "success", "5s", tasks.WithClock(c))
	before, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
	operation(t, dir, "card")
	operation(t, dir, "detail")
	after, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
	if string(before) != string(after) {
		t.Fatal("read wrote state")
	}
	c.Advance(59 * time.Second)
	if len(readState(t, dir)["runs"].([]any)) != 0 {
		t.Fatal("interval fired early")
	}
	c.Advance(time.Second)
	r := lastRecord(t, dir, 1, "succeeded")
	if r["trigger"] != "scheduled" || r["scheduled_at"] != "2026-01-01T00:01:00Z" {
		t.Fatalf("scheduled record %+v", r)
	}
	c.Advance(30 * time.Second)
	if len(readState(t, dir)["runs"].([]any)) != 1 {
		t.Fatal("interval depends on reads or completion")
	}
}

func TestClockJumpsCollapseOldOccurrencesAndRefuseSixtySecondLateness(t *testing.T) {
	c := newClock("2026-01-01T00:00:00Z")
	dir, p := fixture(t, "success", "5s", tasks.WithClock(c))
	c.Advance(2*time.Minute + 59*time.Second)
	r := lastRecord(t, dir, 1, "succeeded")
	if r["scheduled_at"] != "2026-01-01T00:02:00Z" {
		t.Fatalf("latest %+v", r)
	}
	if readState(t, dir)["tasks"].(map[string]any)["sample"].(map[string]any)["missed"] != float64(1) {
		t.Fatal("missed occurrence was not counted")
	}
	changeSchedule(t, dir, p, map[string]any{"every": "5m"})
	c.Advance(6 * time.Minute)
	if len(readState(t, dir)["runs"].([]any)) != 1 {
		t.Fatal("exactly sixty seconds late still ran")
	}
}

func TestPauseResumeAndRestartKeepTheIntervalWithoutReplayingMissedRuns(t *testing.T) {
	c := newClock("2026-01-01T00:00:00Z")
	dir, p := fixture(t, "success", "5s", tasks.WithClock(c))
	if v := operation(t, dir, "action", "pause", "sample"); v["error"] != nil {
		t.Fatalf("pause %+v", v)
	}
	c.Advance(10 * time.Minute)
	if len(readState(t, dir)["runs"].([]any)) != 0 {
		t.Fatal("paused task ran")
	}
	if v := operation(t, dir, "action", "resume", "sample"); v["error"] != nil {
		t.Fatalf("resume %+v", v)
	}
	if len(readState(t, dir)["runs"].([]any)) != 0 {
		t.Fatal("Resume replayed a task")
	}
	c.Advance(time.Minute)
	lastRecord(t, dir, 1, "succeeded")
	tasks.Close(context.Background(), filepath.Dir(dir))
	c.Advance(10 * time.Minute)
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}, tasks.WithClock(c)); err != nil {
		t.Fatal(err)
	}
	if len(readState(t, dir)["runs"].([]any)) != 1 {
		t.Fatal("restart replayed missed work")
	}
	c.Advance(time.Minute)
	r := lastRecord(t, dir, 2, "succeeded")
	if r["scheduled_at"] != "2026-01-01T00:22:00Z" {
		t.Fatalf("anchor moved: %+v", r)
	}
	c.Set(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c.Set(time.Date(2026, 1, 1, 0, 22, 30, 0, time.UTC))
	if len(readState(t, dir)["runs"].([]any)) != 2 {
		t.Fatal("clock rewind replayed work")
	}
}

func TestCronSkipsMissingLocalMinutesAndTheSecondCopyOfARepeatedMinute(t *testing.T) {
	t.Run("gap", func(t *testing.T) {
		c := newClock("2026-03-29T00:59:00Z")
		dir, p := fixture(t, "success", "5s", tasks.WithClock(c))
		changeSchedule(t, dir, p, map[string]any{"cron": "30 2 * * *", "timezone": "Europe/Berlin"})
		b, _ := json.Marshal(operation(t, dir, "detail"))
		if !strings.Contains(string(b), "2026-03-30T00:30:00Z") {
			t.Fatalf("gap next reading %s", b)
		}
	})
	t.Run("fold", func(t *testing.T) {
		c := newClock("2026-10-25T00:00:00Z")
		dir, p := fixture(t, "success", "5s", tasks.WithClock(c))
		changeSchedule(t, dir, p, map[string]any{"cron": "30 2 * * *", "timezone": "Europe/Berlin"})
		c.Advance(30 * time.Minute)
		lastRecord(t, dir, 1, "succeeded")
		c.Advance(time.Hour)
		if len(readState(t, dir)["runs"].([]any)) != 1 {
			t.Fatal("repeated local minute ran twice")
		}
	})
}

func TestScheduledRunsShareFourSlotsAndRecordOverlapWithoutStartingMoreWorkers(t *testing.T) {
	c := newClock("2026-01-01T00:00:00Z")
	dir, p := fixture(t, "block", "30s", tasks.WithClock(c))
	b, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	json.Unmarshal(b, &config)
	seed := config["tasks"].([]any)[0]
	var list []any
	for i := 0; i < 5; i++ {
		b, _ := json.Marshal(seed)
		var task map[string]any
		json.Unmarshal(b, &task)
		task["id"] = fmt.Sprintf("task-%d", i)
		list = append(list, task)
	}
	config["tasks"] = list
	b, _ = json.Marshal(config)
	os.WriteFile(filepath.Join(dir, "tasks.json"), b, 0600)
	tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p})
	c.Advance(time.Minute)
	s := readState(t, dir)
	if len(s["active"].(map[string]any)) != 4 || len(s["runs"].([]any)) != 5 {
		t.Fatalf("slots %+v", s)
	}
	last := s["runs"].([]any)[4].(map[string]any)
	if last["status"] != "skipped" || last["reason"] != "capacity" {
		t.Fatalf("capacity %+v", last)
	}
	c.Advance(time.Minute)
	s = readState(t, dir)
	records := s["runs"].([]any)
	if len(records) != 10 || len(s["active"].(map[string]any)) != 4 {
		t.Fatal("overlap started extra workers")
	}
	for _, raw := range records[5:9] {
		r := raw.(map[string]any)
		if r["status"] != "skipped" || r["reason"] != "overlap" {
			t.Fatalf("overlap %+v", r)
		}
	}
}

func TestAThousandYearClockJumpStillSelectsTheLatestIntervalWithoutDurationOverflow(t *testing.T) {
	c := newClock("2026-01-01T00:00:00Z")
	start := c.Now()
	dir, _ := fixture(t, "success", "5s", tasks.WithClock(c))
	future := start.AddDate(1000, 0, 0)
	c.Set(future)
	if len(readState(t, dir)["runs"].([]any)) != 1 {
		t.Fatal("duration overflow lost the latest due interval")
	}
	r := lastRecord(t, dir, 1, "succeeded")
	if r["scheduled_at"] != future.Format(time.RFC3339) {
		t.Fatalf("wrong latest %+v", r)
	}
	want := (future.Unix()-start.Unix())/60 - 1
	got := readState(t, dir)["tasks"].(map[string]any)["sample"].(map[string]any)["missed"]
	if got != float64(want) {
		t.Fatalf("missed %v want %d", got, want)
	}
}

func TestPauseLetsAnActiveRunFinishAndStopDoesNotPauseFutureTriggers(t *testing.T) {
	c := newClock("2026-01-01T00:00:00Z")
	dir, _ := fixture(t, "block", "30s", tasks.WithClock(c))
	c.Advance(time.Minute)
	r := lastRecord(t, dir, 1, "running")
	if v := operation(t, dir, "action", "pause", "sample"); v["error"] != nil {
		t.Fatalf("pause %+v", v)
	}
	if len(readState(t, dir)["active"].(map[string]any)) != 1 {
		t.Fatal("Pause cancelled the active run")
	}
	operation(t, dir, "action", "resume", "sample")
	operation(t, dir, "action", "stop:"+r["id"].(string), "sample")
	lastRecord(t, dir, 1, "cancelled")
	c.Advance(time.Minute)
	lastRecord(t, dir, 2, "running")
}

func TestPausedTasksStillExpireTheirHistoryWithoutStartingAnotherRun(t *testing.T) {
	c := newClock("2026-01-01T00:00:00Z")
	dir, _ := fixture(t, "success", "5s", tasks.WithClock(c))
	c.Advance(time.Minute)
	lastRecord(t, dir, 1, "succeeded")
	operation(t, dir, "action", "pause", "sample")
	c.Advance(31 * 24 * time.Hour)
	if len(readState(t, dir)["runs"].([]any)) != 0 {
		t.Fatal("paused history was never expired")
	}
}
