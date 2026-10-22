// Package atomicfile writes a file whole or not at all.
package atomicfile

import "os"

// Write writes data to a temporary file next to file and renames it over
// file, so a crash never leaves half a file.
func Write(file string, data []byte) error {
	if err := os.WriteFile(file+".tmp", data, 0o644); err != nil {
		return err
	}
	return replace(file+".tmp", file)
}
