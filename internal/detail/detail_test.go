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
	case "detail-until":
		os.WriteFile(args[1]+".read", nil, 0o644)
		if _, err := os.Stat(args[1]); err == nil {
			fmt.Print(`{"items": [{"id": "svc", "name": "Service"}]}`)
		} else {
			fmt.Print(items)
		}
	case "slow-act":
		os.WriteFile(args[1]+".started", nil, 0o644)
		time.Sleep(time.Second)
		os.WriteFile(args[1], []byte(args[3]+" "+args[4]), 0o644)
	case "act-until":
		os.WriteFile(args[1]+".started", nil, 0o644)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(args[1] + ".release"); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.WriteFile(args[1], []byte(args[3]+" "+args[4]), 0o644)
	case "act":
		os.WriteFile(args[1], []byte(args[3]+" "+args[4]), 0o644)
		fmt.Print(args[2])
	case "fail":
		fmt.Fprintln(os.Stderr, "service not found")
		os.Exit(2)
	case "hang":
		time.Sleep(time.Hour)
	case "show-item":
		value := os.Getenv("HOSTDECK_DETAIL_ITEM")
		if value == "" {
			value = "unset"
		}
		if err := os.WriteFile(args[1], []byte(value), 0o644); err != nil {
			os.Exit(1)
		}
		fmt.Print(`{"items":[{"id":"a","name":"A","status":"ok"}]}`)
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

func TestReadItemPassesTheItemOnlyAsData(t *testing.T) {
	t.Setenv("HOSTDECK_FAKE_DETAIL", "1")
	seen := filepath.Join(t.TempDir(), "seen")
	p := plugin.Plugin{
		ID:     "svc",
		Dir:    t.TempDir(),
		Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{os.Args[0], "show-item", seen}}},
	}

	if got := Read(context.Background(), p); got.Error != nil {
		t.Fatal(got.Error)
	}
	if body, err := os.ReadFile(seen); err != nil || string(body) != "unset" {
		t.Fatalf("list read saw %q (%v), want the item variable unset", body, err)
	}
	t.Setenv("HOSTDECK_DETAIL_ITEM", "stale")
	if err := os.Remove(seen); err != nil {
		t.Fatal(err)
	}
	if got := ReadItem(context.Background(), p, "fresh"); got.Error != nil {
		t.Fatal(got.Error)
	}
	if body, err := os.ReadFile(seen); err != nil || string(body) != "fresh" {
		t.Fatalf("item read saw %q (%v), want the requested id to replace a stale one", body, err)
	}
	if err := os.Remove(seen); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"has\nline", "has\rline", strings.Repeat("a", 257)} {
		if got := ReadItem(context.Background(), p, id); got.Error == nil || got.Error.Code != "bad_request" {
			t.Fatalf("item %q: %+v, want bad_request", id, got.Error)
		}
		if _, err := os.Stat(seen); !os.IsNotExist(err) {
			t.Fatalf("rejected item %q ran the command", id)
		}
	}
}

