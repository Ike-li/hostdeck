//go:build !windows

package run

import (
	"os/exec"
	"syscall"
)

// tree puts the command in a process group of its own, so that killing the
// group reaches every process it started.
type tree struct{ cmd *exec.Cmd }

func newTree(cmd *exec.Cmd) (*tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return &tree{cmd}, nil
}

func (t *tree) started() error { return nil }

func (t *tree) close() {
	if t.cmd.Process != nil {
		syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
	}
}
