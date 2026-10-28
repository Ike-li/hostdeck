//go:build darwin

package listenerview

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

func (osOperator) Term(pid int) error { return signal(pid, syscall.SIGTERM) }

func (osOperator) Kill(pid int) error { return signal(pid, syscall.SIGKILL) }

func signal(pid int, sig syscall.Signal) error {
	if pid <= 1 {
		return errors.New("refused")
	}
	return syscall.Kill(pid, sig)
}

func (osOperator) Alive(pid int) bool {
	if pid <= 1 {
		return false
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return false
	}
	// A zombie is already dead; its parent has not reaped it yet.
	const zombie = 5
	return info.Proc.P_stat != zombie
}
