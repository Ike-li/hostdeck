//go:build darwin

package manualview

import (
	"bytes"
	"encoding/binary"
	"os"
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
	args, env, ok := parseProcArgs(buf.Bytes())
	if !ok || !strings.EqualFold(args[0], "tool") || args[1] != "one two" || len(env) != 2 || env[0] != "A=b" || env[1] != "C=" {
		t.Fatalf("args %q env %q ok %v", args, env, ok)
	}
	if _, _, ok := parseProcArgs([]byte{1, 0, 0, 0}); ok {
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
