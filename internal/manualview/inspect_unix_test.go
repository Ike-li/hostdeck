//go:build darwin

package manualview

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/Ike-li/hostdeck/internal/observation"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcArgsKeepTheArgumentListAndTheEnvironment(t *testing.T) {
	var buf bytes.Buffer
	write := func(s string) { buf.WriteString(s); buf.WriteByte(0) }
	var argc [4]byte
	binary.LittleEndian.PutUint32(argc[:], 2)
	buf.Write(argc[:])
	write("/bin/tool")
	buf.WriteByte(0)
	write("tool")
	write("one two")
	write("A=b")
	write("C=")
	exe, args, env, ok := parseProcArgs(buf.Bytes())
	if !ok || exe != "/bin/tool" || !strings.EqualFold(args[0], "tool") || args[1] != "one two" || len(env) != 2 || env[0] != "A=b" || env[1] != "C=" {
		t.Fatalf("exe %q args %q env %q ok %v", exe, args, env, ok)
	}
	if _, _, _, ok := parseProcArgs([]byte{1, 0, 0, 0}); ok {
		t.Fatal("short buffer was accepted")
	}
}

func TestLsofCwdKeepsTheAbsoluteDirectory(t *testing.T) {
	dir, ok := parseCwd([]byte("p4\nn/tmp/proxy\n"))
	if !ok || dir != "/tmp/proxy" {
		t.Fatalf("%q %v", dir, ok)
	}
	if _, ok := parseCwd([]byte("nrelative\n")); ok {
		t.Fatal("relative cwd accepted")
	}
}

func TestInspectionKeepsTheExecutableSeparateFromArgv(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	cmd.Args = []string{"not-the-path", "30"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(2 * time.Second)
	var in inspection
	for time.Now().Before(deadline) {
		in = inspect(cmd.Process.Pid)
		if in.KnowsArgs && in.Exe != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if in.Exe != "/bin/sleep" || len(in.Args) < 1 || in.Args[0] != "not-the-path" {
		t.Fatalf("exe %q args %q", in.Exe, in.Args)
	}
}

func TestSpawnRunsTheAbsoluteProgramAndKeepsArgv(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "args")
	store := observation.New(t.TempDir())
	id, _ := store.Create("manual", "test", "restart", observation.Target{})
	prepared, err := observation.Prepare(context.Background(), store.Root(), id, observation.Launch{Executable: exe, Arguments: []string{"renamed", "hostdeck-manual-args", file}, Directory: t.TempDir(), Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err := prepared.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(file)
		if err == nil && len(b) > 0 {
			text = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	args := strings.Split(text, "\x00")
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := observation.ReadCapture(store.Root(), id)
		if err == nil && record.Worker.PID > 1 && !record.Worker.Equal(observation.ProcessIdentity(record.Worker.PID)) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(args) < 3 || args[0] != "renamed" || args[1] != "hostdeck-manual-args" || args[2] != file {
		t.Fatalf("argv %q", args)
	}
}

func TestSpawnRefusesABareProgramName(t *testing.T) {
	store := observation.New(t.TempDir())
	id, _ := store.Create("manual", "test", "restart", observation.Target{})
	_, err := observation.Prepare(context.Background(), store.Root(), id, observation.Launch{Executable: "sleep", Arguments: []string{"sleep", "0"}, Directory: t.TempDir()})
	if err == nil {
		t.Fatal("a bare program name was started")
	}
}

func TestThisProcessReportsItsDirectoryCommandAndStart(t *testing.T) {
	in := inspect(os.Getpid())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !in.KnowsDir || in.Dir != wd {
		t.Fatalf("dir %+v, want %s", in, wd)
	}
	if !in.KnowsArgs || len(in.Args) == 0 || !strings.Contains(strings.Join(in.Args, "\n"), os.Args[0]) {
		t.Fatalf("args %+v", in.Args)
	}
	if !in.KnowsEnv || !hasEnv(in.Env, "PATH=") && !hasEnv(in.Env, "Path=") {
		t.Fatalf("env missing PATH, %d entries", len(in.Env))
	}
	if in.Started.IsZero() || in.Started.After(time.Now().Add(time.Second)) || time.Since(in.Started) > time.Hour {
		t.Fatalf("started %s", in.Started)
	}
}

func hasEnv(env []string, prefix string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}
