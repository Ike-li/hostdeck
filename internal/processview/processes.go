// Package processview provides the read-only built-in process list.
package processview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/Ike-li/hostdeck/internal/run"
)

type proc struct {
	PID, PPID  int
	Name, User string
	CPU        *float64
	Memory     *uint64
}

type reader func(context.Context, string, run.Runner, bool) ([]proc, error)

type text map[string]string

func words(en, zh string) text { return text{"en": en, "zh-CN": zh} }

type field struct {
	Name  text `json:"name"`
	Value any  `json:"value"`
}
type item struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Group  string  `json:"group"`
	Value  string  `json:"value"`
	Fields []field `json:"fields"`
}
type group struct {
	ID    string `json:"id"`
	Label text   `json:"label"`
}

// Execute reads a fresh snapshot. It never starts, stops or changes a process.
func Execute(ctx context.Context, dir string, args []string, r run.Runner) ([]byte, error) {
	return execute(ctx, dir, args, r, readPlatform)
}

func execute(ctx context.Context, dir string, args []string, r run.Runner, read reader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 2 || args[0] != "processes" || (args[1] != "card" && args[1] != "detail") {
		return nil, errors.New("unknown processes command")
	}
	procs, err := read(ctx, dir, r, args[1] == "detail")
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return json.Marshal(struct {
			Error text `json:"error"`
		}{words("Could not read the process list. Refresh to try again.", "读不到进程列表，可刷新重试。")})
	}
	if args[1] == "card" {
		return json.Marshal(struct {
			Value string `json:"value"`
			Label text   `json:"label"`
		}{strconv.Itoa(len(procs)), words("Processes on this machine", "本机进程")})
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].PID < procs[j].PID })
	items := make([]item, 0, len(procs))
	for _, p := range procs {
		cpu, mem, user := any(words("Unavailable", "无法读取")), any(words("Unavailable", "无法读取")), any(words("Unavailable", "无法读取"))
		if p.CPU != nil {
			cpu = fmt.Sprintf("%.1f%%", *p.CPU)
		}
		if p.Memory != nil {
			mem = fmt.Sprintf("%.1f MiB", float64(*p.Memory)/(1<<20))
		}
		if p.User != "" {
			user = p.User
		}
		g := "other"
		if p.PID == os.Getpid() {
			g = "hostdeck"
		}
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("PID %d", p.PID)
		}
		items = append(items, item{ID: strconv.Itoa(p.PID), Name: name, Group: g, Value: fmt.Sprintf("PID %d", p.PID), Fields: []field{
			{words("PID", "PID"), strconv.Itoa(p.PID)},
			{words("Parent PID", "父进程 PID"), strconv.Itoa(p.PPID)},
			{words("User", "用户"), user},
			{words("CPU", "CPU"), cpu},
			{words("Memory", "内存"), mem},
		}})
	}
	return json.Marshal(struct {
		Presentation string  `json:"presentation"`
		Groups       []group `json:"groups"`
		Items        []item  `json:"items"`
	}{"compact", []group{{"hostdeck", words("hostdeck", "hostdeck")}, {"other", words("Other processes", "其他进程")}}, items})
}
