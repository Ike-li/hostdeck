package manualview

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Ike-li/hostdeck/internal/observation"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-service-collector" {
		os.Exit(observation.Collector())
	}
	if len(os.Args) > 1 && os.Args[1] == "hostdeck-manual-listen" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(1)
		}
		body := ln.Addr().String() + "\n" + strconv.Itoa(os.Getpid()) + "\n"
		if err := os.WriteFile(os.Args[2], []byte(body), 0o644); err != nil {
			os.Exit(1)
		}
		_, _ = ln.Accept()
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "hostdeck-manual-args" {
		if err := os.WriteFile(os.Args[2], []byte(strings.Join(os.Args, "\x00")), 0o644); err != nil {
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

func TestStopAndRestartOnlyTheListenerThisTestStarted(t *testing.T) {
	if testing.Short() {
		t.Skip("real process integration test")
	}
	op := osOperator{run: execCommand}
	pid, file := listenUnderShell(t, op)
	waitListed(t, op, pid)

	out, err := act(context.Background(), op, "stop", strconv.Itoa(pid))
	if err != nil {
		t.Fatal(err)
	}
	var stopped struct{ Message text }
	if err := json.Unmarshal(out, &stopped); err != nil || stopped.Message["en"] == "" {
		t.Fatalf("stop %s", out)
	}
	if op.Alive(pid) {
		t.Fatalf("pid %d still running after stop: %s", pid, out)
	}

	pid, file = listenUnderShell(t, op)
	waitListed(t, op, pid)
	store := observation.New(t.TempDir())
	id, _ := store.Create("manual", strconv.Itoa(pid), "restart", observation.Target{})
	ctx := observation.WithCapture(context.Background(), store.Root(), id)
	out, err = act(ctx, op, "restart", strconv.Itoa(pid))
	if err != nil {
		t.Fatal(err)
	}
	var restarted struct {
		Message text `json:"message"`
		Error   text `json:"error"`
	}
	if err := json.Unmarshal(out, &restarted); err != nil || restarted.Error != nil || restarted.Message["zh-CN"] == "" {
		t.Fatalf("restart %s", out)
	}
	if op.Alive(pid) {
		t.Fatalf("old pid %d still running", pid)
	}
	next := waitReplacement(t, file, pid)
	t.Cleanup(func() {
		_ = op.Kill(next)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			record, err := observation.ReadCapture(store.Root(), id)
			if err == nil && record.Worker.PID > 1 && !record.Worker.Equal(observation.ProcessIdentity(record.Worker.PID)) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Error("collector did not finish after temporary service cleanup")
	})
	if !op.Alive(next) {
		t.Fatalf("restarted pid %d is not running", next)
	}
	time.Sleep(200 * time.Millisecond)
	if !op.Alive(next) {
		t.Fatal("restarted process ended when the action returned")
	}
}

func listenUnderShell(t *testing.T, op osOperator) (int, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "addr")
	cmd := startShell(t, file)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopShell(cmd)
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(file)
		if err == nil && strings.Count(string(b), "\n") >= 2 {
			text = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if text == "" {
		t.Fatal("listener did not start")
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	pid, err := strconv.Atoi(lines[1])
	if err != nil || pid <= 1 {
		t.Fatalf("pid %q", lines[1])
	}
	t.Cleanup(func() { _ = op.Kill(pid) })
	return pid, file
}

func waitListed(t *testing.T, op osOperator, pid int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		listeners, procs, err := op.Snapshot(context.Background())
		if err == nil {
			if _, ok := findHit(selectHits(listeners, procs), pid); ok {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pid %d was not listed as a service started by hand", pid)
}

func waitReplacement(t *testing.T, file string, old int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(file)
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) >= 2 {
				pid, conv := strconv.Atoi(lines[1])
				if conv == nil && pid > 1 && pid != old {
					return pid
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("restarted listener did not write its pid")
	return 0
}

func execCommand(ctx context.Context, argv []string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	out, err := cmd.Output()
	if err == nil {
		return out, 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out, exit.ExitCode(), nil
	}
	return out, 0, err
}
