package processview

import (
	"context"
	"github.com/Ike-li/hostdeck/internal/run"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWindowsDoesNotInvokeAnExternalProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("real process table")
	}
	if _, err := readPlatform(context.Background(), "", func(context.Context, run.Command) ([]byte, error) {
		t.Fatal("unexpected external process")
		return nil, nil
	}, true); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsProcessMetadataIncludesThisProgramsPathLifetimeAndThreads(t *testing.T) {
	if testing.Short() {
		t.Skip("real process table")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := windowsSnapshot(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rows {
		if s.PID == os.Getpid() {
			if !strings.EqualFold(s.Path, executable) || s.StartedAt.IsZero() || s.Elapsed == nil || s.CPUTime == nil || s.Threads == nil || *s.Threads == 0 {
				t.Fatalf("metadata %+v", s)
			}
			return
		}
	}
	t.Fatal("test process missing")
}
