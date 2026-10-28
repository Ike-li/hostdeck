package observation

import (
	"bytes"
	"path/filepath"
)

// lsof field output is structured by file descriptor. Names alone do not
// authorize a source: only stdout/stderr descriptors of type REG are accepted.
func descriptorFiles(out []byte) []string {
	var paths []string
	fd := ""
	regular := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) < 2 {
			continue
		}
		value := string(line[1:])
		switch line[0] {
		case 'f':
			fd = value
			regular = false
		case 't':
			regular = value == "REG"
		case 'n':
			if (fd == "1" || fd == "2") && regular && filepath.IsAbs(value) {
				paths = append(paths, value)
			}
		}
	}
	return paths
}
