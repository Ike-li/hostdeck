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

type fileKey [2]uint64

// hardLink identifies a file with more than one name, so its blocks count once.
func hardLink(info os.FileInfo) (fileKey, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink < 2 {
		return fileKey{}, false
	}
	return fileKey{uint64(st.Dev), st.Ino}, true
}

// allocated is the space a file's blocks occupy, which for a sparse file is
// less than its length.
func allocated(_ string, info os.FileInfo) (int64, error) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512, nil
	}
	return info.Size(), nil
}
