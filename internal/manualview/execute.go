package manualview

import (
	"context"
	"encoding/json"
	"errors"
)

// commandRunner runs one absolute command. Exit is the code when the program
// ran; Err is set when it could not be started. A cancelled context is Err.
type commandRunner func(context.Context, []string) ([]byte, int, error)

// Execute runs the manual card or detail program. A reading failure is JSON
// the card can show; a cancelled context and an unknown operation are errors.
func Execute(ctx context.Context, args []string, run commandRunner) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 2 || args[0] != "manual" || (args[1] != "card" && args[1] != "detail") {
		return nil, errors.New("unknown manual command")
	}
	listeners, procs, err := readPlatform(ctx, run)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var failure *readFailure
		if !errors.As(err, &failure) {
			failure = &readFailure{en: "Could not list services started by hand.", zh: "读不到手动启动的服务。"}
		}
		return json.Marshal(struct {
			Error text `json:"error"`
		}{words(failure.en, failure.zh)})
	}
	if args[1] == "card" {
		return Card(listeners, procs), nil
	}
	return Detail(listeners, procs), nil
}
