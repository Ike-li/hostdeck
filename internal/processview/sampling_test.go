package processview

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWindowsCPUUsesTwoReadingsAndNeverReusesAnotherProcessLifetime(t *testing.T) {
	at := time.Now()
	calls := 0
	out, err := readWindows(context.Background(), true, func(context.Context, bool) ([]sample, error) {
		calls++
		if calls == 1 {
			return []sample{{proc: proc{PID: 42, Name: "busy"}, Started: 1, Ticks: 10, At: at}, {proc: proc{PID: 43, Name: "old"}, Started: 1, Ticks: 100, At: at}}, nil
		}
		return []sample{{proc: proc{PID: 42, Name: "busy"}, Started: 1, Ticks: 2500010, At: at.Add(200 * time.Millisecond)}, {proc: proc{PID: 43, Name: "replacement"}, Started: 2, Ticks: 1, At: at.Add(200 * time.Millisecond)}, {proc: proc{PID: 44, Name: "protected"}}}, nil
	}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(out) != 3 || out[0].CPU == nil || *out[0].CPU != 125 || out[1].CPU != nil || out[2].CPU != nil {
		t.Fatalf("readings %+v; calls %d", out, calls)
	}
}

func TestWindowsCardsDoNotWaitOrOpenProcessMetrics(t *testing.T) {
	calls := 0
	out, err := readWindows(context.Background(), false, func(_ context.Context, metrics bool) ([]sample, error) {
		calls++
		if metrics {
			t.Error("requested metrics")
		}
		return []sample{{proc: proc{PID: 42, Name: "protected"}}}, nil
	}, func(context.Context) error { t.Error("card waited"); return nil })
	if err != nil || calls != 1 || len(out) != 1 {
		t.Fatalf("%+v %v %d", out, err, calls)
	}
}

func TestWindowsEnumerationAndCancellationFailuresAreNotPartialReadings(t *testing.T) {
	for _, failed := range []int{1, 2} {
		calls := 0
		out, err := readWindows(context.Background(), true, func(context.Context, bool) ([]sample, error) {
			calls++
			if calls == failed {
				return nil, errors.New("enumeration failed")
			}
			return []sample{{proc: proc{PID: 42}}}, nil
		}, func(context.Context) error { return nil })
		if err == nil || len(out) != 0 {
			t.Fatalf("partial result %+v %v", out, err)
		}
	}
	out, err := readWindows(context.Background(), true, func(context.Context, bool) ([]sample, error) { return []sample{{proc: proc{PID: 42}}}, nil }, func(context.Context) error { return context.Canceled })
	if !errors.Is(err, context.Canceled) || len(out) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestWindowsCPUDoesNotInventZeroForMissingOrInconsistentReadings(t *testing.T) {
	at := time.Now()
	cases := []struct {
		name          string
		before, after sample
		available     bool
	}{
		{"no identity", sample{Started: 0, Ticks: 1, At: at}, sample{Started: 0, Ticks: 2, At: at.Add(time.Second)}, false},
		{"counter reversed", sample{Started: 1, Ticks: 2, At: at}, sample{Started: 1, Ticks: 1, At: at.Add(time.Second)}, false},
		{"no elapsed time", sample{Started: 1, Ticks: 1, At: at}, sample{Started: 1, Ticks: 2, At: at}, false},
		{"observed idle", sample{Started: 1, Ticks: 1, At: at}, sample{Started: 1, Ticks: 1, At: at.Add(time.Second)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			out, err := readWindows(context.Background(), true, func(context.Context, bool) ([]sample, error) {
				calls++
				if calls == 1 {
					return []sample{tc.before}, nil
				}
				return []sample{tc.after}, nil
			}, func(context.Context) error { return nil })
			if err != nil || len(out) != 1 || (out[0].CPU != nil) != tc.available {
				t.Fatalf("reading %+v %v", out, err)
			}
		})
	}
}
