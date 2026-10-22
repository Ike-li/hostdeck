// Package run runs a plugin's command and makes sure that it ends, along with
// everything it started.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
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
	t.started()
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
