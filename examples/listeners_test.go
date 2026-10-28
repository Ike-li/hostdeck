package examples

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Ike-li/hostdeck/internal/run"
)

func TestTheListenersPluginOffersStopActionAndRejectsConsolePID(t *testing.T) {
	p := load(t, "listeners")
	if p.Actions.Exec.Command == nil {
		t.Fatal("listeners plugin must declare actions.exec.command")
	}
	wantCmd := []string{"@hostdeck", "listeners", "action"}
	if !slices.Equal(p.Actions.Exec.Args(), wantCmd) {
		t.Fatalf("actions args %v, want %v", p.Actions.Exec.Args(), wantCmd)
	}

	out, err := run.Output(context.Background(), p.Dir, append(p.Actions.Exec.Args(), "stop", "999999"))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	var got struct {
		Error map[string]string `json:"error"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if got.Error["en"] != "That process is no longer listed, so it was left alone." || got.Error["zh-CN"] != "这个进程已经不在列表里，所以没有动它。" {
		t.Fatalf("unexpected error response: %+v", got)
	}
}
