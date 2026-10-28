package manualview

import (
	"context"
	"errors"
	"os"
	"time"
)

// osOperator performs an action on this machine. run reads the listen table
// and the process list; inspection and signals are direct system calls.
type osOperator struct{ run commandRunner }

func (o osOperator) Snapshot(ctx context.Context) (map[int]Listener, map[int]Proc, error) {
	listeners, procs, err := readPlatform(ctx, o.run)
	if err != nil {
		return nil, nil, err
	}
	enrichListed(listeners, procs)
	return listeners, procs, nil
}

func (osOperator) Self() int { return os.Getpid() }

func (osOperator) Sleep(d time.Duration) { time.Sleep(d) }

func (osOperator) Spawn(string, []string, string, string, []string) error {
	return errors.New("manual restart requires a prepared collector")
}
