package examples

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/detail"
)

func TestJobDetailsReadConfiguredArgumentsAllLaunchRulesAndBoundedLogs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS plist reader")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "job.plist")
	log := filepath.Join(dir, "中文 out.log")
	if err := os.WriteFile(log, []byte(strings.Repeat("old line\n", 4000)+"last 中文 line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Label</key><string>com.example.job</string><key>ProgramArguments</key><array><string>/usr/bin/printf</string><string>a b</string><string>quoted &quot;中文&quot;</string></array><key>RunAtLoad</key><true/><key>StartInterval</key><integer>3600</integer><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict></dict></plist>`)
	if err := os.WriteFile(config, []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	withJobInfo(t, fmt.Sprintf("\tstate = running\n\tpid = 4242\n\tpath = %s\n\tprogram = /usr/bin/printf\n\tstdout path = %s\n\tstderr path = %s", config, log, log))
	got := detail.Read(context.Background(), load(t, "jobs"))
	if got.Error != nil || len(got.Items) != 1 {
		t.Fatalf("detail %+v error %+v", got, got.Error)
	}
	encoded, _ := json.Marshal(got.Items[0].Fields)
	out := string(encoded)
	for _, want := range []string{"4242", config, "a b", "中文", "SuccessfulExit", "3600", "Run at load"} {
		if !strings.Contains(out, want) {
			t.Errorf("fields missing %q: %s", want, out)
		}
	}
	tail := got.Items[0].Log
	if !strings.Contains(tail, "last 中文 line") || strings.Count(tail, "old line") != 19 || strings.Count(tail, log) != 1 || len(tail) > 17000 {
		t.Fatalf("unbounded or duplicate tail: %q", tail)
	}
}

func TestMissingJobConfigurationAndLogsKeepTheJobAndItsAction(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS plist reader")
	}
	withJobInfo(t, "\tstate = running\n\tpath = /does-not-exist/job.plist\n\tstdout path = /does-not-exist/log")
	got := detail.Read(context.Background(), load(t, "jobs"))
	if got.Error != nil || len(got.Items) != 1 || got.Items[0].Status != "ok" || len(got.Items[0].Actions) != 1 {
		t.Fatalf("detail %+v error %+v", got, got.Error)
	}
	encoded, _ := json.Marshal(got.Items[0].Fields)
	fields := string(encoded)
	if !strings.Contains(fields, "Could not read") || !strings.Contains(fields, "读不到") {
		t.Fatalf("missing local bilingual explanation: %s", fields)
	}
}

func TestJobLogPreviewsShareOnePageBudget(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS log reader")
	}
	log := filepath.Join(t.TempDir(), "large.log")
	if err := os.WriteFile(log, []byte(strings.Repeat("中文", 6000)), 0600); err != nil {
		t.Fatal(err)
	}
	withLaunchctl(t, fmt.Sprintf(`#!/bin/sh
case "$2" in
"gui/$(id -u)")
 printf '\tservices = {\n'
 i=0; while [ "$i" -lt 34 ]; do printf '0 0 com.example.job%%s\n' "$i"; i=$((i+1)); done
 printf '}\n' ;;
*) printf '\tstate = running\n\tstdout path = %%s\n' %q ;;
esac
`, log))
	got := detail.Read(context.Background(), load(t, "jobs"))
	if got.Error != nil || len(got.Items) != 34 {
		t.Fatalf("items %d error %+v", len(got.Items), got.Error)
	}
	size := 0
	for _, it := range got.Items {
		size += len(it.Log)
	}
	out, _ := json.Marshal(got)
	if size > 524288 || !strings.Contains(string(out), "512 KiB") {
		t.Fatalf("budget %d bytes, no truncation notice: %s", size, out[len(out)-1000:])
	}
}

func TestBinaryJobConfigurationKeepsCalendarRulesAndTreatsArgumentsAsData(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS plist reader")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "binary.plist")
	marker := filepath.Join(dir, "must-not-run")
	args := []string{"/bin/echo", "single'quote", "$(touch " + marker + ")", "line\nending"}
	data, _ := json.Marshal(map[string]any{"Label": "com.example.job", "ProgramArguments": args, "KeepAlive": true, "StartCalendarInterval": []any{map[string]int{"Hour": 3, "Minute": 15}, map[string]int{"Weekday": 2, "Hour": 4}}, "WatchPaths": []string{dir}, "StartOnMount": true})
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("plutil", "-convert", "binary1", "--", config).CombinedOutput(); err != nil {
		t.Fatalf("binary fixture %q: %v", out, err)
	}
	withJobInfo(t, "\tstate = not running\n\tpath = "+config)
	got := detail.Read(context.Background(), load(t, "jobs"))
	if got.Error != nil || len(got.Items) != 1 {
		t.Fatalf("reading %+v", got.Error)
	}
	encoded, _ := json.Marshal(got.Items[0].Fields)
	for _, want := range []string{"StartCalendarInterval", "Weekday", "WatchPaths", "StartOnMount", "Keep alive after exit"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("missing %s in %s", want, encoded)
		}
	}
	command := ""
	for _, field := range got.Items[0].Fields {
		if field.Name.ByLang["en"] == "Command" {
			command = field.Value.String()
		}
	}
	if command == "" {
		t.Fatal("no configured command")
	}
	// Parse the displayed shell quoting without executing the program itself.
	out, err := exec.Command("sh", "-c", "set -- "+command+`; printf '%s\000' "$@"`).Output()
	if err != nil || string(out) != strings.Join(args, "\x00")+"\x00" {
		t.Fatalf("argument boundaries changed: %q %v, shown %q", out, err, command)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("an argument was executed")
	}
}

func TestUnreadableOptionalFilesDoNotChangeJobHealth(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS file reader")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "broken.plist")
	log := filepath.Join(dir, "private.log")
	if err := os.WriteFile(config, []byte("not a plist"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, []byte("private"), 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(log, 0600)
	withJobInfo(t, fmt.Sprintf("\tstate = running\n\tpath = %s\n\tstderr path = %s", config, log))
	got := detail.Read(context.Background(), load(t, "jobs"))
	if got.Error != nil || len(got.Items) != 1 {
		t.Fatalf("reading %+v", got.Error)
	}
	encoded, _ := json.Marshal(got.Items[0].Fields)
	if got.Items[0].Status != "ok" || len(got.Items[0].Actions) != 1 || !strings.Contains(string(encoded), "Could not read the task configuration") || !strings.Contains(string(encoded), "Could not read log") {
		t.Fatalf("optional read changed task or hid errors: %+v %s", got.Items[0], encoded)
	}
}
