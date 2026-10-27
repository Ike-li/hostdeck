package manualview

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/Ike-li/hostdeck/internal/run"
)

// commandRunner runs one absolute command. Exit is the code when the program
// ran; Err is set when it could not be started. A cancelled context is Err.
type commandRunner func(context.Context, []string) ([]byte, int, error)

// Execute runs the manual card or detail program. A reading failure is JSON
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
	if len(args) < 2 || args[0] != "manual" {
		return nil, errors.New("unknown manual command")
	}
	switch args[1] {
	case "card", "detail":
		if len(args) != 2 {
			return nil, errors.New("unknown manual command")
		}
	case "action":
		if len(args) != 4 {
			return nil, errors.New("unknown manual command")
		}
		return act(ctx, osOperator{run: runCmd}, args[2], args[3])
	default:
		return nil, errors.New("unknown manual command")
	}
	listeners, procs, err := readPlatform(ctx, runCmd)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return readingError(err)
	}
	enrichListed(listeners, procs)
	if args[1] == "card" {
		return Card(listeners, procs), nil
	}
	return Detail(listeners, procs, os.Getpid()), nil
}

func readingError(err error) ([]byte, error) {
	var failure *readFailure
	if !errors.As(err, &failure) {
		failure = &readFailure{en: "Could not list services started by hand.", zh: "读不到手动启动的服务。"}
	}
	return json.Marshal(struct {
		Error text `json:"error"`
	}{words(failure.en, failure.zh)})
}
