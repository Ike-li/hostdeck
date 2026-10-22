package main

import "testing"

func TestTheDefaultFolderIsUnderLocalAppData(t *testing.T) {
	t.Setenv("HOSTDECK_DIR", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\someone\AppData\Local`)
	if got, _ := configDir(""); got != `C:\Users\someone\AppData\Local\hostdeck` {
		t.Errorf("got %q, want %%LOCALAPPDATA%%\\hostdeck", got)
	}
}
