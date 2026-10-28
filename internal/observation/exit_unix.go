//go:build !windows

package observation

import (
	"os"
	"strconv"
	"syscall"
)

func exitSignal(state *os.ProcessState) string {
	if wait, ok := state.Sys().(syscall.WaitStatus); ok && wait.Signaled() {
		return strconv.Itoa(int(wait.Signal()))
	}
	return ""
}
