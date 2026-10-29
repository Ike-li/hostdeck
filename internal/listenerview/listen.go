// Package listenerview lists every TCP listener on this machine for the
// built-in listeners plugin. Unlike manualview it does not filter by shell
// ancestry. S1 is read-only: no Stop or Restart.
package listenerview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/process"
)

// Socket is one TCP listen endpoint.
type Socket struct {
	PID         int
	Port        int
	Addr        string
	Command     string
	Dir         string
	KnowsDir    bool
	CommandLine string
	Args        []string
	KnowsArgs   bool
}

type inspection struct {
	Dir         string
	KnowsDir    bool
	CommandLine string
	Args        []string
	KnowsArgs   bool
	Started     time.Time
}

type inspector func(pid int) inspection

func enrichSockets(sockets []Socket, inspect inspector) []Socket {
	if inspect == nil {
		return sockets
	}
	pids := make([]int, 0, len(sockets))
	for _, socket := range sockets {
		pids = append(pids, socket.PID)
	}
	readings := process.ReadPIDs(pids, inspect)
	for i := range sockets {
		in := readings[sockets[i].PID]
		if in.KnowsDir {
			sockets[i].Dir = in.Dir
			sockets[i].KnowsDir = true
		}
		if in.KnowsArgs {
			sockets[i].CommandLine = in.CommandLine
			sockets[i].Args = in.Args
			sockets[i].KnowsArgs = true
		}
	}
	return sockets
}

// Proc is a process name from the platform snapshot.
type Proc struct {
	PPID    int
	Name    string
	Started time.Time
}

// Marks names the console itself and ports hostdeck is forwarding.
type Marks struct {
	SelfPID   int
	Forwarded map[int]string // port -> plugin id
}

type text map[string]string

func words(en, zh string) text { return text{"en": en, "zh-CN": zh} }

type cardDoc struct {
	Status string `json:"status,omitempty"`
	Value  string `json:"value,omitempty"`
	Label  text   `json:"label,omitempty"`
	Error  text   `json:"error,omitempty"`
}

type detailDoc struct {
	Presentation string     `json:"presentation,omitempty"`
	Groups       []groupDoc `json:"groups,omitempty"`
	Items        []itemDoc  `json:"items"`
}

type groupDoc struct {
	ID    string `json:"id"`
	Label text   `json:"label"`
}

type itemDoc struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Group   string      `json:"group"`
	Value   text        `json:"value"`
	Fields  []fieldDoc  `json:"fields"`
	Actions []actionDoc `json:"actions,omitempty"`
}

type actionDoc struct {
	ID    string `json:"id"`
	Label text   `json:"label"`
}

type fieldDoc struct {
	Name  text `json:"name"`
	Value any  `json:"value"`
}

// Card is the overview card: a count, or nothing but an error from readingError.
func Card(sockets []Socket, _ Marks) []byte {
	n := len(sockets)
	doc := cardDoc{Value: strconv.Itoa(n), Label: words("No TCP listeners", "没有 TCP 监听端口")}
	if n > 0 {
		doc.Label = words("TCP listeners on this machine", "本机的 TCP 监听端口")
	}
	out, _ := json.Marshal(doc)
	return out
}

// Detail is the compact searchable list of every TCP listener.
func Detail(sockets []Socket, marks Marks) []byte {
	items := make([]itemDoc, 0, len(sockets))
	usedGroups := map[string]bool{}
	sorted := append([]Socket(nil), sockets...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Addr != b.Addr {
			return a.Addr < b.Addr
		}
		if a.PID != b.PID {
			return a.PID < b.PID
		}
		return a.Command < b.Command
	})
	for _, s := range sorted {
		item := detailItem(s, marks)
		items = append(items, item)
		usedGroups[item.Group] = true
	}
	doc := detailDoc{Items: items}
	if len(items) == 0 {
		out, _ := json.Marshal(doc)
		return out
	}
	doc.Presentation = "compact"
	for _, g := range groupOrder {
		if usedGroups[g.id] {
			doc.Groups = append(doc.Groups, groupDoc{ID: g.id, Label: g.label})
		}
	}
	out, _ := json.Marshal(doc)
	return out
}

