package dockerview

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestContainerDetailsOfferStartOrRestartOnlyForEligibleStates(t *testing.T) {
	rows := []map[string]any{containerRow(1, "running", "", 0), containerRow(2, "running", "unhealthy", 0), containerRow(3, "created", "", 0), containerRow(4, "exited", "", 0), containerRow(5, "exited", "", 7), containerRow(6, "paused", "", 0), containerRow(7, "restarting", "", 0), containerRow(8, "removing", "", 0), containerRow(9, "dead", "", 1)}
	got := executeFixture(t, "detail", rows)
	raw, _ := json.Marshal(got)
	var v view
	json.Unmarshal(raw, &v)
	want := []string{"restart", "restart", "start", "start", "start", "", "", "", ""}
	for i, it := range v.Items {
		if want[i] == "" {
			if len(it.Actions) != 0 {
				t.Errorf("%s offers %+v", rows[i]["status"], it.Actions)
			}
			continue
		}
		if len(it.Actions) != 1 || it.Actions[0].ID != want[i] {
			t.Errorf("%s: %+v, want %s", rows[i]["status"], it.Actions, want[i])
		}
	}
}

func actionTarget(t *testing.T, state string) (string, []map[string]any) {
	t.Helper()
	rows := []map[string]any{containerRow(1, state, "", 0)}
	got := executeFixture(t, "detail", rows)
	id := got["items"].([]any)[0].(map[string]any)["id"].(string)
	return id, rows
}

func TestContainerActionsReinspectTheBoundFullIDBeforeChangingIt(t *testing.T) {
	for _, tc := range []struct{ state, action string }{{"running", "restart"}, {"created", "start"}, {"exited", "start"}} {
		t.Run(tc.state, func(t *testing.T) {
			id, rows := actionTarget(t, tc.state)
			base := fixture(t, rows)
			var calls []Command
			out, err := Execute(context.Background(), "", []string{"docker", "action", tc.action, id}, func(ctx context.Context, c Command) ([]byte, error) {
				calls = append(calls, c)
				if slices.Contains(c.Args, tc.action) {
					return []byte(rows[0]["id"].(string)), nil
				}
				return base(ctx, c)
			})
			if err != nil || !strings.Contains(string(out), `"message"`) {
				t.Fatalf("action %s %v", out, err)
			}
			if len(calls) != 2 || !slices.Contains(calls[0].Args, "inspect") || calls[1].Args[len(calls[1].Args)-1] != rows[0]["id"] || calls[1].Args[len(calls[1].Args)-2] != "--" {
				t.Fatalf("not a fresh full-ID action: %+v", calls)
			}
			if slices.Contains(calls[1].Args, "--timeout") {
				t.Fatal("overrode the container's stop timeout")
			}
		})
	}
}

func TestStaleContainerActionsAndChangedContextsNeverMutateAnything(t *testing.T) {
	for _, tc := range []struct{ before, after, action string }{{"running", "paused", "restart"}, {"running", "restarting", "restart"}, {"exited", "running", "start"}, {"exited", "dead", "start"}} {
		id, rows := actionTarget(t, tc.before)
		rows[0]["status"] = tc.after
		base := fixture(t, rows)
		mutations := 0
		out, err := Execute(context.Background(), "", []string{"docker", "action", tc.action, id}, func(ctx context.Context, c Command) ([]byte, error) {
			if slices.Contains(c.Args, "start") || slices.Contains(c.Args, "restart") {
				mutations++
				return nil, nil
			}
			return base(ctx, c)
		})
		if err != nil || mutations != 0 || !strings.Contains(string(out), `"error"`) {
			t.Fatalf("stale %s/%s: %s, %v, mutations %d", tc.before, tc.after, out, err, mutations)
		}
	}
	id, _ := actionTarget(t, "running")
	t.Setenv("DOCKER_HOST", "unix:///another/docker.sock")
	calls := 0
	out, err := Execute(context.Background(), "", []string{"docker", "action", "restart", id}, func(context.Context, Command) ([]byte, error) { calls++; return nil, nil })
	if err != nil || calls != 0 || !strings.Contains(string(out), `"error"`) {
		t.Fatalf("changed context: %s %v calls %d", out, err, calls)
	}
}

func TestInvalidDockerActionTargetsAreRejectedBeforeStartingACommand(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("a", 16) + ":--help", strings.Repeat("a", 16) + ":" + strings.Repeat("b", 64) + "\n", "container-name", "a:b:c"} {
		calls := 0
		out, err := Execute(context.Background(), "", []string{"docker", "action", "start", id}, func(context.Context, Command) ([]byte, error) { calls++; return nil, nil })
		if calls != 0 || err == nil && !strings.Contains(string(out), `"error"`) {
			t.Fatalf("id %q output %s error %v calls %d", id, out, err, calls)
		}
	}
}

func TestDockerActionFailuresAndTimeoutsNeverReportSuccess(t *testing.T) {
	id, rows := actionTarget(t, "running")
	base := fixture(t, rows)
	for _, problem := range []error{errors.New("fixture restart refused"), context.DeadlineExceeded} {
		out, err := Execute(context.Background(), "", []string{"docker", "action", "restart", id}, func(ctx context.Context, c Command) ([]byte, error) {
			if slices.Contains(c.Args, "restart") {
				return nil, problem
			}
			return base(ctx, c)
		})
		if err != nil || strings.Contains(string(out), `"message"`) || !strings.Contains(string(out), `"error"`) {
			t.Fatalf("failure %s %v", out, err)
		}
		if errors.Is(problem, context.DeadlineExceeded) && !strings.Contains(string(out), "may still") {
			t.Fatalf("timeout hid engine uncertainty: %s", out)
		}
	}
}
