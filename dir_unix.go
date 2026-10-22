//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// defaultDir follows the XDG convention that command-line tools on macOS use,
// rather than ~/Library/Application Support, which is awkward to edit by hand.
func defaultDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "hostdeck"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "hostdeck"), nil
}
