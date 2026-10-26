// Package process owns OS process trees independently of native plugin dispatch.
package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Identity struct {
	PID   int    `json:"pid"`
	Token string `json:"token"`
}
type Spec struct {
	Command []string `json:"command"`
	Dir     string   `json:"dir"`
	Env     []string `json:"env"`
	Token   string   `json:"token"`
}
type Hooks struct {
	Prepared       func(Identity) error
	Started        func(int) error
	Stdout, Stderr io.Writer
}
type Outcome struct {
	Code  *int
	Error error
	Clean bool
}
type frame struct {
	Type  string `json:"type"`
	Data  []byte `json:"data,omitempty"`
	PID   int    `json:"pid,omitempty"`
	Code  *int   `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

// Run gates task launch on OS ownership and the caller's durable start marker.
// The parent keeps stdin open as a liveness pipe until the worker is reaped.
func Run(ctx context.Context, executable string, spec Spec, hooks Hooks) (result Outcome) {
	cmd := exec.CommandContext(ctx, executable, "--internal-task-worker")
	cmd.WaitDelay = time.Second
	input, err := cmd.StdinPipe()
	if err != nil {
		result.Error = err
		return
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		result.Error = err
		return
	}
	defer output.Close()
	tree, err := NewTree(cmd, spec.Token)
	if err != nil {
		result.Error = err
		return
	}
	defer tree.Close()
	if err = cmd.Start(); err != nil {
		result.Error = err
		result.Clean = true
		return
	}
	id := Identity{PID: cmd.Process.Pid, Token: spec.Token}
	defer func() {
		tree.Stop()
		input.Close()
		cmd.Wait()
		until := time.Now().Add(time.Second)
		for {
			if Gone(id) {
				result.Clean = true
				break
			}
			if time.Now().After(until) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if ctx.Err() != nil {
			result.Error = ctx.Err()
		}
	}()
	if err = tree.Started(); err != nil {
		result.Error = err
		return
	}
	if hooks.Prepared != nil {
		if err = hooks.Prepared(id); err != nil {
			result.Error = err
			return
		}
	}
	if err = json.NewEncoder(input).Encode(spec); err != nil {
		result.Error = err
		return
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		var f frame
		if err = json.Unmarshal(scanner.Bytes(), &f); err != nil {
			result.Error = errors.New("invalid worker frame")
			return
		}
		switch f.Type {
		case "started":
			if hooks.Started != nil {
				if err = hooks.Started(f.PID); err != nil {
					result.Error = err
					return
				}
			}
		case "stdout", "stderr":
			w := hooks.Stdout
			if f.Type == "stderr" {
				w = hooks.Stderr
			}
			if w != nil {
				if _, err = w.Write(f.Data); err != nil {
					result.Error = err
					return
				}
			}
		case "exit":
			result.Code = f.Code
			if f.Error != "" {
				result.Error = errors.New(f.Error)
			} else if f.Code == nil {
				result.Error = errors.New("worker omitted exit code")
			} else if *f.Code != 0 {
				result.Error = fmt.Errorf("exit status %d", *f.Code)
			}
			return
		default:
			result.Error = errors.New("unknown worker frame")
			return
		}
	}
	result.Error = scanner.Err()
	if result.Error == nil {
		result.Error = errors.New("worker ended without a result")
	}
	return
}

type frameWriter struct {
	mu      *sync.Mutex
	encoder *json.Encoder
	kind    string
}

func (w frameWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		size := min(len(p), 32<<10)
		w.mu.Lock()
		err := w.encoder.Encode(frame{Type: w.kind, Data: p[:size]})
		w.mu.Unlock()
		if err != nil {
			return 0, err
		}
		p = p[size:]
	}
	return n, nil
}

// Worker is a private entry point, reached only by a child of this executable.
// Arguments and environment travel through stdin rather than process listings.
func Worker() int {
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20))
	var spec Spec
	if decoder.Decode(&spec) != nil || len(spec.Command) == 0 || spec.Token == "" {
		return 125
	}
	go func() { io.Copy(io.Discard, os.Stdin); killWorkerTree(spec.Token) }()
	enc := json.NewEncoder(os.Stdout)
	mu := &sync.Mutex{}
	emit := func(f frame) { mu.Lock(); defer mu.Unlock(); enc.Encode(f) }
	for _, entry := range spec.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "PATH") {
			os.Setenv("PATH", value)
		}
	}
	if err := os.Chdir(spec.Dir); err != nil {
		emit(frame{Type: "exit", Error: err.Error()})
		return 0
	}
	cmd := exec.Command(spec.Command[0], spec.Command[1:]...)
	cmd.Env = spec.Env
	cmd.Dir = spec.Dir
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Stdout = frameWriter{mu, enc, "stdout"}
	cmd.Stderr = frameWriter{mu, enc, "stderr"}
	if err := cmd.Start(); err != nil {
		emit(frame{Type: "exit", Error: err.Error()})
		return 0
	}
	emit(frame{Type: "started", PID: cmd.Process.Pid})
	err := cmd.Wait()
	code := cmd.ProcessState.ExitCode()
	var exited *exec.ExitError
	if err != nil && !errors.As(err, &exited) && !errors.Is(err, exec.ErrWaitDelay) {
		emit(frame{Type: "exit", Code: &code, Error: err.Error()})
	} else {
		emit(frame{Type: "exit", Code: &code})
	}
	return 0
}
