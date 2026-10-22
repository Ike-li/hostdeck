package main

import (
	"strings"
	"testing"
)

func TestStartupRefusesAddressesOtherThanLoopback(t *testing.T) {
	for _, addr := range []string{
		"0.0.0.0:4678",
		":4678", // an empty host listens on every interface
		"192.168.1.20:4678",
		"[::]:4678",
		"example.com:4678",
		"127.0.0.1",
	} {
		err := run([]string{"--addr", addr, "--dir", t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("--addr %s: err = %v, want a refusal naming 127.0.0.1", addr, err)
		}
	}
}

func TestTheFolderComesFromTheFlagThenTheEnvironment(t *testing.T) {
	t.Setenv("HOSTDECK_DIR", "env-dir")
	if got, _ := configDir("flag-dir"); got != "flag-dir" {
		t.Errorf("with --dir: got %q, want flag-dir", got)
	}
	if got, _ := configDir(""); got != "env-dir" {
		t.Errorf("without --dir: got %q, want env-dir", got)
	}
}
