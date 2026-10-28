//go:build darwin

package listenerview

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestProcArgsKeepsTheArgumentList(t *testing.T) {
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
	if !ok || exe != "/bin/tool" || !strings.EqualFold(args[0], "tool") || args[1] != "one two" || len(env) != 2 {
		t.Fatalf("exe %q args %q env %q ok %v", exe, args, env, ok)
	}
	if _, _, _, ok := parseProcArgs([]byte{1, 0, 0, 0}); ok {
		t.Fatal("short buffer was accepted")
	}
}

func TestLsofCwdKeepsTheAbsoluteDirectory(t *testing.T) {
	dir, ok := parseCwd([]byte("p4\nn/tmp/project\n"))
	if !ok || dir != "/tmp/project" {
		t.Fatalf("%q %v", dir, ok)
	}
	if _, ok := parseCwd([]byte("nrelative\n")); ok {
		t.Fatal("relative cwd accepted")
	}
	if _, ok := parseCwd([]byte("")); ok {
		t.Fatal("empty output accepted")
	}
}
