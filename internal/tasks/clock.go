package tasks

import (
	"sync"
	"time"
)

// Clock is the scheduling boundary. Execution deadlines retain real time.
type Clock interface {
	Now() time.Time
	Every(time.Duration, func()) func()
}
type Option func(*entry)

func WithClock(clock Clock) Option {
	return func(e *entry) {
		if clock != nil {
			e.clock = clock
		}
	}
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }
func (realClock) Every(d time.Duration, tick func()) func() {
	ticker := time.NewTicker(d)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case <-ticker.C:
				tick()
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { ticker.Stop(); close(done) }) }
}
func (e *entry) now() time.Time { return e.clock.Now().UTC() }

func (e *entry) initializeScheduleLocked() error {
	e.next = map[string]time.Time{}
	needed := len(e.state.Tasks) > 0 || len(e.state.Runs) > 0
	for _, t := range e.cfg.Tasks {
		if *t.Enabled && !e.state.Tasks[t.ID].Paused {
			needed = true
		}
	}
	if !needed {
		return nil
	}
	if err := e.claimLocked(); err != nil {
		e.ownerErr = err
		return err
	}
	if err := e.pruneLocked(0); err != nil {
		return err
	}
	now := e.now()
	for _, t := range e.cfg.Tasks {
		ts := e.runtimeLocked(t)
		if ts.Eligible && !ts.Paused && !ts.Cursor.IsZero() {
			addMissed(&ts, t.Schedule.batch(ts.Cursor, now, ts.Anchor))
		}
		ts.Eligible = *t.Enabled && !ts.Paused
		if ts.Eligible && t.Schedule.Every != nil && ts.Anchor == nil {
			anchor := now
			ts.Anchor = &anchor
		}
		if now.After(ts.Cursor) {
			ts.Cursor = now
		}
		e.state.Tasks[t.ID] = ts
		if ts.Eligible {
			e.next[t.ID] = t.Schedule.next(ts.Cursor, ts.Anchor)
		}
	}
	return e.saveLocked()
}

func addMissed(ts *taskState, b occurrences) {
	if b.Count == 0 {
		return
	}
	ts.Missed += b.Count
	if ts.MissedFirst == nil || b.First.Before(*ts.MissedFirst) {
		v := b.First
		ts.MissedFirst = &v
	}
	if ts.MissedLast == nil || b.Last.After(*ts.MissedLast) {
		v := b.Last
		ts.MissedLast = &v
	}
}
func (e *entry) tick() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping || e.retired || !e.valid || e.owner == nil || e.ownerErr != nil || e.stateErr != nil {
		return
	}
	now := e.now()
	if !e.nextPrune.IsZero() && !now.Before(e.nextPrune) {
		if err := e.pruneLocked(0); err != nil {
			e.stateErr = err
			return
		}
	}
	for _, t := range e.cfg.Tasks {
		ts := e.state.Tasks[t.ID]
		next := e.next[t.ID]
		if !*t.Enabled || ts.Paused || next.IsZero() || now.Before(next) || now.Before(ts.Cursor) {
			continue
		}
		batch := t.Schedule.batch(ts.Cursor, now, ts.Anchor)
		if batch.Count == 0 {
			e.next[t.ID] = t.Schedule.next(now, ts.Anchor)
			continue
		}
		ts.Cursor = now
		if now.Sub(batch.Last) >= time.Minute {
			addMissed(&ts, batch)
			e.state.Tasks[t.ID] = ts
			if err := e.saveLocked(); err != nil {
				e.stateErr = err
				return
			}
			e.next[t.ID] = t.Schedule.next(now, ts.Anchor)
			continue
		}
		missed := batch
		if missed.Count > 0 {
			missed.Count--
			missed.Last = batch.Previous
		}
		addMissed(&ts, missed)
		e.state.Tasks[t.ID] = ts
		reason := ""
		if e.running[t.ID] != nil {
			reason = "overlap"
		} else if len(e.state.Active) >= 4 {
			reason = "capacity"
		}
		if reason != "" {
			when := batch.Last
			e.state.Runs = append(e.state.Runs, Record{ID: randomID("run-"), TaskID: t.ID, Revision: ts.Revision, Trigger: "scheduled", ScheduledAt: &when, AcceptedAt: now, FinishedAt: &now, Status: "skipped", Reason: str(reason)})
			if err := e.pruneLocked(0); err != nil {
				e.stateErr = err
				return
			}
			if err := e.saveLocked(); err != nil {
				e.stateErr = err
				return
			}
		} else {
			when := batch.Last
			if _, err := e.startLocked(t, "scheduled", &when); err != nil {
				e.stateErr = err
				return
			}
		}
		e.next[t.ID] = t.Schedule.next(now, ts.Anchor)
	}
}