func TestADetailListsTheItemsItsCommandPrints(t *testing.T) {
	p, _ := fakePlugin(t, items, "")

	got := Read(context.Background(), p)

	want := View{Items: []Item{
		{
			ID: "svc", Name: plugin.Text{ByLang: map[string]string{"en": "Service", "zh-CN": "服务"}}, Status: "warn", Value: Value{Text: plugin.Text{All: "3"}},
			Fields:  []Field{{Name: plugin.Text{All: "PID"}, Value: Value{Text: plugin.Text{All: "4242"}}}},
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
		{`{"items": [{"id": "a"}]}`, "command_output", `item "a" has no name`},
		{`{"items": [{"id": "a", "name": ""}]}`, "command_output", `item "a" has no name`},
		{`{"items": [{"id": "a", "name": {"en": "", "zh-CN": ""}}]}`, "command_output", `item "a" has no name`},
		{`{"error": "no services"}`, "reported", ""},
	} {
		p, _ := fakePlugin(t, tc.printed, "")
		got := Read(context.Background(), p)
		if got.Error == nil || got.Error.Code != tc.code || !strings.Contains(got.Error.Detail, tc.mention) || got.Items != nil {
			t.Errorf("%s: got %+v, want %s mentioning %q and no items", tc.printed, got, tc.code, tc.mention)
		}
	}
}

func TestADetailRequiresANonblankNameForEveryItem(t *testing.T) {
	for _, name := range []string{`null`, `""`, `{}`, `{"en":"","zh-CN":""}`, `" \t\n　"`, `{"zh-CN":"　"}`} {
		t.Run(name, func(t *testing.T) {
			printed := fmt.Sprintf(`{"items":[{"id":"valid","name":"Valid"},{"id":"blank","name":%s,"actions":[{"id":"restart","label":"Restart"}]}]}`, name)
			p, ran := fakePlugin(t, printed, "")
			got := Read(context.Background(), p)
			if got.Error == nil || got.Error.Code != "command_output" || got.Error.Detail != `item "blank" has no name` || got.Items != nil {
				t.Fatalf("got %+v, want a missing-name error and no items", got)
			}
			if _, err := Act(context.Background(), p, "blank", "restart"); err == nil || err.Code != "command_output" {
				t.Errorf("action error %+v, want command_output", err)
			}
			if _, err := os.Stat(ran); !os.IsNotExist(err) {
				t.Errorf("the action ran or could not be checked: %v", err)
			}
		})
	}
}

func TestADetailAcceptsANameInOneLanguage(t *testing.T) {
	for _, name := range []string{`"Service"`, `{"en":"Service"}`, `{"zh-CN":"服务"}`} {
		t.Run(name, func(t *testing.T) {
			p, _ := fakePlugin(t, fmt.Sprintf(`{"items":[{"id":"svc","name":%s}]}`, name), "")
			got := Read(context.Background(), p)
			if got.Error != nil || len(got.Items) != 1 || got.Items[0].ID != "svc" {
				t.Fatalf("got %+v, want the named item", got)
			}
		})
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
	defer cancel()
	canceled := make(chan struct{})
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := os.Stat(ran + ".started"); err == nil {
				cancel()
				close(canceled)
				return
			}
			select {
			case <-finished:
				return
			case <-deadline.C:
				cancel()
				return
			case <-tick.C:
			}
		}
	}()

	Act(ctx, p, "svc", "restart")
	select {
	case <-canceled:
	default:
		t.Fatal("the request never went away while its action was running")
	}

	if got, err := os.ReadFile(ran); err != nil || string(got) != "restart svc" {
		t.Errorf("the action did not finish: %q, %v", got, err)
	}
}

func TestConcurrentActionsWaitAndReReadBeforeRunning(t *testing.T) {
	p, ran := fakePlugin(t, items, "")
	p.Detail.Exec.Command = []string{os.Args[0], "detail-until", ran}
	p.Actions.Exec.Command[1] = "slow-act"

	firstErr := make(chan *string, 1)
	go func() {
		_, err := Act(context.Background(), p, "svc", "restart")
		if err != nil {
			s := err.Code
			firstErr <- &s
			return
		}
		firstErr <- nil
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ran + ".read"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first action did not read the detail within 10s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, secondErr := Act(context.Background(), p, "svc", "restart")
	select {
	case got := <-firstErr:
		if got != nil {
			t.Fatalf("first action error = %v, want nil", *got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first action did not finish within 10s")
	}
	if secondErr == nil || secondErr.Code != "not_offered" {
		t.Errorf("second action error = %+v, want not_offered after first action changed state", secondErr)
	}
}

func TestAnActionOnOnePluginDoesNotWaitForAnotherPlugin(t *testing.T) {
	first, firstRan := fakePlugin(t, items, "")
	first.ID = "first"
	first.Actions.Exec.Command[1] = "act-until"

	second, secondRan := fakePlugin(t, items, "")
	second.ID = "second"

	firstDone := make(chan *string, 1)
	go func() {
		_, err := Act(context.Background(), first, "svc", "restart")
		if err != nil {
			s := err.Code
			firstDone <- &s
			return
		}
		firstDone <- nil
	}()
	t.Cleanup(func() {
		_ = os.WriteFile(firstRan+".release", nil, 0o644)
		select {
		case <-firstDone:
		case <-time.After(10 * time.Second):
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(firstRan + ".started"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first plugin's action did not start within 10s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	secondDone := make(chan *string, 1)
	go func() {
		_, err := Act(context.Background(), second, "svc", "restart")
		if err != nil {
			s := err.Code
			secondDone <- &s
			return
		}
		secondDone <- nil
	}()
	select {
	case got := <-secondDone:
		if got != nil {
			t.Fatalf("second plugin action error = %v, want nil", *got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second plugin's action waited for the first plugin's action")
	}
	if got, err := os.ReadFile(secondRan); err != nil || string(got) != "restart svc" {
		t.Fatalf("second plugin action wrote %q (%v), want restart svc", got, err)
	}
	select {
	case <-firstDone:
		t.Fatal("the first plugin's action finished before it was released")
	default:
	}

	if err := os.WriteFile(firstRan+".release", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-firstDone:
		if got != nil {
			t.Fatalf("first plugin action error = %v, want nil", *got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first plugin's action did not finish within 10s")
	}
}

// jsonFields lists the fields of t as the plugin reference writes them, such
// as items[].actions[].label.
func jsonFields(t reflect.Type, prefix string) []string {
	var fields []string
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if ft := f.Type; ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct {
			fields = append(fields, jsonFields(ft.Elem(), prefix+name+"[].")...)
		} else {
			fields = append(fields, prefix+name)
		}
	}
	return fields
}

func TestThePluginReferenceNamesTheItemReadEnvironmentVariable(t *testing.T) {
	for _, doc := range []string{"PLUGINS.md", "PLUGINS.zh-CN.md", "ARCHITECTURE.md", "ARCHITECTURE.zh-CN.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "HOSTDECK_DETAIL_ITEM") {
			t.Errorf("docs/%s does not name HOSTDECK_DETAIL_ITEM", doc)
		}
	}
}

func TestThePluginReferenceNamesEveryFieldADetailProgramPrints(t *testing.T) {
	fields := append(jsonFields(reflect.TypeFor[Item](), "items[]."), "presentation", "note", "groups[].id", "groups[].label")
	for _, doc := range []string{"PLUGINS.md", "PLUGINS.zh-CN.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range fields {
			if !strings.Contains(string(data), "`"+field+"`") {
				t.Errorf("docs/%s does not name `%s`", doc, field)
			}
		}
	}
}
