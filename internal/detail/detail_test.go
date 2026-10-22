package detail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

// With HOSTDECK_FAKE_DETAIL set, the test binary plays a plugin's commands:
// "print <text>", "act <file> <text> <action> <item>", which records the
// action in file and prints text, "slow-act", which notes in file.started
// that it started and records the action a second later, "fail" and "hang".
func TestMain(m *testing.M) {
	if os.Getenv("HOSTDECK_FAKE_DETAIL") == "" {
		os.Exit(m.Run())
	}
	switch args := os.Args[1:]; args[0] {
	case "print":
		fmt.Print(args[1])
	case "slow-act":
		os.WriteFile(args[1]+".started", nil, 0o644)
		time.Sleep(time.Second)
		os.WriteFile(args[1], []byte(args[3]+" "+args[4]), 0o644)
	case "act":
		os.WriteFile(args[1], []byte(args[3]+" "+args[4]), 0o644)
		fmt.Print(args[2])
	case "fail":
		fmt.Fprintln(os.Stderr, "service not found")
		os.Exit(2)
	case "hang":
		time.Sleep(time.Hour)
	}
	os.Exit(0)
}

const items = "\ufeff" + `{"items": [
	{"id": "svc", "name": {"en": "Service", "zh-CN": "服务"}, "status": "warn", "value": 3,
	 "fields": [{"name": "PID", "value": 4242}], "log": "line 1\nline 2",
	 "actions": [{"id": "restart", "label": {"en": "Restart", "zh-CN": "重启"}}]},
	{"id": "other", "name": "Other"}
]}`

// fakePlugin has a detail that prints detail and actions that record what
// they ran in the returned file and print printed.
func fakePlugin(t *testing.T, detail, printed string) (plugin.Plugin, string) {
	t.Setenv("HOSTDECK_FAKE_DETAIL", "1")
	ran := filepath.Join(t.TempDir(), "ran")
	bin := os.Args[0]
	return plugin.Plugin{
		ID:      "svc",
		Dir:     t.TempDir(),
		Name:    plugin.Text{All: "Services"},
		Detail:  &plugin.Detail{Exec: &plugin.Exec{Command: []string{bin, "print", detail}}},
		Actions: &plugin.Actions{Exec: &plugin.Exec{Command: []string{bin, "act", ran, printed}}},
	}, ran
}

func TestADetailListsTheItemsItsCommandPrints(t *testing.T) {
	p, _ := fakePlugin(t, items, "")

	got := Read(context.Background(), p)

	want := View{Items: []Item{
		{
			ID: "svc", Name: plugin.Text{ByLang: map[string]string{"en": "Service", "zh-CN": "服务"}}, Status: "warn", Value: "3",
			Fields:  []Field{{Name: plugin.Text{All: "PID"}, Value: "4242"}},
			Log:     "line 1\nline 2",
			Actions: []Action{{ID: "restart", Label: plugin.Text{ByLang: map[string]string{"en": "Restart", "zh-CN": "重启"}}}},
		},
		{ID: "other", Name: plugin.Text{All: "Other"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestABrokenDetailSaysWhy(t *testing.T) {
	for _, tc := range []struct{ printed, code, mention string }{
		{`{"items": [{"id": "a", "name": "A", "status": "purple"}]}`, "command_output", "purple"},
		{`{"items": [{"id": "a", "name": "A"}, {"name": {"en": "B"}}]}`, "command_output", "item 2 has no id"},
		{`{"items": [{"id": "a", "name": "A"}, {"id": "a", "name": "B"}]}`, "command_output", `"a"`},
		{`{"items": [{"id": "a", "name": "A", "actions": [{"label": "Go"}]}]}`, "command_output", "action"},
		{`{"error": "no services"}`, "reported", ""},
	} {
		p, _ := fakePlugin(t, tc.printed, "")
		got := Read(context.Background(), p)
		if got.Error == nil || got.Error.Code != tc.code || !strings.Contains(got.Error.Detail, tc.mention) || got.Items != nil {
			t.Errorf("%s: got %+v, want %s mentioning %q and no items", tc.printed, got, tc.code, tc.mention)
		}
	}
}

func TestAnOfferedActionRunsForThatItem(t *testing.T) {
	p, ran := fakePlugin(t, items, `{"message": {"en": "Restarted", "zh-CN": "已重启"}}`)

	message, err := Act(context.Background(), p, "svc", "restart")

	if err != nil {
		t.Fatalf("error %+v", err)
	}
	if want := (plugin.Text{ByLang: map[string]string{"en": "Restarted", "zh-CN": "已重启"}}); !reflect.DeepEqual(message, want) {
		t.Errorf("message %+v, want %+v", message, want)
	}
	if got, _ := os.ReadFile(ran); string(got) != "restart svc" {
		t.Errorf("the action command got %q, want restart svc", got)
	}
}

func TestAnActionMayPrintNothing(t *testing.T) {
	p, ran := fakePlugin(t, items, "")
	if message, err := Act(context.Background(), p, "svc", "restart"); err != nil || !message.IsZero() {
		t.Errorf("got %+v, %+v; want success without a message", message, err)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Error("the action did not run")
	}
}

// The detail is read again first: the page may be old, and only what the
// plugin offers now may run.
func TestActionsTheDetailDoesNotOfferDoNotRun(t *testing.T) {
	for _, tc := range []struct{ item, action string }{{"other", "restart"}, {"svc", "delete"}, {"gone", "restart"}} {
		p, ran := fakePlugin(t, items, "")
		_, err := Act(context.Background(), p, tc.item, tc.action)
		if err == nil || err.Code != "not_offered" {
			t.Errorf("%s on %s: error %+v, want not_offered", tc.action, tc.item, err)
		}
		if _, statErr := os.Stat(ran); statErr == nil {
			t.Errorf("%s on %s ran", tc.action, tc.item)
		}
	}
}

func TestAFailingOrHungActionSaysWhy(t *testing.T) {
	p, _ := fakePlugin(t, items, "")
	p.Actions.Exec.Command = []string{os.Args[0], "fail"}
	if _, err := Act(context.Background(), p, "svc", "restart"); err == nil || err.Code != "command_exit" || !strings.Contains(err.Detail, "service not found") {
		t.Errorf("failing action: %+v", err)
	}

	p.Actions.Exec.Command = []string{os.Args[0], "hang"}
	defer func(was time.Duration) { ActTimeout = was }(ActTimeout)
	ActTimeout = 500 * time.Millisecond
	if _, err := Act(context.Background(), p, "svc", "restart"); err == nil || err.Code != "command_timeout" {
		t.Errorf("hung action: %+v", err)
	}
}

// The page asking for an action may be closed or reloaded while the action
// runs; a restart stopped halfway could leave the service down.
func TestAnActionFinishesWhenItsRequestGoesAway(t *testing.T) {
	p, ran := fakePlugin(t, items, "")
	p.Actions.Exec.Command[1] = "slow-act"
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			if _, err := os.Stat(ran + ".started"); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	Act(ctx, p, "svc", "restart")

	if got, err := os.ReadFile(ran); err != nil || string(got) != "restart svc" {
		t.Errorf("the action did not finish: %q, %v", got, err)
	}
}
