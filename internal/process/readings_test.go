package process

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependentProcessReadingsCanFinishWhileAnotherProcessIsSlow(t *testing.T) {
	started := make(chan int, 8)
	release := make(chan struct{})
	done := make(chan map[int]int, 1)
	var active, peak atomic.Int32
	go func() {
		done <- ReadPIDs([]int{1, 2, 3, 4, 5, 6, 7, 8, 1}, func(pid int) int {
			n := active.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- pid
			<-release
			active.Add(-1)
			return pid * 10
		})
	}()
	// Readings must overlap, but no more than four processes may be inspected.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-timer.C:
			close(release)
			<-done
			t.Fatal("one blocked process held up independent readings")
		}
	}
	select {
	case pid := <-started:
		close(release)
		<-done
		t.Fatalf("unbounded inspection started PID %d", pid)
	default:
	}
	close(release)
	got := <-done
	if len(got) != 8 || got[1] != 10 || got[8] != 80 || peak.Load() > 4 {
		t.Fatalf("readings %v peak %d", got, peak.Load())
	}
}
