// Package manualview lists TCP listeners started from a shell. A daemon whose
// parent is launchd or a Windows service, and an app port with no shell in its
// ancestry, is left out: those keep running after the terminal closes.
package manualview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Listener is one process that is listening, named as the tool reported it.
// Ports may repeat; the card shows each port once.
type Listener struct {
	Command string
	Ports   []int
}

// Proc is one process in the tree. Uptime is how long it has been running.
type Proc struct {
	PPID   int
	Name   string
	Uptime time.Duration
}

type text map[string]string

func words(en, zh string) text { return text{"en": en, "zh-CN": zh} }

type cardDoc struct {
	Status string    `json:"status,omitempty"`
	Value  string    `json:"value"`
	Label  text      `json:"label"`
	Rows   []cardRow `json:"rows,omitempty"`
	Error  text      `json:"error,omitempty"`
}

type cardRow struct {
	Name  text `json:"name"`
	Value text `json:"value"`
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
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Group  string     `json:"group"`
	Value  text       `json:"value"`
	Fields []fieldDoc `json:"fields"`
}

type fieldDoc struct {
	Name  text `json:"name"`
	Value any  `json:"value"`
}

type hit struct {
	pid     int
	command string
	ports   []int
	uptime  time.Duration
	origin  origin
}

type origin struct {
	id    string
	label text
}

var shells = map[string]bool{
	"zsh": true, "bash": true, "sh": true, "fish": true, "dash": true,
	"cmd": true, "powershell": true, "pwsh": true,
}

var knownOrder = []string{"claude", "terminal", "iterm", "windows-terminal", "vscode", "powershell", "cmd"}

// Card is the overview card for these listeners.
func Card(listeners map[int]Listener, procs map[int]Proc) []byte {
	hits := selectHits(listeners, procs)
	doc := cardDoc{Value: strconv.Itoa(len(hits)), Label: words("No services started by hand", "没有手动启动的服务")}
	if len(hits) > 0 {
		doc.Label = words("Started by hand · they stop when that terminal or session closes", "手动启动的服务 · 关掉启动它的终端或会话就会停")
		doc.Rows = make([]cardRow, len(hits))
		for i, h := range hits {
			doc.Rows[i] = cardRow{Name: rowName(h.command, h.ports), Value: running(h.origin.label, h.uptime)}
		}
	}
	out, _ := json.Marshal(doc)
	return out
}

// Detail is the compact list of the same listeners. It offers no actions.
func Detail(listeners map[int]Listener, procs map[int]Proc) []byte {
	hits := selectHits(listeners, procs)
	doc := detailDoc{Items: []itemDoc{}}
	if len(hits) == 0 {
		out, _ := json.Marshal(doc)
		return out
	}
	doc.Presentation = "compact"
	labels := map[string]text{}
	for _, h := range hits {
		labels[h.origin.id] = h.origin.label
	}
	for _, id := range knownOrder {
		if label, ok := labels[id]; ok {
			doc.Groups = append(doc.Groups, groupDoc{ID: id, Label: label})
			delete(labels, id)
		}
	}
	var rest []string
	for id := range labels {
		rest = append(rest, id)
	}
	sort.Strings(rest)
	for _, id := range rest {
		doc.Groups = append(doc.Groups, groupDoc{ID: id, Label: labels[id]})
	}
	doc.Items = make([]itemDoc, len(hits))
	for i, h := range hits {
		doc.Items[i] = itemDoc{
			ID: strconv.Itoa(h.pid), Name: h.command, Group: h.origin.id,
			Value: running(h.origin.label, h.uptime),
			Fields: []fieldDoc{
				{Name: words("PID", "PID"), Value: strconv.Itoa(h.pid)},
				{Name: words("Ports", "端口"), Value: portList(h.ports)},
				{Name: words("Started from", "来源"), Value: h.origin.label},
				{Name: words("Running for", "已运行"), Value: uptimeText(h.uptime)},
			},
		}
	}
	out, _ := json.Marshal(doc)
	return out
}

func selectHits(listeners map[int]Listener, procs map[int]Proc) []hit {
	var hits []hit
	for pid, listener := range listeners {
		names := ancestors(pid, procs)
		if !hasShell(names) {
			continue
		}
		ports := uniquePorts(listener.Ports)
		if len(ports) == 0 {
			continue
		}
		proc, ok := procs[pid]
		if !ok {
			continue
		}
		command := strings.TrimSpace(listener.Command)
		if command == "" {
			command = strings.TrimSpace(proc.Name)
		}
		if command == "" {
			command = fmt.Sprintf("pid %d", pid)
		}
		hits = append(hits, hit{pid: pid, command: command, ports: ports, uptime: proc.Uptime, origin: classifyOrigin(names)})
	}
	sort.Slice(hits, func(i, j int) bool {
		a, b := strings.ToLower(hits[i].command), strings.ToLower(hits[j].command)
		if a != b {
			return a < b
		}
		return hits[i].pid < hits[j].pid
	})
	separateOrigins(hits)
	return hits
}

