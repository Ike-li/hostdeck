package tasks_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func TestTheConsoleCanSaveListAndDeleteAScriptUnderThePlugin(t *testing.T) {
	dir, _ := fixture(t, "success", "5s")
	before, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	const body = "print('你好')\n"
	if err := tasks.CreateScript(dir, "hello.py", body); err != nil {
		t.Fatal(err)
	}
	listed, err := tasks.ListScripts(dir)
	if err != nil || len(listed) != 1 || listed[0].Name != "hello.py" || listed[0].Size != int64(len(body)) {
		t.Fatalf("list %+v %v", listed, err)
	}
	got, err := tasks.ReadScript(dir, "hello.py")
	if err != nil || got != body {
		t.Fatalf("read %q %v", got, err)
	}
	if err := tasks.UpdateScript(dir, "hello.py", "print(1)\n"); err != nil {
		t.Fatal(err)
	}
	if got, err = tasks.ReadScript(dir, "hello.py"); err != nil || got != "print(1)\n" {
		t.Fatalf("update %q %v", got, err)
	}
	if err := tasks.CreateScript(dir, "hello.py", "again"); !errors.Is(err, tasks.ErrScriptExists) {
		t.Fatalf("create overwrote: %v", err)
	}
	if err := tasks.DeleteScript(dir, "hello.py"); err != nil {
		t.Fatal(err)
	}
	if listed, err = tasks.ListScripts(dir); err != nil || len(listed) != 0 {
		t.Fatalf("after delete %+v %v", listed, err)
	}
	if err := tasks.UpdateScript(dir, "hello.py", "x"); !errors.Is(err, tasks.ErrScriptMissing) {
		t.Fatalf("update missing: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("script save changed tasks.json")
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "hello.py")); !os.IsNotExist(err) {
		t.Fatal("deleted script still present")
	}
}

func TestAScriptNameOrBodyOutsideTheFolderIsRefused(t *testing.T) {
	dir, _ := fixture(t, "success", "5s")
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside.txt", `..\outside.txt`, "scripts/hello.py", ".hidden.py", "hello.py.tmp", "app.min.js", "你好.py", "hello.", "hello.toolongext", strings.Repeat("a", 65)} {
		if err := tasks.CreateScript(dir, name, "x"); !errors.Is(err, tasks.ErrScriptName) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	if _, err := os.ReadFile(outside); err != nil || string(mustRead(t, outside)) != "keep" {
		t.Fatal("traversal wrote outside the plugin")
	}
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "scripts", "link.py")); err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateScript(dir, "link.py", "changed"); !errors.Is(err, tasks.ErrScriptRefused) {
		t.Fatalf("symlink update: %v", err)
	}
	if listed, err := tasks.ListScripts(dir); err != nil || len(listed) != 0 {
		t.Fatalf("symlink listed: %+v %v", listed, err)
	}
	if err := tasks.DeleteScript(dir, "link.py"); !errors.Is(err, tasks.ErrScriptRefused) {
		t.Fatalf("symlink delete: %v", err)
	}
	if string(mustRead(t, outside)) != "keep" {
		t.Fatal("symlink delete changed the target")
	}
	if err := tasks.CreateScript(dir, "big.py", strings.Repeat("a", (1<<20)+1)); !errors.Is(err, tasks.ErrScriptContent) {
		t.Fatalf("oversize: %v", err)
	}
	if err := tasks.CreateScript(dir, "nul.py", "a\x00b"); !errors.Is(err, tasks.ErrScriptContent) {
		t.Fatalf("nul: %v", err)
	}
	if err := tasks.CreateScript(dir, "bad.py", "\xff"); !errors.Is(err, tasks.ErrScriptContent) {
		t.Fatalf("utf-8: %v", err)
	}
}

func TestAScriptCannotBeWrittenWhenThisConsoleDoesNotOwnTheTasks(t *testing.T) {
	if err := tasks.CreateScript(t.TempDir(), "hello.py", "x"); !errors.Is(err, tasks.ErrTasksUnavailable) {
		t.Fatalf("unsynced: %v", err)
	}
	dir, p := fixture(t, "block", "30s")
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data/state.json"), []byte(`{"version":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.CreateScript(dir, "hello.py", "x"); err == nil {
		t.Fatal("corrupt state accepted a script")
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "hello.py")); !os.IsNotExist(err) {
		t.Fatal("corrupt state left a script")
	}

	if err := tasks.Close(context.Background(), filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "data/state.json")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "--task-owner-probe", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitRecord(t, dir, "running")
	if err := tasks.Sync(filepath.Dir(dir), []plugin.Plugin{p}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.CreateScript(dir, "hello.py", "x"); err == nil {
		t.Fatal("second console wrote a script")
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "hello.py")); !os.IsNotExist(err) {
		t.Fatal("second console left a script")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
