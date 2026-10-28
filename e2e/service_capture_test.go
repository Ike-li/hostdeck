package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/observation"
	"github.com/chromedp/chromedp"
)

func manualCapture(args []string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	defer ln.Close()
	os.WriteFile(args[2], []byte(ln.Addr().String()+"\n"+strconv.Itoa(os.Getpid())+"\n"), 0600)
	fmt.Fprintln(os.Stdout, "early stdout")
	fmt.Fprintln(os.Stderr, "early stderr")
	emitted := false
	for {
		if _, err := os.Stat(args[2] + ".emit"); err == nil && !emitted {
			fmt.Fprintln(os.Stdout, "delayed output")
			emitted = true
		}
		if _, err := os.Stat(args[2] + ".exit"); err == nil {
			fmt.Fprintln(os.Stderr, "finished output")
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func observationConsole(t *testing.T, dir string) (*exec.Cmd, string, func()) {
	t.Helper()
	cmd := exec.Command(binary, "--addr", "127.0.0.1:0", "--dir", dir)
	observationConsoleGroup(cmd)
	output, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	wait := func() { once.Do(func() { cmd.Wait() }) }
	t.Cleanup(func() { cmd.Process.Kill(); wait() })
	found := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(output)
		for scan.Scan() {
			if match := consoleURL.FindStringSubmatch(scan.Text()); match != nil {
				select {
				case found <- match[1]:
				default:
				}
			}
		}
		close(found)
	}()
	select {
	case url := <-found:
		if url == "" {
			t.Fatal("console exited before readiness")
		}
		return cmd, url, wait
	case <-time.After(10 * time.Second):
		t.Fatal("console startup timed out")
	}
	return nil, "", wait
}
func TestCapturedServicesSurviveConsoleExitAndRetainTheirResultOnReopen(t *testing.T) {
	if testing.Short() {
		t.Skip("temporary service and console integration")
	}
	for _, graceful := range []bool{true, false} {
		t.Run(fmt.Sprint("graceful-", graceful), func(t *testing.T) {
			root := t.TempDir()
			plugins := filepath.Join(root, "plugins")
			os.MkdirAll(plugins, 0700)
			if _, err := catalog.Add(examples.Files, plugins, "manual"); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "service")
			shell := startCaptureShell(t, file)
			if err := shell.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stopManualShell(shell); shell.Wait() })
			deadline := time.Now().Add(5 * time.Second)
			var original int
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(file)
				parts := strings.Split(string(data), "\n")
				if len(parts) >= 2 {
					original, _ = strconv.Atoi(parts[1])
					if original > 1 {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			if original <= 1 {
				t.Fatal("temporary service not ready")
			}
			t.Cleanup(func() {
				if p, err := os.FindProcess(original); err == nil {
					p.Kill()
				}
			})
			console, url, wait := observationConsole(t, root)
			body := fmt.Sprintf(`{"item":%q,"action":"restart"}`, strconv.Itoa(original))
			req, _ := http.NewRequest("POST", url+"/api/action/manual", bytes.NewBufferString(body))
			req.Header.Set("Origin", url)
			client := &http.Client{Timeout: 40 * time.Second}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Observation string
				Message     any
				Error       any
			}
			json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if response.StatusCode != 200 || result.Observation == "" {
				t.Fatalf("restart %d %+v", response.StatusCode, result)
			}
			storage := filepath.Join(root, "data", "service-observations")
			record, err := observation.ReadCapture(storage, result.Observation)
			if err != nil || record.Identity.PID <= 1 || record.Identity.PID == original || record.Identity.Started.IsZero() {
				t.Fatalf("replacement %+v %v", record, err)
			}
			t.Cleanup(func() {
				if p, err := os.FindProcess(record.Identity.PID); err == nil {
					p.Kill()
				}
				if p, err := os.FindProcess(record.Worker.PID); err == nil {
					p.Kill()
				}
			})
			if graceful {
				if err = interruptObservationConsole(console); err != nil {
					t.Fatal(err)
				}
			} else {
				console.Process.Kill()
			}
			wait()
			if !record.Identity.Equal(observation.ProcessIdentity(record.Identity.PID)) {
				t.Fatal("console exit stopped service")
			}
			os.WriteFile(file+".emit", nil, 0600)
			deadline = time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				text, _ := observation.CaptureText(storage, result.Observation)
				if strings.Contains(text, "delayed output") {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			text, _ := observation.CaptureText(storage, result.Observation)
			if !strings.Contains(text, "early stdout") || !strings.Contains(text, "early stderr") || !strings.Contains(text, "delayed output") {
				t.Fatalf("capture after console exit: %q", text)
			}
			os.WriteFile(file+".exit", nil, 0600)
			deadline = time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				record, err = observation.ReadCapture(storage, result.Observation)
				if err == nil && record.ExitCode != nil {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if record.ExitCode == nil || *record.ExitCode != 0 {
				t.Fatalf("exit result %+v %v", record, err)
			}
			_, reopened, _ := observationConsole(t, root)
			req, _ = http.NewRequest("POST", reopened+"/api/observations/session/"+result.Observation+"/read", strings.NewReader("{}"))
			req.Header.Set("Origin", reopened)
			response, err = client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var observed observation.Snapshot
			json.NewDecoder(response.Body).Decode(&observed)
			response.Body.Close()
			if response.StatusCode != 200 || observed.State != "idle" || observed.ExitCode == nil || !strings.Contains(observed.Log, "finished output") {
				t.Fatalf("reopened %+v", observed)
			}
			ctx := openBrowser(t, "zh-CN")
			do(t, ctx, chromedp.Navigate(reopened+"#/manual"), chromedp.WaitVisible(chromedp.CSS(".recent-operations .observation summary")), chromedp.Click(chromedp.CSS(".recent-operations .observation summary")))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.recent-operations .observation-log')?.textContent.includes('finished output')`)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
