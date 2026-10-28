//go:build unix

package e2e

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func startCaptureShell(t *testing.T, file string) *exec.Cmd {
	cmd := exec.Command("/bin/bash", "-c", `"$1" manual-capture "$2"; result=$?; exit "$result"`, "bash", os.Args[0], file)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}
func observationConsoleGroup(cmd *exec.Cmd)           { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func interruptObservationConsole(cmd *exec.Cmd) error { return cmd.Process.Signal(os.Interrupt) }
