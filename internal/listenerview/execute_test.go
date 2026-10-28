package listenerview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/run"
)

func init() {
	run.Register("listeners", Execute)
}

func TestExecuteCardAndDetailMarkForwardedPortsFromPortsJSON(t *testing.T) {
	if !unixReader {
		t.Skip("windows read path does not use the command runner")
	}
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "listeners")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ports.json"), []byte(`{"app":3000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd := func(ctx context.Context, cmd run.Command) ([]byte, error) {
		argv := cmd.Args
		switch {
		case len(argv) > 0 && strings.Contains(argv[0], "lsof"):
			return []byte("p42\ncnode\nn127.0.0.1:3000\n"), nil
		case len(argv) > 0 && strings.Contains(argv[0], "ps"):
			return []byte("   42     1       00:01:00 node\n"), nil
		default:
			return nil, errors.New("unexpected " + strings.Join(argv, " "))
		}
	}
	out, err := Execute(context.Background(), pluginDir, []string{"listeners", "card"}, runCmd)
	if err != nil {
		t.Fatal(err)
	}
	var card cardDoc
	if err := json.Unmarshal(out, &card); err != nil {
		t.Fatal(err)
	}
	if card.Error != nil || card.Value != "1" {
		t.Fatalf("card %+v", card)
	}
	out, err = Execute(context.Background(), pluginDir, []string{"listeners", "detail"}, runCmd)
	if err != nil {
		t.Fatal(err)
	}
	var detail detailDoc
	if err := json.Unmarshal(out, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 1 || fieldText(detail.Items[0], "Mark")["en"] != "hostdeck forwarding (app)" {
		t.Fatalf("detail %+v", detail.Items)
	}
	if fieldText(detail.Items[0], "Directory")["en"] != "Could not read the working directory." {
		t.Fatalf("detail Directory %+v", detail.Items[0].Fields)
	}
	if fieldText(detail.Items[0], "Command")["en"] != "Could not read the command." {
		t.Fatalf("detail Command %+v", detail.Items[0].Fields)
	}
}

func TestExecuteDetailEnrichesDirectoryAndCommand(t *testing.T) {
	if !unixReader {
		t.Skip("windows read path does not use the command runner")
	}
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "listeners")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runCmd := func(ctx context.Context, cmd run.Command) ([]byte, error) {
		argv := cmd.Args
		switch {
		case len(argv) > 0 && strings.Contains(argv[0], "lsof"):
			return []byte("p55\ncnode\nn127.0.0.1:3000\n"), nil
		case len(argv) > 0 && strings.Contains(argv[0], "ps"):
			return []byte("   55     1       00:01:00 node\n"), nil
		default:
			return nil, errors.New("unexpected " + strings.Join(argv, " "))
		}
	}
	oldInspector := defaultInspector
	defaultInspector = func(pid int) inspection {
		if pid == 55 {
			return inspection{
				Dir:         "/Users/raylee/app",
				KnowsDir:    true,
				CommandLine: "node server.js",
				KnowsArgs:   true,
			}
		}
		return inspection{}
	}
	defer func() { defaultInspector = oldInspector }()

	out, err := Execute(context.Background(), pluginDir, []string{"listeners", "detail"}, runCmd)
	if err != nil {
		t.Fatal(err)
	}
	var detail detailDoc
	if err := json.Unmarshal(out, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 1 {
		t.Fatalf("items %d, want 1", len(detail.Items))
	}
	it := detail.Items[0]
	if field(it, "Directory") != "/Users/raylee/app" {
		t.Fatalf("Directory %q, want /Users/raylee/app", field(it, "Directory"))
	}
	if field(it, "Command") != "node server.js" {
		t.Fatalf("Command %q, want node server.js", field(it, "Command"))
	}
}

func TestUnknownListenersCommandIsAnError(t *testing.T) {
	_, err := Execute(context.Background(), t.TempDir(), []string{"listeners", "stop"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err %v", err)
	}
}

func TestExecuteActionRequiresFourArguments(t *testing.T) {
	for _, argv := range [][]string{
		{"listeners", "action"},
		{"listeners", "action", "stop"},
		{"listeners", "action", "stop", "42", "extra"},
	} {
		_, err := Execute(context.Background(), t.TempDir(), argv, nil)
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("argv %v err %v, want unknown command", argv, err)
		}
	}
}

func TestExecuteActionRefusesUnknownProcess(t *testing.T) {
	if !unixReader {
		t.Skip("windows read path does not use the command runner")
	}
	runCmd := func(ctx context.Context, cmd run.Command) ([]byte, error) {
		return nil, nil
	}
	out, err := Execute(context.Background(), t.TempDir(), []string{"listeners", "action", "stop", "9999"}, runCmd)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Error text }
	if err := json.Unmarshal(out, &got); err != nil || got.Error["en"] == "" || got.Error["zh-CN"] == "" {
		t.Fatalf("out %s", out)
	}
}

func TestARealMachineCardIsACountOrBilingualErrorNeverBoth(t *testing.T) {
	if testing.Short() {
		t.Skip("real machine listen table")
	}
	dir := t.TempDir()
	out, err := run.Output(context.Background(), dir, []string{"@hostdeck", "listeners", "card"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Value string         `json:"value"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != nil && got.Value != "" {
		t.Fatalf("card %+v has both value and error", got)
	}
	if got.Error == nil && got.Value == "" {
		t.Fatalf("card %+v has neither", got)
	}
	if got.Error != nil {
		if got.Error["en"] == nil || got.Error["zh-CN"] == nil {
			t.Fatalf("error not bilingual: %+v", got.Error)
		}
	}
}
