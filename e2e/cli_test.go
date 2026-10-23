package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

func cliCommand(t *testing.T, want int, args ...string) gjson.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "HOSTDECK_DIR="+t.TempDir())
	out, err := cmd.Output()
	code := 0
	if err != nil {
		if failed, ok := err.(*exec.ExitError); ok {
			code = failed.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if code != want || !json.Valid(out) {
		t.Fatalf("%v: exit %d want %d; stdout %q, err %v", args, code, want, out, err)
	}
	return gjson.ParseBytes(out)
}

func TestTheBinaryExposesConnectionCommandsWithoutStartingAnotherServer(t *testing.T) {
	help := exec.Command(binary, "--help")
	out, err := help.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "hostdeck help") {
		t.Fatalf("startup help should point to agent commands and exit successfully: %s %v", out, err)
	}
	dir := t.TempDir()
	url := startConsole(t, dir)
	added := cliCommand(t, 0, "page", "add", "--url", url, "--mode", "link", "--address", "https://example.test/", "--name", "Notes")
	id := added.Get("data.id").String()
	if id == "" || !added.Get("data.loaded").Bool() {
		t.Fatalf("page not loaded: %s", added.Raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", id, "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	cliCommand(t, 0, "page", "update", "--url", url, "--id", id, "--name", "Updated")
	listed := cliCommand(t, 0, "plugin", "list", "--url", url)
	if listed.Get(`data.installed.#(id=="`+id+`").name`).String() != "Updated" {
		t.Fatalf("updated page missing: %s", listed.Raw)
	}
	cliCommand(t, 2, "plugin", "unknown")
	cliCommand(t, 2, "page", "add", "--url", "https://example.test", "--address", "https://example.test")
}
