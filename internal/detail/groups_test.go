package detail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestADetailKeepsItsPresentationAndNamedGroups(t *testing.T) {
	printed := `{"presentation":"compact","groups":[{"id":"running","label":{"en":"Running","zh-CN":"运行中"}}],"items":[{"id":"svc","name":"Service","group":"running"}]}`
	p, _ := fakePlugin(t, printed, "")
	got := Read(context.Background(), p)
	if got.Error != nil {
		t.Fatalf("reading: %+v", got.Error)
	}
	out, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(out), `"presentation":"compact"`) || !strings.Contains(string(out), `"group":"running"`) || !strings.Contains(string(out), `"zh-CN":"运行中"`) {
		t.Fatalf("output %s, error %v", out, err)
	}
}

func TestADetailRefusesAmbiguousOrUnknownGroups(t *testing.T) {
	for _, printed := range []string{
		`{"presentation":"table","items":[]}`,
		`{"groups":[{"id":"","label":"Empty"}],"items":[]}`,
		`{"groups":[{"id":"a","label":" "}],"items":[]}`,
		`{"groups":[{"id":"a","label":"A"},{"id":"a","label":"Again"}],"items":[]}`,
		`{"groups":[{"id":"a","label":"A"}],"items":[{"id":"svc","name":"Service","group":"missing"}]}`,
		`{"groups":[{"id":"a","label":"A"}],"items":[{"id":"svc","name":"Service"}]}`,
	} {
		p, _ := fakePlugin(t, printed, "")
		if got := Read(context.Background(), p); got.Error == nil || got.Error.Code != "command_output" {
			t.Errorf("%s: %+v", printed, got)
		}
	}
}