var groupOrder = []struct {
	id    string
	label text
}{
	{"hostdeck", words("hostdeck", "hostdeck")},
	{"loopback", words("Loopback", "本机回环")},
	{"all-interfaces", words("All interfaces", "全部网卡")},
	{"other", words("Other addresses", "其他地址")},
}

func detailItem(s Socket, marks Marks) itemDoc {
	name := strings.TrimSpace(s.Command)
	if name == "" {
		name = fmt.Sprintf("pid %d", s.PID)
	}
	group, mark := classify(s, marks)
	value := words(fmt.Sprintf("%s:%d", s.Addr, s.Port), fmt.Sprintf("%s:%d", s.Addr, s.Port))
	if mark != nil {
		value = words(mark["en"]+" · "+value["en"], mark["zh-CN"]+" · "+value["zh-CN"])
	}
	item := itemDoc{
		ID:    fmt.Sprintf("%d-%s-%d", s.PID, sanitizeID(s.Addr), s.Port),
		Name:  name,
		Group: group,
		Value: value,
		Fields: []fieldDoc{
			{Name: words("Port", "端口"), Value: strconv.Itoa(s.Port)},
			{Name: words("Address", "地址"), Value: s.Addr},
			{Name: words("PID", "PID"), Value: strconv.Itoa(s.PID)},
			{Name: words("Process", "进程"), Value: name},
			{Name: words("Directory", "目录"), Value: directoryValue(s)},
			{Name: words("Command", "命令"), Value: commandValue(s)},
		},
	}
	if mark != nil {
		item.Fields = append(item.Fields, fieldDoc{Name: words("Mark", "标记"), Value: mark})
	}
	if canStop(s, marks) {
		item.Actions = []actionDoc{{ID: "stop", Label: words("Stop", "结束")}}
	}
	return item
}

func canStop(s Socket, marks Marks) bool {
	if s.PID <= 1 {
		return false
	}
	if marks.SelfPID > 0 && s.PID == marks.SelfPID {
		return false
	}
	if _, ok := marks.Forwarded[s.Port]; ok {
		return false
	}
	return true
}

func directoryValue(s Socket) any {
	if s.KnowsDir && s.Dir != "" {
		return s.Dir
	}
	return words("Could not read the working directory.", "读不到工作目录。")
}

func commandValue(s Socket) any {
	if !s.KnowsArgs {
		return words("Could not read the command.", "读不到启动命令。")
	}
	cmd := s.CommandLine
	if cmd == "" {
		cmd = quoteArgs(s.Args)
	}
	if cmd == "" {
		return words("Could not read the command.", "读不到启动命令。")
	}
	const limit = 512
	r := []rune(cmd)
	if len(r) <= limit {
		return cmd
	}
	cut := string(r[:limit]) + "…"
	return words(cut+" (truncated)", cut+"（已截断）")
}

func quoteArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t\n\"\\") {
			parts[i] = strconv.Quote(arg)
		} else {
			parts[i] = arg
		}
	}
	return strings.Join(parts, " ")
}

func classify(s Socket, marks Marks) (string, text) {
	// Forwarded ports are held by the console process too; name the forwarding
	// role first so a plugin's port is not labelled only as the console.
	if id, ok := marks.Forwarded[s.Port]; ok {
		return "hostdeck", words("hostdeck forwarding ("+id+")", "hostdeck 转发（"+id+"）")
	}
	if marks.SelfPID > 0 && s.PID == marks.SelfPID {
		return "hostdeck", words("hostdeck console", "hostdeck 控制台")
	}
	switch s.Addr {
	case "127.0.0.1", "::1":
		return "loopback", nil
	case "0.0.0.0", "::":
		return "all-interfaces", nil
	default:
		return "other", nil
	}
}

func sanitizeID(addr string) string {
	var b strings.Builder
	for _, r := range addr {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "addr"
	}
	return id
}
