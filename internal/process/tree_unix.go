//go:build !windows

package process

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

type Tree struct {
	cmd     *exec.Cmd
	mu      sync.Mutex
	stopped bool
}

func NewTree(cmd *exec.Cmd, token string) (*Tree, error) {
	t := &Tree{cmd: cmd}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = t.Stop
	return t, nil
}
func (t *Tree) Started() error { return nil }
func (t *Tree) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.cmd.Process == nil {
		return nil
	}
	t.stopped = true
	err := syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
func (t *Tree) Close() { t.Stop() }
func Gone(id Identity) bool {
	if id.PID <= 0 {
		return false
	}
	return errors.Is(syscall.Kill(-id.PID, 0), syscall.ESRCH)
}
func killWorkerTree(token string) { syscall.Kill(-os.Getpid(), syscall.SIGKILL); os.Exit(125) }
