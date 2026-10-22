package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

// waitForReaders bounds how long replace waits for other programs to let go.
const waitForReaders = 2 * time.Second

// replace renames from over to. Windows refuses while another program, such
// as an editor or a virus scanner, has it open, so replace tries again until
// it lets go.
func replace(from, to string) error {
	deadline := time.Now().Add(waitForReaders)
	for {
		err := os.Rename(from, to)
		if err == nil || !errors.Is(err, fs.ErrPermission) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
