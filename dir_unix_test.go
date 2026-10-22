//go:build !windows

package main

import (
	"path/filepath"
	"testing"
)

func TestTheDefaultFolderIsUnderXDGConfigHome(t *testing.T) {
	t.Setenv("HOSTDECK_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got, _ := configDir(""); got != filepath.FromSlash("/xdg/hostdeck") {
		t.Errorf("got %q, want /xdg/hostdeck", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/someone")
	if got, _ := configDir(""); got != filepath.FromSlash("/home/someone/.config/hostdeck") {
		t.Errorf("got %q, want ~/.config/hostdeck", got)
	}
}
