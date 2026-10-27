package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary also plays the plugins these tests run: HOSTDECK_FAKE says
// which one.
func TestMain(m *testing.M) {
	switch os.Getenv("HOSTDECK_FAKE") {
	case "":
		os.Exit(m.Run())
	case "print":
		fmt.Print(`{"value": "42"}`)
	case "mixed":
		fmt.Fprint(os.Stdout, "stdout line\n")
		fmt.Fprint(os.Stderr, "stderr line\n")
	case "environment":
		fmt.Fprint(os.Stdout, os.Getenv("HOSTDECK_CHILD_VALUE"))
	case "fail":
		fmt.Fprint(os.Stdout, "partial\n")
		fmt.Fprint(os.Stderr, "first line\nthe actual problem\n")
		os.Exit(3)
	case "hang":
		time.Sleep(time.Hour)
	case "spawn-and-hang":
		// A grandchild that keeps stdout open, as a shell's background job does.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "HOSTDECK_FAKE=heartbeat")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		time.Sleep(time.Hour)
	case "heartbeat":
		for i := 0; ; i++ {
			os.WriteFile(os.Getenv("HOSTDECK_BEAT"), []byte(fmt.Sprint(i)), 0o644)
			time.Sleep(50 * time.Millisecond)
		}
	}
	os.Exit(0)
}

func fake(t *testing.T, mode string) []string {
	t.Setenv("HOSTDECK_FAKE", mode)
	return []string{os.Args[0]}
}

func TestOutputIsWhatTheCommandPrints(t *testing.T) {
	out, err := Output(context.Background(), t.TempDir(), fake(t, "print"))
	if err != nil || string(out) != `{"value": "42"}` {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestAFailingCommandGivesItsExitCodeAndLastErrorLine(t *testing.T) {
	out, err := Output(context.Background(), t.TempDir(), fake(t, "fail"))
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 || exit.LastLine != "the actual problem" || string(out) != "partial\n" {
		t.Errorf("got %q, %v; want stdout kept, exit code 3 and the last line of stderr", out, err)
	}
}

func TestHostdeckDispatchesByRegisteredNameAndRejectsUnknownCommands(t *testing.T) {
	dir := t.TempDir()
	var gotDir string
	var gotArgs []string
	var hasRunner bool
	Register("test-native", func(_ context.Context, d string, args []string, r Runner) ([]byte, error) {
		gotDir = d
		gotArgs = append([]string(nil), args...)
		hasRunner = r != nil
		return []byte(`{"value":"ok"}`), nil
	})
	out, err := Output(context.Background(), dir, []string{"@hostdeck", "test-native", "card"})
	if err != nil || string(out) != `{"value":"ok"}` {
		t.Fatalf("Output(@hostdeck test-native card) = %q, %v", out, err)
	}
	if gotDir != dir || len(gotArgs) != 2 || gotArgs[0] != "test-native" || gotArgs[1] != "card" || !hasRunner {
		t.Fatalf("dispatched dir=%q args=%v hasRunner=%v", gotDir, gotArgs, hasRunner)
	}
	for _, argv := range [][]string{
		{"@hostdeck", "unknown", "card"},
		{"@hostdeck"},
	} {
		if out, err := Output(context.Background(), dir, argv); err == nil {
			t.Fatalf("Output(%v) succeeded with %s; want dispatch error", argv, out)
		}
	}
}

func TestACommandThatCannotStartSaysSo(t *testing.T) {
	_, err := Output(context.Background(), t.TempDir(), []string{filepath.Join(t.TempDir(), "missing")})
	var exit *ExitError
	if err == nil || errors.As(err, &exit) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want a start error", err)
	}
}

func TestAHungCommandIsStoppedAtTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Output(ctx, t.TempDir(), fake(t, "hang"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want the deadline", err)
	}
	if took := time.Since(start); took > 500*time.Millisecond+WaitDelay+2*time.Second {
		t.Errorf("returned after %v", took)
	}
}

// A grandchild holding stdout open would keep the wait going forever, and it
// would live on after the card gave up on it.
func TestAtTheDeadlineTheCommandsChildrenStopToo(t *testing.T) {
	beat := filepath.Join(t.TempDir(), "beat")
	t.Setenv("HOSTDECK_BEAT", beat)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := Output(ctx, t.TempDir(), fake(t, "spawn-and-hang"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want the deadline", err)
	}
	if took := time.Since(start); took > time.Second+WaitDelay+2*time.Second {
		t.Errorf("returned after %v", took)
	}
	before, err := os.ReadFile(beat)
	if err != nil {
		t.Fatalf("the grandchild never ran, so this test shows nothing: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if after, _ := os.ReadFile(beat); string(after) != string(before) {
		t.Errorf("the grandchild is still running: beat %s, then %s", before, after)
	}
}

func TestDecodeRejectsTrailingOutput(t *testing.T) {
	for _, out := range []string{
		`{"value": "42"} trailing`,
		"{\"value\": \"42\"}\n{\"value\": \"99\"}",
	} {
		var got struct {
			Value string `json:"value"`
		}
		var outErr *OutputError
		if err := Decode([]byte(out), &got); !errors.As(err, &outErr) {
			t.Errorf("Decode(%q) = %v, want *OutputError", out, err)
		}
	}
}

func TestDockerCommandOptionsMergeBothLogStreamsAndKeepEnvironmentScoped(t *testing.T) {
	command := fake(t, "mixed")
	out, err := external(context.Background(), Command{Dir: t.TempDir(), Args: command, Limit: 10, Combined: true})
	if err != nil || len(out) != 10 || string(out) != "stdout lin" {
		t.Fatalf("bounded combined log %q %v", out, err)
	}
	out, err = external(context.Background(), Command{Dir: t.TempDir(), Args: command, Limit: 1024, Combined: true})
	if err != nil || !strings.Contains(string(out), "stdout line") || !strings.Contains(string(out), "stderr line") {
		t.Fatalf("combined log %q %v", out, err)
	}
	t.Setenv("HOSTDECK_FAKE", "environment")
	t.Setenv("HOSTDECK_CHILD_VALUE", "parent")
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "HOSTDECK_CHILD_VALUE=") {
			env = append(env, e)
		}
	}
	env = append(env, "HOSTDECK_CHILD_VALUE=child")
	out, err = external(context.Background(), Command{Dir: t.TempDir(), Args: command, Env: env, Limit: 1024})
	if err != nil || string(out) != "child" || os.Getenv("HOSTDECK_CHILD_VALUE") != "parent" {
		t.Fatalf("scoped environment %q %v", out, err)
	}
}
