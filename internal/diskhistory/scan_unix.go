//go:build !windows

package diskhistory

import (
	"os"
	"syscall"
)

func skipEntry(info, root os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	a, aok := info.Sys().(*syscall.Stat_t)
	b, bok := root.Sys().(*syscall.Stat_t)
	return aok && bok && a.Dev != b.Dev
}
