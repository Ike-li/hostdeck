//go:build darwin

package manualview

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func (osOperator) Spawn(exe string, args []string, _ string, dir string, env []string) error {
	// Command resolves a bare name in this process's PATH before Env is applied.
	// The recorded path is absolute, and Args keeps the original argument list.
	if !filepath.IsAbs(exe) || len(args) == 0 {
		return errors.New("no command")
	}
	cmd := exec.Command(exe)
	cmd.Args = append([]string(nil), args...)
	cmd.Dir = dir
	if env == nil {
		env = []string{}
	}
	cmd.Env = env
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
