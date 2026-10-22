package main

import (
	"errors"
	"os"
	"path/filepath"
)

// defaultDir is under %LOCALAPPDATA%, not the roaming %APPDATA%: plugins point
// at services on this machine and should not follow the user elsewhere.
func defaultDir() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return "", errors.New("%LOCALAPPDATA% is not set; pass --dir")
	}
	return filepath.Join(local, "hostdeck"), nil
}
