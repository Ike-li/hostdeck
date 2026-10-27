package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/process"
)

func text(en, zh string) plugin.Text {
	return plugin.Text{ByLang: map[string]string{"en": en, "zh-CN": zh}}
}

type field struct {
	Name  plugin.Text `json:"name"`
	Value any         `json:"value"`
}
type action struct {
	ID    string      `json:"id"`
	Label plugin.Text `json:"label"`
}
type item struct {
	ID      string   `json:"id"`
	Name    any      `json:"name"`
	Group   string   `json:"group,omitempty"`
	Status  string   `json:"status,omitempty"`
	Value   any      `json:"value,omitempty"`
	Fields  []field  `json:"fields,omitempty"`
	Log     string   `json:"log,omitempty"`
	Actions []action `json:"actions,omitempty"`
}
type group struct {
	ID    string      `json:"id"`
	Label plugin.Text `json:"label"`
}

func Execute(ctx context.Context, dir string, args []string) ([]byte, error) {
	if len(args) == 4 && args[0] == "tasks" && args[1] == "action" {
		e := lookup(dir)
		if e == nil {
			return marshalAction(args[2], "", errors.New("task plugin is not active"))
		}
		id, err := e.action(args[2], args[3])
		return marshalAction(args[2], id, err)
	}
	if len(args) != 2 || args[0] != "tasks" || (args[1] != "card" && args[1] != "detail") {
		return nil, errors.New("unknown task operation")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e := lookup(dir); e != nil {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.viewLocked(args[1])
	}
	c, err := Load(dir)
	if err != nil {
		return json.Marshal(map[string]any{"error": text("Task configuration could not be read: "+err.Error(), "无法读取任务配置："+err.Error())})
	}
	stage := text("Plugin is not active in this console; configuration preview only.", "插件未在此控制台启用，当前只预览配置。")
	if args[1] == "card" {
		return json.Marshal(map[string]any{"status": "warn", "value": fmt.Sprint(len(c.Tasks)), "label": stage})
	}
	items := []item{}
	var note any
	if len(c.Tasks) == 0 {
		note = emptyNote(dir)
	}
	for _, t := range c.Tasks {
		schedule := ""
		if t.Schedule.Cron != nil {
			schedule = *t.Schedule.Cron + " · " + *t.Schedule.Timezone
		} else {
			schedule = *t.Schedule.Every
		}
		g := "ready"
		enabled := text("Enabled", "已启用")
		if !*t.Enabled {
			g = "disabled"
			enabled = text("Disabled", "已停用")
		}
		items = append(items, item{ID: t.ID, Name: t.Name, Group: g, Value: enabled, Fields: []field{{text("Schedule", "计划"), schedule}, {text("Command", "命令"), strings.Join(t.Exec.Command, " ")}, {text("Working directory", "工作目录"), t.Exec.Dir}, {text("Timeout", "超时"), t.Timeout}, {text("Availability", "可用性"), stage}}})
	}
	body := map[string]any{"presentation": "compact", "groups": []group{{"ready", text("Enabled", "已启用")}, {"disabled", text("Disabled", "已停用")}}, "items": items}
	if note != nil {
		body["note"] = note
	}
	return json.Marshal(body)
}

func (e *entry) viewLocked(op string) ([]byte, error) {
	if e.owner == nil && e.stateErr == nil {
		e.state, e.stateErr = loadState(e.root)
	}
	if !e.valid {
		return json.Marshal(map[string]any{"error": text("Task configuration could not be read: "+fmt.Sprint(e.configErr), "无法读取任务配置："+fmt.Sprint(e.configErr))})
	}
	items := []item{}
	var note any
	if len(e.cfg.Tasks) == 0 {
		note = emptyNote(e.dir)
	}
	runningCount, attention, paused := 0, 0, 0
	budget := 512 << 10
	for _, t := range e.cfg.Tasks {
		ts := e.state.Tasks[t.ID]
		g := "ready"
		status := ""
		value := text("Ready", "就绪")
		var last *Record
		for i := len(e.state.Runs) - 1; i >= 0; i-- {
			if e.state.Runs[i].TaskID == t.ID {
				last = &e.state.Runs[i]
				break
			}
		}
		if *t.Enabled && last != nil && (last.Status == "failed" || last.Status == "timed_out" || last.Status == "interrupted" || last.Status == "skipped" && last.Reason != nil && *last.Reason == "capacity") {
			g = "attention"
			status = "error"
			value = runStatus(last.Status)
			attention++
		}
		if !*t.Enabled {
			g = "disabled"
			status = ""
			value = text("Disabled", "已停用")
		} else if ts.Paused {
			g = "paused"
			status = ""
			value = text("Paused", "已暂停")
			paused++
		}
		if last != nil && activeStatus(last.Status) && e.running[t.ID] == nil {
			g = "running"
			value = text("Running in another console", "在其他控制台运行中")
			runningCount++
		}
		if e.running[t.ID] != nil {
			g = "running"
			status = "ok"
			value = text("Running", "运行中")
			runningCount++
		}
		schedule := ""
		if t.Schedule.Cron != nil {
			schedule = *t.Schedule.Cron + " · " + *t.Schedule.Timezone
		} else {
			schedule = *t.Schedule.Every
		}
		command := t.Exec.Command
		if runtime.GOOS == "windows" && t.Exec.Windows != nil {
			command = t.Exec.Windows
		}
		it := item{ID: t.ID, Name: t.Name, Group: g, Status: status, Value: value, Fields: []field{{text("Schedule", "计划"), schedule}, {text("Command", "命令"), strings.Join(command, " ")}, {text("Working directory", "工作目录"), t.Exec.Dir}, {text("Timeout", "超时"), t.Timeout}, {text("Availability", "可用性"), text("Automatic scheduling runs while hostdeck is running.", "hostdeck 运行期间自动调度。")}}}
		if current := e.running[t.ID]; current != nil {
			last = e.record(current.id)
			if current.ctx.Err() == nil {
				it.Actions = append(it.Actions, action{"stop:" + current.id, text("Stop", "停止")})
			}
		}
		if *t.Enabled && e.stateErr == nil && e.ownerErr == nil {
			if ts.Paused {
				clean := true
				for _, record := range e.state.Runs {
					if record.TaskID != t.ID {
						continue
					}
					identity, ok := e.state.Active[record.ID]
					if !ok {
						continue
					}
					if current := e.running[t.ID]; current != nil && current.id == record.ID {
						continue
					}
					if identity.PID != 0 && !process.Gone(identity) {
						clean = false
						it.Fields = append(it.Fields, field{text("Needs local inspection", "需要本地检查"), fmt.Sprintf("worker PID %d", identity.PID)})
					}
				}
				if clean {
					it.Actions = append(it.Actions, action{"resume", text("Resume", "恢复")})
				}
			} else {
				if e.running[t.ID] == nil {
					occupied := false
					for _, record := range e.state.Runs {
						if record.TaskID == t.ID && e.state.Active[record.ID].Token != "" {
							occupied = true
						}
					}
					if !occupied {
						it.Actions = append(it.Actions, action{"run", text("Run now", "立即运行")})
					}
				}
				it.Actions = append(it.Actions, action{"pause", text("Pause schedule", "暂停计划")})
			}
		}
		if next := e.next[t.ID]; !next.IsZero() && *t.Enabled && !ts.Paused && e.stateErr == nil && e.ownerErr == nil {
			it.Fields = append(it.Fields, field{text("Next run (UTC)", "下次运行（UTC）"), next.Format(time.RFC3339Nano)})
		}
		it.Fields = append(it.Fields, field{text("Missed occurrences", "错过次数"), ts.Missed})

		if last != nil {
			it.Fields = append(it.Fields, field{text("Last run", "最近运行"), last.AcceptedAt.Format(time.RFC3339)}, field{text("Run ID", "运行编号"), last.ID}, field{text("Result", "结果"), runStatus(last.Status)})
			it.Fields = append(it.Fields, field{text("Run revision", "运行修订"), last.Revision})
			if last.Reason != nil {
				it.Fields = append(it.Fields, field{text("Reason", "原因"), runReason(*last.Reason)})
			}
			if last.ExitCode != nil {
				it.Fields = append(it.Fields, field{text("Exit code", "退出码"), *last.ExitCode})
			}
			if last.Logs.StdoutTruncated || last.Logs.StderrTruncated {
				it.Fields = append(it.Fields, field{text("Captured output", "已捕获输出"), text("Truncated at 1 MiB per stream; the script output kept draining.", "每个输出流最多保留 1 MiB，超出后继续排空。")})
			}

		}
		var history strings.Builder
		count := 0
		for i := len(e.state.Runs) - 1; i >= 0 && count < 10; i-- {
			r := e.state.Runs[i]
			if r.TaskID != t.ID {
				continue
			}
			count++
			fmt.Fprintf(&history, "%s · %s · %s · %s\n", r.AcceptedAt.Format(time.RFC3339), r.Status, r.Trigger, r.ID)
			if r.Reason != nil {
				fmt.Fprintf(&history, "reason=%s ", *r.Reason)
			}
			if r.ExitCode != nil {
				fmt.Fprintf(&history, "exit=%d ", *r.ExitCode)
			}
			fmt.Fprintf(&history, "revision=%s\n", r.Revision)
			if r.Diagnostic != "" {
				fmt.Fprintln(&history, r.Diagnostic)
			}
		}
		if last != nil {
			for i, p := range []*string{last.Logs.Stdout, last.Logs.Stderr} {
				if p == nil {
					continue
				}
				preview := e.preview(*p)
				if preview != "" {
					fmt.Fprintf(&history, "\n%s (last 20 lines / 8 KiB)\n%s\n", []string{"stdout", "stderr"}[i], preview)
				}
			}
		}
		log := history.String()
		if len(log) > budget {
			log = strings.ToValidUTF8(log[:budget], "")
		}
		it.Log = log
		budget = max(0, budget-len(log))
		items = append(items, it)
	}
	for _, err := range []error{e.configErr, e.stateErr, e.ownerErr} {
		if err != nil {
			items = append(items, item{ID: "_diagnostic", Name: text("Task configuration", "任务配置"), Group: "attention", Status: "error", Fields: []field{{text("Needs attention", "需要处理"), err.Error()}, {text("Loaded configuration", "已加载配置"), text("The last valid configuration is retained.", "保留上一次有效配置。")}}})
			attention++
			break
		}
	}
	if op == "card" {
		status := "ok"
		if attention > 0 {
			status = "error"
		}
		return json.Marshal(map[string]any{"status": status, "value": fmt.Sprint(len(e.cfg.Tasks)), "label": text(fmt.Sprintf("%d running · %d need attention", runningCount, attention), fmt.Sprintf("%d 个运行中 · %d 个需要处理", runningCount, attention)), "rows": []map[string]any{{"name": text("Paused", "已暂停"), "value": paused}}})
	}
	body := map[string]any{"presentation": "compact", "groups": []group{{"running", text("Running", "运行中")}, {"attention", text("Needs attention", "需要处理")}, {"ready", text("Ready", "就绪")}, {"paused", text("Paused", "已暂停")}, {"disabled", text("Disabled", "已停用")}}, "items": items}
	if note != nil {
		body["note"] = note
	}
	return json.Marshal(body)
}

func runStatus(s string) plugin.Text {
	words := map[string][2]string{"starting": {"Starting", "启动中"}, "running": {"Running", "运行中"}, "succeeded": {"Succeeded", "成功"}, "failed": {"Failed", "失败"}, "timed_out": {"Timed out", "已超时"}, "cancelled": {"Cancelled", "已取消"}, "interrupted": {"Interrupted", "已中断"}, "skipped": {"Skipped", "已跳过"}}
	w := words[s]
	return text(w[0], w[1])
}
func (e *entry) preview(path string) string {
	f, err := e.root.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	start := max(int64(0), st.Size()-(8<<10))
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(f, 8<<10))
	if err != nil {
		return ""
	}
	s := strings.ToValidUTF8(string(b), "�")
	if len(s) > 8<<10 {
		s = strings.ToValidUTF8(s[:8<<10], "")
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}

func emptyNote(dir string) plugin.Text {
	config := filepath.Join(dir, "tasks.json")
	example := filepath.Join(dir, "tasks.example.json")
	return text(
		fmt.Sprintf("No tasks yet. Use Scripts to add a script file. To add a task, edit the configuration and reload this console. Configuration file: %s. Disabled example: %s.", config, example),
		fmt.Sprintf("还没有任务。可以先在「脚本」里写一个脚本。添加任务仍是编辑配置文件后重载。配置文件：%s。停用的示例：%s。", config, example),
	)
}
func runReason(reason string) plugin.Text {
	words := map[string][2]string{"exit": {"Nonzero exit", "非零退出"}, "start": {"Could not start", "无法启动"}, "environment": {"Missing environment reference", "缺少环境引用"}, "log_io": {"Log storage failed", "日志存储失败"}, "timeout": {"Deadline reached", "超过期限"}, "stop": {"Stopped by request", "已请求停止"}, "task_removed": {"Task removed", "任务已移除"}, "plugin_disabled": {"Plugin disabled", "插件已停用"}, "shutdown": {"Console stopped", "控制台已退出"}, "recovery": {"Uncertain outcome", "结果未确认"}, "cleanup": {"Process cleanup unconfirmed", "进程清理未确认"}, "overlap": {"Previous run still active", "上次运行尚未结束"}, "capacity": {"Four execution slots occupied", "四个执行名额已占用"}}
	w := words[reason]
	return text(w[0], w[1])
}
