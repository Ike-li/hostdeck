package observation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestProcessFilesUseTheKernelIdentityAndOnlyActualOutputDescriptors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cmd := exec.Command("/bin/sleep", "20")
	cmd.Stdout = f
	cmd.Stderr = f
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	path, _ = filepath.EvalSymlinks(path)
	id := ProcessIdentity(cmd.Process.Pid)
	if id.Started.IsZero() {
		t.Fatal("identity missing")
	}
	paths, reason := ProcessFiles(context.Background(), id)
	if reason != "" || !slices.Contains(paths, path) {
		t.Fatalf("paths %v reason %s", paths, reason)
	}
	id.Started = id.Started.Add(time.Second)
	paths, reason = ProcessFiles(context.Background(), id)
	if len(paths) != 0 || reason != "identity_changed" {
		t.Fatalf("reused: %v %s", paths, reason)
	}
}
