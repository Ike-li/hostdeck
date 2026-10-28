//go:build unix

package e2e

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func startManualShell(t *testing.T, file string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/bash", "-c", `"$1" manual-listen "$2"; result=$?; exit "$result"`, "bash", os.Args[0], file)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func stopManualShell(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func processGone(pid int) bool {
	if pid <= 1 {
		return true
	}
	// A zombie is already dead. Its parent may not have reaped it, and kill -0
	// still succeeds for one.
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		return true
	}
	stat := strings.TrimSpace(string(out))
	return stat == "" || strings.Contains(stat, "Z")
}
