package run_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/diskhistory"
	"github.com/Ike-li/hostdeck/internal/dockerview"
	"github.com/Ike-li/hostdeck/internal/manualview"
	"github.com/Ike-li/hostdeck/internal/run"
	"github.com/Ike-li/hostdeck/internal/tasks"
)

func init() {
	run.Register("tasks", tasks.Execute)
	run.Register("disk-history", diskhistory.Execute)
	run.Register("docker", dockerview.Execute)
	run.Register("manual", manualview.Execute)
}

func TestTheManualCardReadsThisMachineWithoutBecomingDiskHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("reads this machine's listening ports")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := run.Output(ctx, t.TempDir(), []string{"@hostdeck", "manual", "card"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Disk history") || strings.Contains(string(out), "磁盘变化") || strings.Contains(string(out), "unknown native plugin command") {
		t.Fatalf("fell through to disk history: %s", out)
	}
	var got struct {
		Value string            `json:"value"`
		Error map[string]string `json:"error"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output %s: %v", out, err)
	}
	if got.Error != nil {
		if got.Error["en"] == "" || got.Error["zh-CN"] == "" {
			t.Fatalf("error %+v", got.Error)
		}
		return
	}
	if _, err := strconv.Atoi(got.Value); err != nil {
		t.Fatalf("value %q, want a count or a bilingual error", got.Value)
	}
}

func TestNativeDockerErrorsSayTheLastDiagnosticOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	name := "docker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "unix:///fixture.sock")
	t.Setenv("HOSTDECK_FAKE", "fail")
	out, err := run.Output(context.Background(), dir, []string{"@hostdeck", "docker", "detail"})
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Error map[string]string `json:"error"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "zh-CN"} {
		if strings.Count(reply.Error[lang], "the actual problem") != 1 {
			t.Fatalf("%s repeats or loses the diagnostic: %s", lang, reply.Error[lang])
		}
	}
}
