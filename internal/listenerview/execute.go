package listenerview

import (
	"context"
	"errors"
	"os"

	"github.com/Ike-li/hostdeck/internal/run"
)

type commandRunner func(context.Context, []string) ([]byte, int, error)

// Execute runs the listeners card or detail program. A reading failure is JSON
// the card can show; a cancelled context and an unknown operation are errors.
func Execute(ctx context.Context, dir string, args []string, r run.Runner) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runCmd := func(ctx context.Context, argv []string) ([]byte, int, error) {
		got, runErr := r(ctx, run.Command{Dir: dir, Args: argv})
		var exit *run.ExitError
		if errors.As(runErr, &exit) {
			return got, exit.Code, nil
		}
		return got, 0, runErr
	}
	if len(args) < 2 || args[0] != "listeners" {
		return nil, errors.New("unknown listeners command")
	}
	switch args[1] {
	case "card", "detail":
		if len(args) != 2 {
			return nil, errors.New("unknown listeners command")
		}
	case "action":
		if len(args) != 4 {
			return nil, errors.New("unknown listeners command")
		}
		return act(ctx, osOperator{run: runCmd}, dir, args[2], args[3])
	default:
		return nil, errors.New("unknown listeners command")
	}
	sockets, procs, err := readPlatform(ctx, runCmd)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return readingError(err)
	}
	sockets = fillCommands(sockets, procs)
	marks := Marks{SelfPID: os.Getpid()}
	if forwarded, ferr := loadForwarded(dir); ferr == nil {
		marks.Forwarded = forwarded
	}
	if args[1] == "card" {
		return Card(sockets, marks), nil
	}
	sockets = enrichSockets(sockets, defaultInspector)
	return Detail(sockets, marks), nil
}

var defaultInspector inspector = platformInspect

func fillCommands(sockets []Socket, procs map[int]Proc) []Socket {
	out := make([]Socket, 0, len(sockets))
	for _, s := range sockets {
		if s.Command == "" {
			if p, ok := procs[s.PID]; ok {
				s.Command = p.Name
			}
		}
		if _, ok := procs[s.PID]; !ok {
			continue
		}
		out = append(out, s)
	}
	return out
}
