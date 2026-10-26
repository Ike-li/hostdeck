package dockerview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T, rows []map[string]any) Runner {
	t.Helper()
	return func(ctx context.Context, c Command) ([]byte, error) {
		t.Helper()
		if slices.Contains(c.Args, "ls") {
			var ids []string
			for _, r := range rows {
				ids = append(ids, r["id"].(string))
			}
			return []byte(strings.Join(ids, "\n")), nil
		}
		if slices.Contains(c.Args, "inspect") {
			var out []byte
			for _, r := range rows {
				if !slices.Contains(c.Args, r["id"].(string)) {
					continue
				}
				b, _ := json.Marshal(r)
				out = append(out, append(b, '\n')...)
			}
			return out, nil
		}
		if slices.Contains(c.Args, "logs") {
			return []byte("stdout line\nstderr line\n"), nil
		}
		t.Fatalf("unexpected Docker command %v", c.Args)
		return nil, nil
	}
}

func containerRow(id int, state, health string, exit int) map[string]any {
	return map[string]any{"id": fmt.Sprintf("%064x", id), "name": fmt.Sprintf("/service-%d", id), "status": state, "health": health, "exitCode": exit, "image": "example/image:1", "created": "2026-10-26T00:00:00Z", "started": "2026-10-26T01:00:00Z", "restartCount": 2, "restartPolicy": map[string]any{"Name": "unless-stopped"}, "ports": map[string]any{"80/tcp": []map[string]string{{"HostIp": "127.0.0.1", "HostPort": "8080"}}}, "networks": map[string]any{"app-net": map[string]any{"IPAddress": "172.18.0.2"}}, "mounts": []map[string]any{{"Type": "bind", "Source": "/source 中文", "Destination": "/data", "RW": false}}, "path": "/bin/app", "args": []string{"--name", "a b"}}
}

func executeFixture(t *testing.T, op string, rows []map[string]any) map[string]any {
	t.Helper()
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "unix:///local/docker.sock")
	out, err := Execute(context.Background(), []string{"docker", op}, fixture(t, rows))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["error"] != nil {
		t.Fatalf("reported error %s", out)
	}
	return got
}

func TestContainerReadingsDistinguishHealthRuntimeStateAndAnEmptyEngine(t *testing.T) {
	rows := []map[string]any{containerRow(1, "running", "healthy", 0), containerRow(2, "exited", "", 0), containerRow(3, "exited", "", 7), containerRow(4, "paused", "", 0), containerRow(5, "running", "unhealthy", 0), containerRow(6, "created", "", 0), containerRow(7, "restarting", "", 1), containerRow(8, "dead", "", 1), containerRow(9, "running", "starting", 0)}
	got := executeFixture(t, "detail", rows)
	if got["presentation"] != "compact" {
		t.Fatalf("presentation %v", got)
	}
	items := got["items"].([]any)
	want := []string{"running", "stopped", "attention", "paused", "attention", "stopped", "attention", "attention", "running"}
	for i, raw := range items {
		it := raw.(map[string]any)
		if it["group"] != want[i] {
			t.Errorf("item %d group %v want %s", i, it, want[i])
		}
	}
	c := executeFixture(t, "card", rows)
	if c["value"] != "3" || c["status"] != "error" {
		t.Fatalf("card %+v", c)
	}
	empty := executeFixture(t, "detail", nil)
	if len(empty["items"].([]any)) != 0 {
		t.Fatalf("empty %+v", empty)
	}
}

func TestDockerFailuresNeverInventAnEmptyHealthyReading(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "unix:///local/docker.sock")
	for _, problem := range []error{exec.ErrNotFound, errors.New("fixture daemon unavailable")} {
		out, err := Execute(context.Background(), []string{"docker", "detail"}, func(context.Context, Command) ([]byte, error) { return nil, problem })
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		json.Unmarshal(out, &got)
		msg, ok := got["error"].(map[string]any)
		if !ok || msg["en"] == "" || msg["zh-CN"] == "" || got["items"] != nil {
			t.Fatalf("failure %s", out)
		}
	}
}

func TestDockerRefusesRemoteEndpointsBeforeAnyDaemonCommand(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "")
	for _, host := range []string{"ssh://host", "tcp://192.168.1.5:2375", "tcp://example.test:2375", "npipe:////server/pipe/docker_engine", "unix://remote/path", "tcp://user:secret@127.0.0.1:2375"} {
		t.Setenv("DOCKER_HOST", host)
		calls := 0
		out, err := Execute(context.Background(), []string{"docker", "detail"}, func(context.Context, Command) ([]byte, error) { calls++; return nil, nil })
		if err != nil || calls != 0 || !strings.Contains(string(out), `"error"`) {
			t.Errorf("host %q: %s, %v, calls %d", host, out, err, calls)
		}
		if strings.Contains(string(out), "secret") {
			t.Fatal("echoed endpoint credentials")
		}
	}
}

