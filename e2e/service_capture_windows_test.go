package e2e

import (
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func startCaptureShell(t *testing.T, file string) *exec.Cmd {
	return exec.Command("cmd", "/c", os.Args[0], "manual-capture", file)
}
func observationConsoleGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}
func interruptObservationConsole(cmd *exec.Cmd) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}
