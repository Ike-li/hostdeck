package examples

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/run"
)

func TestJobActionsRejectTheWholeNameBeforeCallingTheOperatingSystem(t *testing.T) {
	for _, name := range []string{"", "valid\ninvalid/name", "valid\n", "with space", "with\ttab", "中文", "café"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "calls")
			withLaunchctl(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexit 1\n", record))
			p := load(t, "jobs")
			out, err := run.Output(context.Background(), p.Dir, append(p.Actions.Exec.Args(), "start", name))
			if err != nil || !strings.Contains(string(out), "cannot be passed to launchctl") {
				t.Errorf("output %q, error %v; want the invalid name refused", out, err)
			}
			if got, err := os.ReadFile(record); !os.IsNotExist(err) {
				t.Errorf("launchctl calls %q, error %v; an invalid name must not reach the OS", got, err)
			}
		})
	}
}
