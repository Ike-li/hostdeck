package manualview

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

const day = 24 * time.Hour

func terminal() map[int]Proc {
	return map[int]Proc{
		78071: {PPID: 78070, Name: "-zsh"},
		78070: {PPID: 600, Name: "login"},
		600:   {PPID: 1, Name: "Terminal"},
	}
}

func claude() map[int]Proc {
	return map[int]Proc{
		98223: {PPID: 98200, Name: "zsh"},
		98200: {PPID: 4100, Name: "claude"},
		4100:  {PPID: 1, Name: "Claude"},
	}
}

func with(base map[int]Proc, extra map[int]Proc) map[int]Proc {
	out := map[int]Proc{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func decodeCard(t *testing.T, listeners map[int]Listener, procs map[int]Proc) cardDoc {
	t.Helper()
	var got cardDoc
	if err := json.Unmarshal(Card(listeners, procs), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != nil {
		t.Fatalf("error %v", got.Error)
	}
	if got.Status != "" {
		t.Fatalf("status %q, want none", got.Status)
	}
	return got
}

func TestAServiceStartedInATerminalSaysWhereItCameFromAndForHowLong(t *testing.T) {
	got := decodeCard(t,
		map[int]Listener{78656: {Command: "biliup", Ports: []int{19159}}},
		with(terminal(), map[int]Proc{78656: {PPID: 78071, Name: "biliup", Uptime: day + 2*time.Hour + 3*time.Minute}}),
	)
	want := cardDoc{
		Value: "1",
		Label: text{"en": "Started by hand · they stop when that terminal or session closes", "zh-CN": "手动启动的服务 · 关掉启动它的终端或会话就会停"},
		Rows: []cardRow{{
			Name:  text{"en": "biliup (port 19159)", "zh-CN": "biliup（端口 19159）"},
			Value: text{"en": "Terminal · up 1 day 2 hours", "zh-CN": "终端 · 已运行 1 天 2 小时"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("card %+v, want %+v", got, want)
	}
}

func TestLaunchdServicesAndAppInternalsAreLeftOut(t *testing.T) {
	got := decodeCard(t,
		map[int]Listener{
			78193: {Command: "node", Ports: []int{3000}},
			18020: {Command: "WeChat", Ports: []int{14013}},
			89255: {Command: "Code Helper (Plugin)", Ports: []int{11135}},
		},
		map[int]Proc{
			78193: {PPID: 1, Name: "node", Uptime: 10 * time.Minute},
			18020: {PPID: 1, Name: "WeChat", Uptime: 10 * time.Minute},
			89255: {PPID: 89000, Name: "Code Helper (Plugin)", Uptime: 10 * time.Minute},
			89000: {PPID: 1, Name: "Electron", Uptime: 10 * time.Minute},
		},
	)
	want := cardDoc{Value: "0", Label: text{"en": "No services started by hand", "zh-CN": "没有手动启动的服务"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("card %+v, want %+v", got, want)
	}
}

func TestAServiceStartedByAClaudeSessionOrAnEditorSaysSo(t *testing.T) {
	cases := []struct {
		name      string
		pid       int
		command   string
		listeners map[int]Listener
		procs     map[int]Proc
		want      text
	}{
		{
			name: "claude", pid: 98224, command: "python",
			procs: with(claude(), map[int]Proc{98224: {PPID: 98223, Name: "python", Uptime: 5 * time.Minute}}),
			want:  text{"en": "Claude session · up 5 min", "zh-CN": "Claude 会话 · 已运行 5 分"},
		},
		{
			name: "iterm", pid: 5, command: "python",
			procs: map[int]Proc{
				5: {PPID: 4, Name: "python", Uptime: 2*time.Hour + 3*time.Minute},
				4: {PPID: 3, Name: "zsh"},
				3: {PPID: 1, Name: "iTerm2"},
			},
			want: text{"en": "iTerm · up 2 hours 3 min", "zh-CN": "iTerm · 已运行 2 小时 3 分"},
		},
		{
			name: "vscode", pid: 4, command: "node",
			procs: map[int]Proc{
				4: {PPID: 3, Name: "node", Uptime: 30 * time.Second},
				3: {PPID: 2, Name: "zsh"},
				2: {PPID: 1, Name: "Code Helper (Plugin)"},
			},
			want: text{"en": "VS Code · up 0 min", "zh-CN": "VS Code · 已运行 0 分"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeCard(t, map[int]Listener{tc.pid: {Command: tc.command, Ports: []int{7860}}}, tc.procs)
			if len(got.Rows) != 1 || !reflect.DeepEqual(got.Rows[0].Value, tc.want) {
				t.Fatalf("row %+v, want %v", got.Rows, tc.want)
			}
		})
	}
}

func TestOneRowPerProcessListsEveryPortInOrder(t *testing.T) {
	got := decodeCard(t,
		map[int]Listener{
			79919: {Command: "node", Ports: []int{33342, 33341, 33341}},
			78656: {Command: "biliup", Ports: []int{19159}},
			78657: {Command: "node", Ports: []int{4000}},
		},
		with(terminal(), map[int]Proc{
			79919: {PPID: 78071, Name: "node", Uptime: time.Minute},
			78656: {PPID: 78071, Name: "biliup", Uptime: time.Minute},
			78657: {PPID: 78071, Name: "node", Uptime: time.Minute},
		}),
	)
	var names []string
	for _, row := range got.Rows {
		names = append(names, row.Name["en"])
	}
	want := []string{"biliup (port 19159)", "node (port 4000)", "node (ports 33341, 33342)"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names %v, want %v", names, want)
	}
}

func TestAServiceThatExitsBetweenTheTwoReadsIsLeftOut(t *testing.T) {
	got := decodeCard(t,
		map[int]Listener{
			99001: {Command: "node", Ports: []int{3000}},
			78656: {Command: "biliup", Ports: []int{19159}},
		},
		with(terminal(), map[int]Proc{78656: {PPID: 78071, Name: "biliup", Uptime: time.Minute}}),
	)
	if len(got.Rows) != 1 || got.Rows[0].Name["en"] != "biliup (port 19159)" {
		t.Fatalf("rows %+v", got.Rows)
	}
}

func TestWindowsShellsAreServicesAndAServiceWithoutAShellIsNot(t *testing.T) {
	got := decodeCard(t,
		map[int]Listener{
			10: {Command: "python.exe", Ports: []int{8080}},
			4:  {Command: "node.exe", Ports: []int{3000}},
			7:  {Command: "svchost.exe", Ports: []int{135}},
		},
		map[int]Proc{
			10: {PPID: 9, Name: "python.exe", Uptime: time.Hour},
			9:  {PPID: 8, Name: "powershell.exe"},
			8:  {PPID: 1, Name: "WindowsTerminal.exe"},
			4:  {PPID: 3, Name: "node.exe", Uptime: 2 * time.Hour},
			3:  {PPID: 2, Name: "cmd.exe"},
			2:  {PPID: 1, Name: "explorer.exe"},
			7:  {PPID: 1, Name: "svchost.exe", Uptime: time.Hour},
		},
	)
	if got.Value != "2" || len(got.Rows) != 2 {
		t.Fatalf("card %+v, want the terminal and the command prompt only", got)
	}
	if got.Rows[0].Value["en"] != "Command Prompt · up 2 hours 0 min" || got.Rows[0].Value["zh-CN"] != "命令提示符 · 已运行 2 小时 0 分" {
		t.Fatalf("cmd row %+v", got.Rows[0])
	}
	if got.Rows[1].Name["zh-CN"] != "python.exe（端口 8080）" || got.Rows[1].Value["en"] != "Windows Terminal · up 1 hour 0 min" {
		t.Fatalf("terminal row %+v", got.Rows[1])
	}
}

func TestUnknownSessionsWhoseNamesSanitizeTogetherStayInSeparateGroups(t *testing.T) {
	raw := Detail(
		map[int]Listener{
			8: {Command: "node", Ports: []int{10}},
			5: {Command: "python", Ports: []int{9}},
		},
		map[int]Proc{
			8: {PPID: 7, Name: "node", Uptime: time.Minute},
			7: {PPID: 6, Name: "bash"},
			6: {PPID: 1, Name: "My-Term"},
			5: {PPID: 4, Name: "python", Uptime: time.Minute},
			4: {PPID: 3, Name: "zsh"},
			3: {PPID: 1, Name: "My Term"},
		},
	)
	var got struct {
		Groups []struct {
			ID    string            `json:"id"`
			Label map[string]string `json:"label"`
		} `json:"groups"`
		Items []struct {
			Name  string `json:"name"`
			Group string `json:"group"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 2 || len(got.Items) != 2 {
		t.Fatalf("detail %s", raw)
	}
	ids := map[string]string{}
	for _, g := range got.Groups {
		if g.ID == "" || g.ID == "all" || ids[g.ID] != "" {
			t.Fatalf("group %+v", got.Groups)
		}
		ids[g.ID] = g.Label["en"]
	}
	if ids["other-my-term"] == "" || ids["other-my-term-2"] == "" {
		t.Fatalf("groups %+v", got.Groups)
	}
	for _, it := range got.Items {
		want := "My-Term"
		if it.Name == "python" {
			want = "My Term"
		}
		if ids[it.Group] != want {
			t.Fatalf("item %+v, groups %+v", it, got.Groups)
		}
	}
}

func TestAnUnrecognisedSessionKeepsItsOldestAncestorName(t *testing.T) {
	got := decodeCard(t,
		map[int]Listener{5: {Command: "python", Ports: []int{9}}},
		map[int]Proc{
			5: {PPID: 4, Name: "python", Uptime: time.Minute},
			4: {PPID: 3, Name: "zsh"},
			3: {PPID: 1, Name: "My Term"},
		},
	)
	if got.Rows[0].Value["en"] != "My Term · up 1 min" || got.Rows[0].Value["zh-CN"] != "My Term · 已运行 1 分" {
		t.Fatalf("row %+v", got.Rows)
	}
}

func TestDetailsGroupByOriginAndOfferNoActions(t *testing.T) {
	raw := Detail(
		map[int]Listener{
			78656: {Command: "biliup", Ports: []int{19159}},
			98224: {Command: "python", Ports: []int{7860, 7860}},
		},
		with(terminal(), with(claude(), map[int]Proc{
			78656: {PPID: 78071, Name: "biliup", Uptime: day + 2*time.Hour},
			98224: {PPID: 98223, Name: "python", Uptime: 5 * time.Minute},
		})),
	)
	var got struct {
		Presentation string `json:"presentation"`
		Groups       []struct {
			ID    string            `json:"id"`
			Label map[string]string `json:"label"`
		} `json:"groups"`
		Items []struct {
			ID     string            `json:"id"`
			Name   string            `json:"name"`
			Group  string            `json:"group"`
			Value  map[string]string `json:"value"`
			Fields []struct {
				Name  map[string]string `json:"name"`
				Value json.RawMessage   `json:"value"`
			} `json:"fields"`
			Actions []struct{ ID string } `json:"actions"`
			Log     string                `json:"log"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Presentation != "compact" {
		t.Fatalf("presentation %q", got.Presentation)
	}
	if len(got.Groups) != 2 || got.Groups[0].ID != "claude" || got.Groups[0].Label["zh-CN"] != "Claude 会话" || got.Groups[1].ID != "terminal" {
		t.Fatalf("groups %+v", got.Groups)
	}
	if len(got.Items) != 2 || got.Items[0].Name != "biliup" || got.Items[0].Group != "terminal" || got.Items[0].ID != "78656" {
		t.Fatalf("items %+v", got.Items)
	}
	if got.Items[1].Group != "claude" || got.Items[1].Value["en"] != "Claude session · up 5 min" {
		t.Fatalf("claude item %+v", got.Items[1])
	}
	for _, it := range got.Items {
		if len(it.Actions) != 0 || it.Log != "" {
			t.Fatalf("item %+v has an action or a log", it)
		}
	}
	fields := map[string]string{}
	for _, f := range got.Items[0].Fields {
		fields[f.Name["en"]] = string(f.Value)
	}
	if fields["PID"] != `"78656"` || fields["Ports"] != `"19159"` || fields["Started from"] == "" || fields["Running for"] == "" {
		t.Fatalf("fields %v", fields)
	}
}

func TestNoServicesHaveAnEmptyDetail(t *testing.T) {
	var got struct {
		Items  []any `json:"items"`
		Groups []any `json:"groups"`
		Error  any   `json:"error"`
	}
	if err := json.Unmarshal(Detail(nil, nil), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 || len(got.Groups) != 0 || got.Error != nil {
		t.Fatalf("%+v", got)
	}
}

func TestLsofAndPsOutputBecomeListenersAndProcesses(t *testing.T) {
	lsof := "p78656\ncbiliup\nf10\nn127.0.0.1:19159\nf11\nn[::1]:19159\np78193\ncnode\nf13\nn*:3000\n"
	ps := "78656 78071 1-02:03:04 biliup          \n89255 89000 05:06 Code Helper (Plugin)\n    1     0 2-00:00:00 launchd         \n"
	listeners, procs, err := snapshotFrom(commandOutcome{Out: []byte(lsof)}, commandOutcome{Out: []byte(ps)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(listeners, map[int]Listener{
		78656: {Command: "biliup", Ports: []int{19159}},
		78193: {Command: "node", Ports: []int{3000}},
	}) {
		t.Fatalf("listeners %+v", listeners)
	}
	if procs[78656] != (Proc{PPID: 78071, Name: "biliup", Uptime: day + 2*time.Hour + 3*time.Minute + 4*time.Second}) {
		t.Fatalf("biliup %+v", procs[78656])
	}
	if procs[89255].Uptime != 5*time.Minute+6*time.Second || procs[1].Uptime != 2*day || procs[1].Name != "launchd" {
		t.Fatalf("procs %+v", procs)
	}
}

func TestLsofExitOneWithNoOutputIsAnEmptyListAndOtherFailuresAreErrors(t *testing.T) {
	ps := commandOutcome{Out: []byte("1 0 00:01 launchd\n")}
	listeners, _, err := snapshotFrom(commandOutcome{Exit: 1}, ps)
	if err != nil || len(listeners) != 0 {
		t.Fatalf("empty lsof: %+v %v", listeners, err)
	}
	listeners, _, err = snapshotFrom(commandOutcome{Out: []byte("p5\ncpython\nn127.0.0.1:9\n"), Exit: 1}, ps)
	if err != nil || listeners[5].Command != "python" || len(listeners[5].Ports) != 1 {
		t.Fatalf("lsof exit 1 with output: %+v %v", listeners, err)
	}
	_, _, err = snapshotFrom(commandOutcome{Exit: 2}, ps)
	var failure *readFailure
	if !errors.As(err, &failure) || failure.en != "Could not list listening ports (exit status 2)." || failure.zh != "读不到正在监听的端口（退出码 2）。" {
		t.Fatalf("lsof exit: %v", err)
	}
	_, _, err = snapshotFrom(commandOutcome{Err: errors.New("not found")}, ps)
	if !errors.As(err, &failure) || failure.en != "Could not list listening ports: lsof was not found." || failure.zh != "读不到正在监听的端口：找不到 lsof。" {
		t.Fatalf("missing lsof: %v", err)
	}
	_, _, err = snapshotFrom(commandOutcome{Exit: 1}, commandOutcome{Exit: 1})
	if !errors.As(err, &failure) || failure.en != "Could not list processes (exit status 1)." || failure.zh != "读不到进程列表（退出码 1）。" {
		t.Fatalf("ps exit: %v", err)
	}
	_, _, err = snapshotFrom(commandOutcome{Exit: 1}, commandOutcome{Err: errors.New("not found")})
	if !errors.As(err, &failure) || failure.en != "Could not list processes: ps was not found." {
		t.Fatalf("missing ps: %v", err)
	}
	_, _, err = snapshotFrom(commandOutcome{Exit: 1}, commandOutcome{Out: []byte("not a process table\n")})
	if !errors.As(err, &failure) || failure.en != "Could not list processes." {
		t.Fatalf("unreadable ps: %v", err)
	}
}

func TestAParentCycleDoesNotHangTheReading(t *testing.T) {
	got := decodeCard(t,
		map[int]Listener{8: {Command: "python", Ports: []int{9}}},
		map[int]Proc{
			8: {PPID: 7, Name: "python", Uptime: time.Minute},
			7: {PPID: 6, Name: "zsh"},
			6: {PPID: 7, Name: "Terminal"},
		},
	)
	if len(got.Rows) != 1 || got.Rows[0].Value["zh-CN"] != "终端 · 已运行 1 分" {
		t.Fatalf("row %+v", got.Rows)
	}
}

func TestTheMacOSReaderAsksForAbsoluteLsofAndPs(t *testing.T) {
	if !unixReader {
		t.Skip("Windows reads the TCP table instead of lsof")
	}
	var got [][]string
	_, _, err := readPlatform(context.Background(), func(ctx context.Context, argv []string) ([]byte, int, error) {
		got = append(got, append([]string(nil), argv...))
		if argv[0] == "/usr/sbin/lsof" {
			return nil, 1, nil
		}
		return []byte("1 0 00:01 launchd\n"), 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"/usr/sbin/lsof", "+c", "0", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcn"},
		{"/bin/ps", "-axo", "pid=,ppid=,etime=,ucomm="},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands %q", got)
	}
}

func TestIPv4AndIPv6ListenerTablesKeepThePortAndTheOwningPID(t *testing.T) {
	v4 := []byte{
		1, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0x4a, 0xd7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x40, 0x33, 0x01, 0,
	}
	rows, err := ipv4Listeners(v4)
	if err != nil || len(rows) != 1 || rows[0] != (tableRow{Port: 19159, PID: 78656}) {
		t.Fatalf("ipv4 %+v %v", rows, err)
	}
	v6 := make([]byte, 4+56)
	v6[0] = 1
	v6[24], v6[25] = 0x4a, 0xd7
	v6[56] = 42
	rows, err = ipv6Listeners(v6)
	if err != nil || len(rows) != 1 || rows[0] != (tableRow{Port: 19159, PID: 42}) {
		t.Fatalf("ipv6 %+v %v", rows, err)
	}
	if _, err := ipv4Listeners(v4[:8]); err == nil {
		t.Fatal("a short table was accepted")
	}
	rows, err = ipv4Listeners([]byte{0, 0, 0, 0})
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty %+v %v", rows, err)
	}
}
