package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// notifyRecord is the test binary playing a notification command: it appends
// what it was told to file, one line per run.
func notifyRecord(file string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		os.Exit(1)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s->%s\n", os.Getenv("HOSTDECK_ID"), os.Getenv("HOSTDECK_FROM"), os.Getenv("HOSTDECK_TO"))
}

// downService is a plugin whose card is read every second from a service that
// does not answer, and the notifications.json that watches it.
func downService(t *testing.T, dir string) (notifications []byte, runs string) {
	t.Helper()
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(svc.Close)
	writePlugin(t, dir, "svc", fmt.Sprintf("schema: 1\nname: Service\ncard:\n  interval: 1s\n  http:\n    url: %s\n  value: state\n", svc.URL))
	runs = filepath.Join(dir, "runs.txt")
	notifications, err := json.Marshal(map[string]any{"watch": []string{"svc"}, "command": []string{os.Args[0], "notify-record", runs}})
	if err != nil {
		t.Fatal(err)
	}
	return notifications, runs
}

func waitForRun(t *testing.T, runs, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		got, _ := os.ReadFile(runs)
		if strings.TrimSpace(string(got)) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the notification command was told %q within 20s, want %q", got, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestAWatchedCardThatStopsAnsweringRunsTheNotificationCommandWithNoPageOpen(t *testing.T) {
	dir := t.TempDir()
	notifications, runs := downService(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), notifications, 0o644); err != nil {
		t.Fatal(err)
	}
	startConsole(t, dir)
	waitForRun(t, runs, "svc ->unreadable")
}

func TestNotificationsWrittenWhileTheConsoleRunsAreReadByPluginReload(t *testing.T) {
	dir := t.TempDir()
	notifications, runs := downService(t, dir)
	url := startConsole(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "notifications.json"), notifications, 0o644); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url+"/api/plugins/reload", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", url)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reload status %d", response.StatusCode)
	}
	waitForRun(t, runs, "svc ->unreadable")
}
