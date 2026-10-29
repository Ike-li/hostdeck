package examples

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

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
	if got.Items[0].Log != "" || !got.Items[0].LogAvailable {
		t.Fatalf("list log %q available %v, want the body left out until the log is opened", got.Items[0].Log, got.Items[0].LogAvailable)
	}
	p := load(t, "jobs")
	one := detail.ReadItem(context.Background(), p, "com.example.job")
	if one.Error != nil || len(one.Items) != 1 {
		t.Fatalf("item %+v error %+v", one, one.Error)
	}
	tail := one.Items[0].Log
	if !strings.Contains(tail, "last 中文 line") || strings.Count(tail, "old line") != 4000 || strings.Count(tail, log) != 1 {
		t.Fatalf("opened tail lost lines or repeated the path: count %d len %d", strings.Count(tail, "old line"), len(tail))
	}

	big := filepath.Join(dir, "big.log")
	marker := filepath.Join(dir, "must-not-run")
	body := append([]byte("HEAD-MARKER\n"), bytes.Repeat([]byte("x"), 300000)...)
	body = append(body, []byte("\n$(touch "+marker+")\nlast 中文 line\n")...)
	if err := os.WriteFile(big, body, 0600); err != nil {
		t.Fatal(err)
	}
	withJobInfo(t, "\tstate = running\n\tstdout path = "+big)
	opened := detail.ReadItem(context.Background(), p, "com.example.job")
	if opened.Error != nil || len(opened.Items) != 1 {
		t.Fatalf("big item %+v", opened.Error)
	}
	encoded, _ = json.Marshal(opened.Items[0])
	shown := string(encoded)
	if strings.Contains(opened.Items[0].Log, "HEAD-MARKER") || !strings.Contains(opened.Items[0].Log, "last 中文 line") || !strings.Contains(shown, "Showing the last 256 KiB") || !strings.Contains(shown, "末尾 256 KiB") {
		t.Fatalf("big log was not the tail: %s", shown[max(0, len(shown)-500):])
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("text inside the log was executed")
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

func TestJobLogPreviewsStayOutOfTheListUntilOneJobIsOpened(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS log reader")
	}
	log := filepath.Join(t.TempDir(), "large.log")
	if err := os.WriteFile(log, []byte("LOGBODY-中文\n"), 0600); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "calls")
	withLaunchctl(t, fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$2" in
"gui/$(id -u)")
 printf '\tservices = {\n'
 i=0; while [ "$i" -lt 34 ]; do printf '0 0 com.example.job%%s\n' "$i"; i=$((i+1)); done
 printf '}\n' ;;
*) printf '\tstate = running\n\tstdout path = %%s\n' %q ;;
esac
`, record, log))
	p := load(t, "jobs")
	got := detail.Read(context.Background(), p)
	if got.Error != nil || len(got.Items) != 34 {
		t.Fatalf("items %d error %+v", len(got.Items), got.Error)
	}
	for _, it := range got.Items {
		if it.Log != "" || !it.LogAvailable {
			t.Fatalf("item %+v, want a log offer and no body", it)
		}
	}
	out, _ := json.Marshal(got)
	if strings.Contains(string(out), "LOGBODY") || strings.Contains(string(out), "512 KiB") {
		t.Fatalf("the list carried log text: %s", out[len(out)-500:])
	}
	uid := strconv.Itoa(os.Getuid())
	calls, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	domain := "print gui/" + uid + "\n"
	if strings.Count(string(calls), domain) != 1 || strings.Count(string(calls), "print gui/"+uid+"/com.example.job") != 34 {
		t.Fatalf("launchctl calls:\n%s", calls)
	}
	if err := os.WriteFile(record, nil, 0644); err != nil {
		t.Fatal(err)
	}
	one := detail.ReadItem(context.Background(), p, "com.example.job0")
	if one.Error != nil || len(one.Items) != 1 || !strings.Contains(one.Items[0].Log, "LOGBODY-中文") {
		t.Fatalf("opened item %+v", one)
	}
	calls, err = os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), domain) != 1 || strings.Count(string(calls), "print gui/"+uid+"/com.example.job") != 1 {
		t.Fatalf("one item read launchctl more than that job:\n%s", calls)
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
	encoded, _ := json.Marshal(got.Items[0])
	if got.Items[0].Status != "ok" || len(got.Items[0].Actions) != 1 || got.Items[0].Log != "" || !got.Items[0].LogAvailable || !strings.Contains(string(encoded), "Could not read the task configuration") || strings.Contains(string(encoded), "Could not read log") {
		t.Fatalf("list read changed the task or opened the log: %+v %s", got.Items[0], encoded)
	}
	one := detail.ReadItem(context.Background(), load(t, "jobs"), "com.example.job")
	if one.Error != nil || len(one.Items) != 1 || one.Items[0].Status != "ok" || len(one.Items[0].Actions) != 1 {
		t.Fatalf("opened log changed the task: %+v", one)
	}
	opened, _ := json.Marshal(one.Items[0].Fields)
	if !strings.Contains(string(opened), "Could not read log") || !strings.Contains(string(opened), "读不到日志") {
		t.Fatalf("opened log hid the unreadable file: %s", opened)
	}
}

func TestAJobListDoesNotStartATextUtilityForEachField(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS shell reader")
	}
	bin := t.TempDir()
	record := filepath.Join(bin, "text-calls")
	for _, name := range []string{"awk", "sed"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		script := fmt.Sprintf("#!/bin/sh\nprintf 'call\\n' >> %q\nexec %q \"$@\"\n", record, path)
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	withJobInfo(t, "\tstate = running\n\tpid = 4242\n\tprogram = /bin/echo\n\tlast exit code = 0\n\truns = 3")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v := detail.Read(ctx, load(t, "jobs"))
	if v.Error != nil || len(v.Items) != 1 {
		t.Fatalf("detail %+v", v)
	}
	calls, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(calls), "call\n"); n > 1 {
		t.Fatalf("started %d awk/sed processes for one job; want only the domain parser", n)
	}
}
