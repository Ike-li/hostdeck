//go:build windows

package manualview

import (
	"os"
	"os/exec"
	"testing"
)

func startShell(t *testing.T, file string) *exec.Cmd {
	t.Helper()
	return exec.Command("cmd", "/c", os.Args[0], "hostdeck-manual-listen", file)
}

func stopShell(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
