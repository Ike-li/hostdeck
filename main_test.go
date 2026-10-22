package main

import (
	"os"
	"path/filepath"
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

// exampleAt makes <dir>/examples/disk/plugin.yaml.
func exampleAt(t *testing.T, dir string) string {
	t.Helper()
	examples := filepath.Join(dir, "examples")
	if err := os.MkdirAll(filepath.Join(examples, "disk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(examples, "disk", "plugin.yaml"), []byte("schema: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return examples
}

func TestTheExamplesNextToTheProgramComeBeforeTheWorkingDirectorys(t *testing.T) {
	programDir, workDir := t.TempDir(), t.TempDir()
	if got := examplesDir(programDir, workDir); got != "" {
		t.Errorf("no examples anywhere: got %q, want none", got)
	}
	inWorkDir := exampleAt(t, workDir)
	if got := examplesDir(programDir, workDir); got != inWorkDir {
		t.Errorf("examples in the working directory: got %q, want %q", got, inWorkDir)
	}
	nextToProgram := exampleAt(t, programDir)
	if got := examplesDir(programDir, workDir); got != nextToProgram {
		t.Errorf("examples in both: got %q, want the ones next to the program, %q", got, nextToProgram)
	}
}
