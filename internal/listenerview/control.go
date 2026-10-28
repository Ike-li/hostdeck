package listenerview

import (
	"context"
	"os"
	"time"
)

type osOperator struct {
	run commandRunner
}

func (o osOperator) Snapshot(ctx context.Context) ([]Socket, map[int]Proc, error) {
	sockets, procs, err := readPlatform(ctx, o.run)
	if err != nil {
		return nil, nil, err
	}
	sockets = fillCommands(sockets, procs)
	for i := range sockets {
		pid := sockets[i].PID
		p, ok := procs[pid]
		if !ok {
			continue
		}
		if p.Started.IsZero() {
			in := defaultInspector(pid)
			p.Started = in.Started
			procs[pid] = p
		}
	}
	return sockets, procs, nil
}

func (osOperator) Self() int { return os.Getpid() }

func (osOperator) Sleep(d time.Duration) { time.Sleep(d) }
