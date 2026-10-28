package listenerview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func decodeCard(t *testing.T, sockets []Socket, marks Marks) cardDoc {
	t.Helper()
	var got cardDoc
	if err := json.Unmarshal(Card(sockets, marks), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func decodeDetail(t *testing.T, sockets []Socket, marks Marks) detailDoc {
	t.Helper()
	var got detailDoc
	if err := json.Unmarshal(Detail(sockets, marks), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTheCardCountsEveryTCPListenerWithoutFilteringByShell(t *testing.T) {
	got := decodeCard(t, []Socket{
		{PID: 1, Port: 80, Addr: "0.0.0.0", Command: "httpd"},
		{PID: 50, Port: 4678, Addr: "127.0.0.1", Command: "hostdeck"},
		{PID: 90, Port: 3000, Addr: "127.0.0.1", Command: "node"},
	}, Marks{})
	if got.Error != nil {
		t.Fatalf("error %v", got.Error)
	}
	if got.Status != "" {
		t.Fatalf("status %q, want none", got.Status)
	}
	if got.Value != "3" {
		t.Fatalf("value %q, want 3", got.Value)
	}
	wantLabel := text{"en": "TCP listeners on this machine", "zh-CN": "本机的 TCP 监听端口"}
	if !reflect.DeepEqual(got.Label, wantLabel) {
		t.Fatalf("label %+v, want %+v", got.Label, wantLabel)
	}
}

func TestAnEmptyListeningTableIsAHonestZero(t *testing.T) {
	got := decodeCard(t, nil, Marks{})
	if got.Error != nil || got.Value != "0" {
		t.Fatalf("card %+v, want value 0 and no error", got)
	}
	want := text{"en": "No TCP listeners", "zh-CN": "没有 TCP 监听端口"}
	if !reflect.DeepEqual(got.Label, want) {
		t.Fatalf("label %+v, want %+v", got.Label, want)
	}
}

func TestDetailListsPortAddressPIDAndProcessNameAndIsSearchable(t *testing.T) {
	got := decodeDetail(t, []Socket{
		{PID: 90, Port: 3000, Addr: "127.0.0.1", Command: "node"},
		{PID: 1, Port: 80, Addr: "0.0.0.0", Command: "httpd"},
	}, Marks{})
	if got.Presentation != "compact" {
		t.Fatalf("presentation %q, want compact", got.Presentation)
	}
	if len(got.Groups) == 0 {
		t.Fatal("want groups so the page offers search")
	}
	if len(got.Items) != 2 {
		t.Fatalf("items %+v, want 2", got.Items)
	}
	// Sorted by port then address then pid.
	first, second := got.Items[0], got.Items[1]
	if first.Name != "httpd" || field(first, "Port") != "80" || field(first, "Address") != "0.0.0.0" || field(first, "PID") != "1" {
		t.Fatalf("first item %+v", first)
	}
	if second.Name != "node" || field(second, "Port") != "3000" || field(second, "Address") != "127.0.0.1" || field(second, "PID") != "90" {
		t.Fatalf("second item %+v", second)
	}
	if first.Group == "" || second.Group == "" {
		t.Fatal("every item needs a group id for search filters")
	}
}

func TestHostdeckItselfAndItsForwardedPortsAreMarkedButStillListed(t *testing.T) {
	got := decodeDetail(t, []Socket{
		{PID: 50, Port: 4678, Addr: "127.0.0.1", Command: "hostdeck"},
		{PID: 50, Port: 18080, Addr: "127.0.0.1", Command: "hostdeck"},
		{PID: 90, Port: 3000, Addr: "127.0.0.1", Command: "node"},
	}, Marks{SelfPID: 50, Forwarded: map[int]string{18080: "grafana"}})
	if len(got.Items) != 3 {
		t.Fatalf("items %d, want all three still listed", len(got.Items))
	}
	byPort := map[string]itemDoc{}
	for _, it := range got.Items {
		byPort[field(it, "Port")] = it
	}
	console := byPort["4678"]
	if mark := fieldText(console, "Mark"); mark["en"] != "hostdeck console" || mark["zh-CN"] != "hostdeck 控制台" {
		t.Fatalf("console mark %+v", console)
	}
	fwd := byPort["18080"]
	if mark := fieldText(fwd, "Mark"); mark["en"] != "hostdeck forwarding (grafana)" || mark["zh-CN"] != "hostdeck 转发（grafana）" {
		t.Fatalf("forward mark %+v", fwd)
	}
	other := byPort["3000"]
	if field(other, "Mark") != "" || fieldText(other, "Mark") != nil {
		t.Fatalf("unrelated listener should have no mark: %+v", other)
	}
}

func TestAStarBindFromLsofIsShownAsAllInterfaces(t *testing.T) {
	addr, port, err := parseListenName("*:8080")
	if err != nil || addr != "0.0.0.0" || port != 8080 {
		t.Fatalf("parseListenName(*) = %q %d %v", addr, port, err)
	}
	addr, port, err = parseListenName("127.0.0.1:4678")
	if err != nil || addr != "127.0.0.1" || port != 4678 {
		t.Fatalf("parseListenName(loopback) = %q %d %v", addr, port, err)
	}
	addr, port, err = parseListenName("[::1]:443")
	if err != nil || addr != "::1" || port != 443 {
		t.Fatalf("parseListenName(::1) = %q %d %v", addr, port, err)
	}
	addr, port, err = parseListenName("[::]:80")
	if err != nil || addr != "::" || port != 80 {
		t.Fatalf("parseListenName(::) = %q %d %v", addr, port, err)
	}
}

func TestSnapshotKeepsEveryListenerIncludingOnesWithoutAShell(t *testing.T) {
	sockets, procs, err := snapshotFrom(
		commandOutcome{Out: []byte("p10\nchttpd\nn*:80\np90\ncnode\nn127.0.0.1:3000\n"), Exit: 0},
		commandOutcome{Out: []byte("   10     1       01:00:00 httpd\n   90     1       00:05:00 node\n"), Exit: 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 2 {
		t.Fatalf("procs %d", len(procs))
	}
	if len(sockets) != 2 {
		t.Fatalf("sockets %+v, want both httpd and node", sockets)
	}
}

func TestLsofExitOneWithEmptyStdoutIsAnEmptyListNotAnError(t *testing.T) {
	sockets, procs, err := snapshotFrom(
		commandOutcome{Exit: 1},
		commandOutcome{Out: []byte("    1     0       00:00:01 kernel\n"), Exit: 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 0 || len(procs) != 1 {
		t.Fatalf("sockets %d procs %d", len(sockets), len(procs))
	}
}

func TestAFailedReadingIsBilingualJSONWithNoHealthyZero(t *testing.T) {
	out, err := readingError(&readFailure{en: "Could not list listening ports.", zh: "读不到正在监听的端口。"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Error text   `json:"error"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Value != "" {
		t.Fatalf("value %q, want omitted so the card is not a healthy zero", got.Value)
	}
	if got.Error["en"] != "Could not list listening ports." || got.Error["zh-CN"] != "读不到正在监听的端口。" {
		t.Fatalf("error %+v", got.Error)
	}
}

func TestForwardedPortsAreReadFromPortsJSONBesideThePluginsFolder(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "listeners")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ports.json"), []byte(`{"grafana":18080,"wiki":18081}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadForwarded(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{18080: "grafana", 18081: "wiki"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("forwarded %+v, want %+v", got, want)
	}
}

func field(it itemDoc, name string) string {
	for _, f := range it.Fields {
		if f.Name["en"] != name {
			continue
		}
		switch v := f.Value.(type) {
		case string:
			return v
		case map[string]any:
			if s, ok := v["en"].(string); ok {
				return s
			}
		case text:
			return v["en"]
		}
	}
	return ""
}

func fieldText(it itemDoc, name string) text {
	for _, f := range it.Fields {
		if f.Name["en"] != name {
			continue
		}
		switch v := f.Value.(type) {
		case text:
			return v
		case map[string]any:
			out := text{}
			for k, raw := range v {
				if s, ok := raw.(string); ok {
					out[k] = s
				}
			}
			return out
		}
	}
	return nil
}

func TestDetailEnrichesListenersWithDirectoryAndCommand(t *testing.T) {
	got := decodeDetail(t, []Socket{
		{
			PID:       90,
			Port:      3000,
			Addr:      "127.0.0.1",
			Command:   "node",
			Dir:       "/Users/raylee/project",
			KnowsDir:  true,
			Args:      []string{"node", "server.js", "--port", "3000"},
			KnowsArgs: true,
		},
		{
			PID:       1,
			Port:      80,
			Addr:      "0.0.0.0",
			Command:   "httpd",
			KnowsDir:  false,
			KnowsArgs: false,
		},
	}, Marks{})
	if len(got.Items) != 2 {
		t.Fatalf("items %d, want 2", len(got.Items))
	}
	// Sorted by port: port 80 (httpd, PID 1) then port 3000 (node, PID 90)
	httpd := got.Items[0]
	node := got.Items[1]

	// Known dir and args
	if field(node, "Directory") != "/Users/raylee/project" {
		t.Fatalf("node Directory %q, want /Users/raylee/project", field(node, "Directory"))
	}
	if field(node, "Command") != "node server.js --port 3000" {
		t.Fatalf("node Command %q, want node server.js --port 3000", field(node, "Command"))
	}

	// Unknown dir and args fallback
	httpdDir := fieldText(httpd, "Directory")
	if httpdDir["en"] != "Could not read the working directory." || httpdDir["zh-CN"] != "读不到工作目录。" {
		t.Fatalf("httpd Directory fallback %+v", httpdDir)
	}
	httpdCmd := fieldText(httpd, "Command")
	if httpdCmd["en"] != "Could not read the command." || httpdCmd["zh-CN"] != "读不到启动命令。" {
		t.Fatalf("httpd Command fallback %+v", httpdCmd)
	}
}

func TestCommandTruncationAt512RunesWithBilingualEllipsis(t *testing.T) {
	longArg := strings.Repeat("a", 600)
	got := decodeDetail(t, []Socket{
		{
			PID:         42,
			Port:        8080,
			Addr:        "127.0.0.1",
			Command:     "app",
			CommandLine: "/usr/local/bin/app --flag=" + longArg,
			KnowsArgs:   true,
		},
	}, Marks{})
	if len(got.Items) != 1 {
		t.Fatalf("items %d", len(got.Items))
	}
	cmd := fieldText(got.Items[0], "Command")
	if cmd == nil {
		t.Fatalf("want bilingual truncated command, got string %q", field(got.Items[0], "Command"))
	}
	wantPrefix := ("/usr/local/bin/app --flag=" + longArg)[:512] + "…"
	if cmd["en"] != wantPrefix+" (truncated)" {
		t.Fatalf("cmd en %q, want %q", cmd["en"], wantPrefix+" (truncated)")
	}
	if cmd["zh-CN"] != wantPrefix+"（已截断）" {
		t.Fatalf("cmd zh-CN %q, want %q", cmd["zh-CN"], wantPrefix+"（已截断）")
	}
}

func TestEnrichSocketsCachesInspectionByPID(t *testing.T) {
	called := 0
	fakeInspect := func(pid int) inspection {
		called++
		return inspection{
			Dir:         "/var/run",
			KnowsDir:    true,
			CommandLine: "proxy",
			KnowsArgs:   true,
		}
	}
	sockets := []Socket{
		{PID: 100, Port: 80, Addr: "127.0.0.1", Command: "proxy"},
		{PID: 100, Port: 443, Addr: "127.0.0.1", Command: "proxy"},
		{PID: 200, Port: 8080, Addr: "127.0.0.1", Command: "web"},
	}
	enriched := enrichSockets(sockets, fakeInspect)
	if len(enriched) != 3 {
		t.Fatalf("sockets %d", len(enriched))
	}
	// PID 100 called once, PID 200 called once -> called == 2
	if called != 2 {
		t.Fatalf("inspect called %d times, want 2 (cached per PID)", called)
	}
	if !enriched[0].KnowsDir || enriched[0].Dir != "/var/run" || enriched[0].CommandLine != "proxy" {
		t.Fatalf("enriched[0] %+v", enriched[0])
	}
	if !enriched[1].KnowsDir || enriched[1].Dir != "/var/run" || enriched[1].CommandLine != "proxy" {
		t.Fatalf("enriched[1] %+v", enriched[1])
	}
}
