package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Ike-li/hostdeck/internal/plugin"
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
	Name    string   `json:"name"`
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
	if len(args) != 2 || args[0] != "tasks" || (args[1] != "card" && args[1] != "detail") {
		return nil, errors.New("unknown task operation")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := Load(dir)
	if err != nil {
		return json.Marshal(map[string]any{"error": text("Task configuration could not be read: "+err.Error(), "无法读取任务配置："+err.Error())})
	}
	stage := text("Execution is not available yet; configuration preview only.", "尚未提供执行能力，当前只预览配置。")
	if args[1] == "card" {
		return json.Marshal(map[string]any{"status": "warn", "value": fmt.Sprint(len(c.Tasks)), "label": stage})
	}
	items := []item{}
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
	return json.Marshal(map[string]any{"presentation": "compact", "groups": []group{{"ready", text("Enabled", "已启用")}, {"disabled", text("Disabled", "已停用")}}, "items": items})
}