func TestDockerPinsTheValidatedEndpointAndStripsContextOverrides(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "local-desktop")
	t.Setenv("DOCKER_HOST", "ssh://ignored")
	var calls []Command
	out, err := Execute(context.Background(), []string{"docker", "detail"}, func(ctx context.Context, c Command) ([]byte, error) {
		calls = append(calls, c)
		if slices.Contains(c.Args, "context") {
			return []byte(`"npipe:////./pipe/docker_engine"`), nil
		}
		if !slices.Contains(c.Args, "npipe:////./pipe/docker_engine") {
			t.Fatalf("endpoint not pinned: %v", c.Args)
		}
		for _, entry := range c.Env {
			if strings.HasPrefix(entry, "DOCKER_CONTEXT=") || strings.HasPrefix(entry, "DOCKER_HOST=") {
				t.Fatalf("inherited override %q", entry)
			}
		}
		return nil, nil
	})
	if err != nil || len(calls) != 2 || strings.Contains(string(out), `"error"`) {
		t.Fatalf("calls %v output %s error %v", calls, out, err)
	}
}

func TestDockerRejectsMalformedOrIncompleteReadings(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "unix:///local/docker.sock")
	id := fmt.Sprintf("%064x", 1)
	for _, tc := range []struct{ list, inspect string }{{"not-an-id", ""}, {id + "\n" + id, ""}, {id, `{broken`}, {id, ``}, {id, `{"id":"` + id + `","name":"","status":"running"}`}} {
		out, err := Execute(context.Background(), []string{"docker", "detail"}, func(ctx context.Context, c Command) ([]byte, error) {
			if slices.Contains(c.Args, "ls") {
				return []byte(tc.list), nil
			}
			return []byte(tc.inspect), nil
		})
		if err != nil || !strings.Contains(string(out), `"error"`) {
			t.Fatalf("accepted malformed list/inspect: %s %v", out, err)
		}
	}
}

func TestContainerDetailsShowSelectedMetadataWithoutReadingEnvironmentVariables(t *testing.T) {
	row := containerRow(1, "running", "healthy", 0)
	data := executeFixture(t, "detail", []map[string]any{row})
	encoded, _ := json.Marshal(data)
	for _, want := range []string{"example/image:1", "127.0.0.1:8080", "80/tcp", "app-net", "/source 中文", "unless-stopped", "stdout line", "stderr line"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("missing %q: %s", want, encoded)
		}
	}
	if strings.Contains(inspectFormat, ".Config.Env") || strings.Contains(inspectFormat, ".State.Health.Log") {
		t.Fatal("requested unrelated environment or health-check log output")
	}
}

func TestDockerLogLimitsKeepUTF8ValidAndOptionalFailuresKeepContainers(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "unix:///local/docker.sock")
	var rows []map[string]any
	for i := 1; i <= 40; i++ {
		rows = append(rows, containerRow(i, "running", "", 0))
	}
	base := fixture(t, rows)
	out, err := Execute(context.Background(), []string{"docker", "detail"}, func(ctx context.Context, c Command) ([]byte, error) {
		if slices.Contains(c.Args, "logs") {
			if !c.Combined || c.Limit > logLimit+1 || !slices.Contains(c.Args, "20") {
				t.Fatalf("unbounded or incomplete log request %+v", c)
			}
			return append([]byte{0xff}, []byte(strings.Repeat("中文", 4000))...), nil
		}
		return base(ctx, c)
	})
	if err != nil {
		t.Fatal(err)
	}
	var v view
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.Items[0].Log, "中文") || !strings.Contains(v.Items[0].Log, "�") {
		t.Fatalf("lost valid log content after invalid UTF-8: %q", v.Items[0].Log)
	}
	bytes := 0
	for _, it := range v.Items {
		if len(it.Log) > logLimit {
			t.Fatalf("individual log bytes %d", len(it.Log))
		}
		bytes += len(it.Log)
	}
	if bytes > pageLogLimit {
		t.Fatalf("page log bytes %d", bytes)
	}
	out, err = Execute(context.Background(), []string{"docker", "detail"}, func(ctx context.Context, c Command) ([]byte, error) {
		if slices.Contains(c.Args, "logs") {
			return nil, errors.New("fixture logs unavailable")
		}
		return base(ctx, c)
	})
	if err != nil || !strings.Contains(string(out), "Could not read this container") {
		t.Fatalf("optional log failure %s %v", out, err)
	}
}

func TestCancelledDockerReadsDoNotStartCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := Execute(ctx, []string{"docker", "detail"}, func(context.Context, Command) ([]byte, error) { calls++; return nil, nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("cancelled read started %d commands, error %v", calls, err)
	}
}
