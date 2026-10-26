package process

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--internal-task-worker" {
		os.Exit(Worker())
	}
	os.Exit(m.Run())
}

func TestTaskProgram(t *testing.T) {
	if os.Getenv("HOSTDECK_PROCESS_FIXTURE") == "" {
		return
	}
	if os.Getenv("HOSTDECK_PROCESS_BLOCK") == "1" {
		time.Sleep(time.Hour)
	}
	if err := os.WriteFile(os.Getenv("HOSTDECK_PROCESS_FIXTURE"), []byte("ran"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("stdout 中文\n")
	os.Stderr.WriteString("stderr line\n")
}

func TestAProgramStartsOnlyAfterItsProcessIdentityHasBeenSaved(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	env := append(os.Environ(), "HOSTDECK_PROCESS_FIXTURE="+marker)
	for _, refuse := range []bool{true, false} {
		t.Run(map[bool]string{true: "refused", false: "accepted"}[refuse], func(t *testing.T) {
			var out, errout bytes.Buffer
			var identity Identity
			result := Run(context.Background(), os.Args[0], Spec{Command: []string{os.Args[0], "-test.run=^TestTaskProgram$"}, Dir: t.TempDir(), Env: env, Token: "test-" + map[bool]string{true: "refused", false: "accepted"}[refuse]}, Hooks{Prepared: func(id Identity) error {
				identity = id
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("program already ran: %v", err)
				}
				if refuse {
					return errors.New("state write refused")
				}
				return nil
			}, Stdout: &out, Stderr: &errout})
			if identity.PID <= 0 {
				t.Fatal("no prepared identity")
			}
			if refuse {
				if result.Error == nil {
					t.Fatal("refusal succeeded")
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("refused program ran")
				}
			} else if result.Error != nil || result.Code == nil || *result.Code != 0 || !bytes.Contains(out.Bytes(), []byte("stdout 中文")) || !bytes.Contains(errout.Bytes(), []byte("stderr line")) {
				t.Fatalf("result %+v stdout %s stderr %s", result, out.String(), errout.String())
			}
			if !result.Clean {
				t.Fatal("process tree was not cleaned")
			}
		})
	}
}

func TestCancellationEndsThePreparedProgramAndItsWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := Run(ctx, os.Args[0], Spec{Command: []string{os.Args[0], "-test.run=^TestTaskProgram$"}, Dir: t.TempDir(), Env: append(os.Environ(), "HOSTDECK_PROCESS_FIXTURE=unused", "HOSTDECK_PROCESS_BLOCK=1"), Token: "test-cancel"}, Hooks{})
	if !errors.Is(result.Error, context.DeadlineExceeded) || !result.Clean {
		t.Fatalf("cancellation %+v", result)
	}
}
