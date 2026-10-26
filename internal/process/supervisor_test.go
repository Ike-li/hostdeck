package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	if depth, _ := strconv.Atoi(os.Getenv("HOSTDECK_PROCESS_DEPTH")); depth > 0 {
		child := exec.Command(os.Args[0], "-test.run=^TestTaskProgram$")
		child.Env = append(os.Environ(), "HOSTDECK_PROCESS_DEPTH="+strconv.Itoa(depth-1))
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
	}
	if os.Getenv("HOSTDECK_PROCESS_DEPTH") == "0" {
		fmt.Fprintln(os.Stdout, "grandchild-ready")
		time.Sleep(time.Hour)
	}
	if want := os.Getenv("HOSTDECK_PROCESS_ARGUMENT"); want != "" {
		if os.Args[len(os.Args)-1] != want {
			t.Fatal("argument was split or expanded")
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		actual, err := os.Stat(cwd)
		if err != nil {
			t.Fatal(err)
		}
		wanted, err := os.Stat(os.Getenv("HOSTDECK_PROCESS_DIR"))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(actual, wanted) {
			t.Fatalf("working directory %s", cwd)
		}
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

func TestProgramLookupUsesTheTargetSystemsEnvironmentKeyCase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally treats environment keys without case")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "task-fixture")
	b, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, b, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "ran")
	var env []string
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if key != "PATH" && key != "Path" {
			env = append(env, v)
		}
	}
	env = append(env, "HOSTDECK_PROCESS_FIXTURE="+marker, "PATH="+dir, "Path="+filepath.Join(dir, "missing"))
	result := Run(context.Background(), os.Args[0], Spec{Command: []string{"task-fixture", "-test.run=^TestTaskProgram$"}, Dir: dir, Env: env, Token: "test-env-case"}, Hooks{})
	if result.Error != nil || result.Code == nil || *result.Code != 0 {
		t.Fatalf("case-sensitive lookup %+v", result)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
}

func TestArgumentsDirectoryAndEnvironmentReachTheProgramWithoutAShell(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	argument := "argument with spaces $HOME ; untouched"
	result := Run(context.Background(), os.Args[0], Spec{Command: []string{os.Args[0], "-test.run=^TestTaskProgram$", "--", argument}, Dir: dir, Env: append(os.Environ(), "HOSTDECK_PROCESS_FIXTURE="+filepath.Join(dir, "ran"), "HOSTDECK_PROCESS_ARGUMENT="+argument, "HOSTDECK_PROCESS_DIR="+resolved), Token: "test-arguments"}, Hooks{})
	if result.Error != nil || result.Code == nil || *result.Code != 0 {
		t.Fatalf("argument/directory result %+v", result)
	}
}

func TestCancellationCleansDescendantsAfterTheyHaveActuallyStarted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out bytes.Buffer
	result := Run(ctx, os.Args[0], Spec{Command: []string{os.Args[0], "-test.run=^TestTaskProgram$"}, Dir: t.TempDir(), Env: append(os.Environ(), "HOSTDECK_PROCESS_FIXTURE=unused", "HOSTDECK_PROCESS_DEPTH=2"), Token: "test-descendants"}, Hooks{Stdout: &out})
	if !strings.Contains(out.String(), "grandchild-ready") || !result.Clean || !errors.Is(result.Error, context.DeadlineExceeded) {
		t.Fatalf("descendants %+v %s", result, out.String())
	}
}
