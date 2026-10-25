// Package run runs a plugin's command and makes sure that it ends, along with
// everything it started.
package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/diskhistory"
)

// WaitDelay bounds the wait for a command's output after it exits or is
// killed: a grandchild that holds stdout open is cut off then.
const WaitDelay = time.Second

// maxOutput bounds what is kept of a command's stdout; a card is far smaller.
const maxOutput = 4 << 20

// ExitError is a command that exited with a code other than 0.
type ExitError struct {
	Code     int
	LastLine string // of stderr
}

func (e *ExitError) Error() string {
	if e.LastLine == "" {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return fmt.Sprintf("exit status %d: %s", e.Code, e.LastLine)
}

// Output runs command in dir and returns what it printed on stdout. When ctx
// ends first, the command and every process it started are killed, and the
// error is ctx's. Processes it leaves behind when it exits are killed too.
func Output(ctx context.Context, dir string, command []string) ([]byte, error) {
	if len(command) == 0 {
		return nil, errors.New("empty plugin command")
	}
	if command[0] == "@hostdeck" {
		out, err := diskhistory.Execute(ctx, dir, command[1:])
		if err == nil && len(out) > maxOutput {
			return nil, &OutputError{errors.New("native plugin output exceeds 4 MiB")}
		}
		return out, err
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	stdout, stderr := &capped{limit: maxOutput}, &capped{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = WaitDelay
	t, err := newTree(cmd)
	if err != nil {
		return nil, err
	}
	defer t.close()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if err := t.started(); err != nil {
		return nil, err
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil, errors.Is(err, exec.ErrWaitDelay):
		return stdout.Bytes(), nil
	case errors.As(err, &exitErr):
		return nil, &ExitError{Code: exitErr.ExitCode(), LastLine: lastLine(stderr.String())}
	}
	return nil, err
}

// OutputError is a command whose output is not the JSON it should print.
type OutputError struct{ Err error }

func (e *OutputError) Error() string { return e.Err.Error() }

// bom is what Windows PowerShell may print before its output.
var bom = []byte("\ufeff")

// JSON runs command like Output and decodes what it prints into v. Fields
// that v does not have are an error, so a typo in a plugin shows up.
func JSON(ctx context.Context, dir string, command []string, v any) error {
	out, err := Output(ctx, dir, command)
	if err != nil {
		return err
	}
	return Decode(out, v)
}

// Decode decodes a command's output into v as JSON does.
func Decode(out []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(out, bom)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &OutputError{err}
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("unexpected trailing JSON output")
		}
		return &OutputError{err}
	}
	return nil
}

// capped keeps the first limit bytes written to it and drops the rest.
type capped struct {
	bytes.Buffer
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.limit - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