// separateOrigins gives two different sessions their own group when their
// names sanitize to the same id. The same session keeps one group.
func separateOrigins(hits []hit) {
	assigned := map[string]string{}
	used := map[string]bool{}
	for i := range hits {
		base := hits[i].origin.id
		label := hits[i].origin.label["en"] + "\n" + hits[i].origin.label["zh-CN"]
		key := base + "\n" + label
		if id, ok := assigned[key]; ok {
			hits[i].origin.id = id
			continue
		}
		id := base
		if used[id] {
			for n := 2; ; n++ {
				next := base + "-" + strconv.Itoa(n)
				if !used[next] {
					id = next
					break
				}
			}
		}
		used[id] = true
		assigned[key] = id
		hits[i].origin.id = id
	}
}

// ancestors walks from the parent toward pid 1. The listener itself is not
// included, so a shell only counts when it launched the listener.
func ancestors(pid int, procs map[int]Proc) []string {
	proc, ok := procs[pid]
	if !ok {
		return nil
	}
	pid = proc.PPID
	seen := map[int]bool{}
	var names []string
	for pid > 1 && !seen[pid] {
		parent, ok := procs[pid]
		if !ok {
			break
		}
		seen[pid] = true
		names = append(names, parent.Name)
		pid = parent.PPID
	}
	return names
}

func hasShell(names []string) bool {
	for _, name := range names {
		if shells[commandBase(name)] {
			return true
		}
	}
	return false
}

func commandBase(name string) string {
	name = strings.TrimPrefix(name, "-")
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}

func classifyOrigin(names []string) origin {
	for _, name := range names {
		if strings.Contains(strings.ToLower(name), "claude") {
			return origin{"claude", words("Claude session", "Claude 会话")}
		}
	}
	for _, name := range names {
		if name == "Terminal" {
			return origin{"terminal", words("Terminal", "终端")}
		}
	}
	for _, name := range names {
		if name == "iTerm2" {
			return origin{"iterm", words("iTerm", "iTerm")}
		}
	}
	for _, name := range names {
		base := commandBase(name)
		if base == "windowsterminal" || base == "wt" {
			return origin{"windows-terminal", words("Windows Terminal", "Windows 终端")}
		}
	}
	for _, name := range names {
		if vscode(name) {
			return origin{"vscode", words("VS Code", "VS Code")}
		}
	}
	for _, name := range names {
		base := commandBase(name)
		if base == "powershell" || base == "pwsh" {
			return origin{"powershell", words("PowerShell", "PowerShell")}
		}
	}
	for _, name := range names {
		if commandBase(name) == "cmd" {
			return origin{"cmd", words("Command Prompt", "命令提示符")}
		}
	}
	name := names[len(names)-1]
	return origin{otherID(name), words(name, name)}
}

func vscode(name string) bool {
	if strings.HasPrefix(name, "Code") || name == "Electron" {
		return true
	}
	base := commandBase(name)
	return base == "electron" || base == "code" || strings.HasPrefix(base, "code ") || strings.HasPrefix(base, "code-")
}

func otherID(name string) string {
	var b strings.Builder
	b.WriteString("other-")
	dash := true
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r > unicode.MaxASCII {
				continue
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	id := strings.TrimRight(b.String(), "-")
	if id == "other" {
		return "other-process"
	}
	return id
}

func uniquePorts(ports []int) []int {
	sort.Ints(ports)
	var out []int
	for _, port := range ports {
		if len(out) == 0 || out[len(out)-1] != port {
			out = append(out, port)
		}
	}
	return out
}

func portList(ports []int) string {
	parts := make([]string, len(ports))
	for i, port := range ports {
		parts[i] = strconv.Itoa(port)
	}
	return strings.Join(parts, ", ")
}

func rowName(command string, ports []int) text {
	list := portList(ports)
	if len(ports) == 1 {
		return words(command+" (port "+list+")", command+"（端口 "+list+"）")
	}
	return words(command+" (ports "+list+")", command+"（端口 "+list+"）")
}

func running(label text, up time.Duration) text {
	clock := uptimeText(up)
	return words(label["en"]+" · up "+clock["en"], label["zh-CN"]+" · 已运行 "+clock["zh-CN"])
}

func uptimeText(up time.Duration) text {
	if up < 0 {
		up = 0
	}
	secs := int(up / time.Second)
	days, secs := secs/86400, secs%86400
	hours, secs := secs/3600, secs%3600
	mins := secs / 60
	switch {
	case days > 0:
		return words(plural(days, "day", "days")+" "+plural(hours, "hour", "hours"), fmt.Sprintf("%d 天 %d 小时", days, hours))
	case hours > 0:
		return words(plural(hours, "hour", "hours")+fmt.Sprintf(" %d min", mins), fmt.Sprintf("%d 小时 %d 分", hours, mins))
	default:
		return words(fmt.Sprintf("%d min", mins), fmt.Sprintf("%d 分", mins))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
