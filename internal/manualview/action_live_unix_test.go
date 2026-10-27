//go:build unix

package manualview

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func startShell(t *testing.T, file string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/bash", "-c", `"$1" hostdeck-manual-listen "$2"`, "bash", os.Args[0], file)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func stopShell(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
