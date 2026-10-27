// Package atomicfile writes a file whole or not at all.
package atomicfile

import "os"

// syncFile flushes the temp file to durable storage before replace.
// Tests replace it to observe Sync and to force Sync failures.
var syncFile = (*os.File).Sync

// Write writes data to a temporary file next to file, syncs it, and renames
// it over file, so a crash never leaves half a file and a power loss does
// not keep stale layout or ports bytes after a successful Write returns.
func Write(file string, data []byte) error {
	tmp := file + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = syncFile(f)
	}
	closeErr := f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if err := replace(tmp, file); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
