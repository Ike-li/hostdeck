//go:build !windows

package observation

import (
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"syscall"
)

func detached(cmd *exec.Cmd, _ string)    { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func storageLock(f *os.File) error        { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func executableAllowed(path string) error { return unix.Access(path, unix.X_OK) }
