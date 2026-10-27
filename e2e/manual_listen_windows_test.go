//go:build windows

package e2e

import (
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func startManualShell(t *testing.T, file string) *exec.Cmd {
	t.Helper()
	return exec.Command("cmd", "/c", os.Args[0], "manual-listen", file)
}

func stopManualShell(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func processGone(pid int) bool {
	if pid <= 1 {
		return true
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code != 259
}
