package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The process-tree contract must say honestly what Unix can and cannot end:
// a process group via Setpgid and kill(-pgid), which setsid/daemonize can leave,
// versus a Windows Job Object that keeps the whole tree.
func TestTheDocsNameTheUnixProcessGroupBoundaryAndWindowsJobObject(t *testing.T) {
	want := []string{
		"Setpgid",
		"setsid",
		"Job Object",
	}
	wantZH := []string{
		"Setpgid",
		"setsid",
		"Job Object",
	}
	for _, doc := range []string{"PLUGINS.md", "ARCHITECTURE.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		for _, phrase := range want {
			if !strings.Contains(body, phrase) {
				t.Errorf("docs/%s does not name %q", doc, phrase)
			}
		}
	}
	for _, doc := range []string{"PLUGINS.zh-CN.md", "ARCHITECTURE.zh-CN.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		for _, phrase := range wantZH {
			if !strings.Contains(body, phrase) {
				t.Errorf("docs/%s does not name %q", doc, phrase)
			}
		}
	}
}
