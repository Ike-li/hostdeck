package processview

import (
	"context"
	"time"
)

type sample struct {
	proc
	Started, Ticks uint64 // Windows FILETIME units (100 ns).
	At             time.Time
}

func readWindows(ctx context.Context, metrics bool, snapshot func(context.Context, bool) ([]sample, error), wait func(context.Context) error) ([]proc, error) {
	first, err := snapshot(ctx, metrics)
	if err != nil {
		return nil, err
	}
	latest := first
	if metrics {
		if err := wait(ctx); err != nil {
			return nil, err
		}
		latest, err = snapshot(ctx, true)
		if err != nil {
			return nil, err
		}
	}
	before := map[int]sample{}
	for _, s := range first {
		before[s.PID] = s
	}
	out := make([]proc, 0, len(latest))
	for _, s := range latest {
		old, ok := before[s.PID]
		elapsed := s.At.Sub(old.At).Seconds()
		if metrics && ok && s.Started != 0 && old.Started == s.Started && s.Ticks >= old.Ticks && elapsed > 0 {
			cpu := float64(s.Ticks-old.Ticks) / 1e7 / elapsed * 100
			s.CPU = &cpu
		}
		out = append(out, s.proc)
	}
	return out, nil
}
