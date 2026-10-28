package observation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-service-collector" {
		os.Exit(Collector())
	}
	if len(os.Args) > 2 && os.Args[1] == "observation-test-service" {
		os.WriteFile(os.Args[2], []byte(fmt.Sprint(os.Getpid())), 0600)
		fmt.Fprintln(os.Stdout, "first stdout")
		if secret := os.Getenv("TOKEN"); secret != "" {
			fmt.Fprintln(os.Stdout, "TOKEN "+secret)
		}
		fmt.Fprintln(os.Stderr, "first stderr")
		if len(os.Args) > 3 && os.Args[3] == "crash" {
			os.Exit(23)
		}
		for i := 0; i < 1000; i++ {
			fmt.Fprintln(os.Stdout, "delayed", i)
			time.Sleep(20 * time.Millisecond)
		}
		return
	}
	os.Exit(m.Run())
}
func TestADetachedCollectorKeepsOutputAndExitAfterItsControlPipeCloses(t *testing.T) {
	root := t.TempDir()
	id := token()
	ready := filepath.Join(t.TempDir(), "ready")
	spec := Launch{Executable: os.Args[0], Arguments: []string{os.Args[0], "observation-test-service", ready, "crash"}, Directory: t.TempDir(), Environment: []string{"TOKEN=do-not-save-this"}}
	prepared, err := Prepare(context.Background(), root, id, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitTestCollector(t, root, id) })
	record, err := prepared.Start()
	if err != nil {
		t.Fatal(err)
	}
	prepared.Close()
	if record.Identity.PID <= 1 {
		t.Fatalf("identity %+v", record)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err = ReadCapture(root, id)
		if err == nil && record.State == "exited" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if record.ExitCode == nil || *record.ExitCode != 23 {
		t.Fatalf("exit %+v %v", record, err)
	}
	text, err := CaptureText(root, id)
	if err != nil || !strings.Contains(text, "first stdout") || !strings.Contains(text, "first stderr") {
		t.Fatalf("log %q %v", text, err)
	}
	if strings.Contains(text, "do-not-save-this") {
		t.Fatal("launch credential leaked into captured output")
	}
	data, _ := json.Marshal(record)
	if strings.Contains(string(data), "do-not-save-this") || strings.Contains(string(data), "observation-test-service") {
		t.Fatal("persisted launch information")
	}
}
func TestCapturePreparationRejectsInvalidLaunchInformationBeforeAnyServiceIsStopped(t *testing.T) {
	for _, spec := range []Launch{{}, {Executable: "relative", Directory: t.TempDir()}, {Executable: os.Args[0], Arguments: []string{os.Args[0], "bad\x00arg"}, Directory: t.TempDir()}} {
		p, err := Prepare(context.Background(), t.TempDir(), token(), spec)
		if err == nil {
			p.Close()
			t.Fatal("accepted invalid launch")
		}
	}
}
func TestCaptureOutputRotatesToTwoFilesOfTwoMiB(t *testing.T) {
	root := t.TempDir()
	writer, err := newCaptureWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(strings.Repeat("a", 2<<20) + strings.Repeat("b", 2<<20) + strings.Repeat("c", 1<<20))
	if n, err := writer.Write(input); err != nil || n != len(input) {
		t.Fatalf("write %d %v", n, err)
	}
	writer.Close()
	one, _ := os.ReadFile(filepath.Join(root, "previous.log"))
	two, _ := os.ReadFile(filepath.Join(root, "output.log"))
	if len(one) != 2<<20 || len(two) != 1<<20 || one[0] != 'b' || two[0] != 'c' {
		t.Fatalf("rotation %d %d", len(one), len(two))
	}
}

func TestACollectorFailureIsNotReportedAsAHealthyService(t *testing.T) {
	root := t.TempDir()
	id := token()
	if err := reserveCapture(root, id, "service"); err != nil {
		t.Fatal(err)
	}
	record, _ := ReadCapture(root, id)
	record.State = "running"
	record.Identity = ProcessIdentity(os.Getpid())
	record.Worker = record.Identity
	record.Error = "capture storage failed"
	saveCapture(root, record)
	store := New(root)
	got, ok, err := store.CaptureTarget(id)
	if err != nil || !ok || got.State != "unknown" || got.Reason != "capture_failed" {
		t.Fatalf("failure %+v %v", got, err)
	}
}
func TestCaptureStorageFaultKeepsDrainingWithoutPretendingItWasSaved(t *testing.T) {
	writer, err := newCaptureWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writer.file.Close()
	if n, err := writer.Write([]byte("lost")); n != 4 || err != nil {
		t.Fatalf("drain %d %v", n, err)
	}
	if writer.Err() == nil {
		t.Fatal("storage failure was hidden")
	}
	if n, err := writer.Write([]byte("more")); n != 4 || err != nil {
		t.Fatalf("failed to keep draining %d %v", n, err)
	}
}

func TestClosingAnUncommittedCollectorNeverStartsTheService(t *testing.T) {
	root := t.TempDir()
	id := token()
	file := filepath.Join(t.TempDir(), "ready")
	prepared, err := Prepare(context.Background(), root, id, Launch{Executable: os.Args[0], Arguments: []string{os.Args[0], "observation-test-service", file}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	prepared.Close()
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("service started without authorization")
	}
	record, err := ReadCapture(root, id)
	if err != nil || record.State != "abandoned" {
		t.Fatalf("abandoned %+v %v", record, err)
	}
}
func TestAStartupFailureAfterPreparationIsRecordedWithoutRetry(t *testing.T) {
	root := t.TempDir()
	id := token()
	file := filepath.Join(t.TempDir(), "ready")
	exe := filepath.Join(t.TempDir(), "removed-program.exe")
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(exe, data, 0700); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), root, id, Launch{Executable: exe, Arguments: []string{exe, "observation-test-service", file}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err = os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitTestCollector(t, root, id) })
	record, err := prepared.Start()
	if err == nil || record.State != "failed" || record.Error == "" || record.Identity.PID != 0 {
		t.Fatalf("startup %+v %v", record, err)
	}
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("startup retried")
	}
}

func TestCredentialRedactionCoversValuesSplitAcrossOutputWrites(t *testing.T) {
	var out strings.Builder
	writer := newCredentialWriter(&out, []string{"API_KEY=secret-value", "PATH=/usr/bin"})
	writer.Write([]byte("before secret-"))
	writer.Write([]byte("value after"))
	writer.Flush()
	if got := out.String(); got != "before *** after" {
		t.Fatalf("redaction %q", got)
	}
}

func waitTestCollector(t *testing.T, root, id string) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := ReadCapture(root, id)
		if err == nil && record.Worker.PID > 1 && !record.Worker.Equal(ProcessIdentity(record.Worker.PID)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("temporary collector did not exit")
}
