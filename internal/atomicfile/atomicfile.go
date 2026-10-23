// Package atomicfile writes a file whole or not at all.
package atomicfile

import "os"

// Write writes data to a temporary file next to file and renames it over
// file, so a crash never leaves half a file.
func Write(file string, data []byte) error {
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := replace(tmp, file); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
