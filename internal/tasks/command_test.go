package tasks

import (
	"strings"
	"testing"
)

func TestApplyCommandWritesOnlyTheCurrentPlatform(t *testing.T) {
	argv := []string{"echo", "hi"}
	created := applyCommand(Execution{}, argv, "windows", true)
	if strings.Join(created.Command, " ") != "echo hi" || created.Windows != nil {
		t.Fatalf("create on windows %+v", created)
	}
	edited := applyCommand(Execution{Command: []string{"keep"}, Windows: []string{"old"}}, argv, "windows", false)
	if strings.Join(edited.Command, " ") != "keep" || strings.Join(edited.Windows, " ") != "echo hi" {
		t.Fatalf("edit existing windows %+v", edited)
	}
	plain := applyCommand(Execution{Command: []string{"old"}}, argv, "windows", false)
	if strings.Join(plain.Command, " ") != "echo hi" || plain.Windows != nil {
		t.Fatalf("edit without windows %+v", plain)
	}
	other := applyCommand(Execution{Command: []string{"old"}, Windows: []string{"keep"}}, argv, "darwin", false)
	if strings.Join(other.Command, " ") != "echo hi" || strings.Join(other.Windows, " ") != "keep" {
		t.Fatalf("darwin %+v", other)
	}
}

func TestACommandLineRoundTripsSpacesAndDoubleQuotes(t *testing.T) {
	args, err := splitCommand(`bash -lc "echo hi"`)
	if err != nil || strings.Join(args, "|") != "bash|-lc|echo hi" {
		t.Fatalf("%#v %v", args, err)
	}
	line, ok := quoteCommand(args)
	if !ok || line != `bash -lc "echo hi"` {
		t.Fatalf("quote %q %v", line, ok)
	}
	if _, ok := quoteCommand([]string{"echo", "say \"hi\""}); ok {
		t.Fatal("an argument containing a quote was presented as a line")
	}
	if _, err := splitCommand(`echo "hi`); err == nil {
		t.Fatal("unclosed quote accepted")
	}
}
