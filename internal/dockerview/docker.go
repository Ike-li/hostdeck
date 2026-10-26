// Package dockerview implements the native Docker card and detail programs.
// The command runner is the process boundary; daemon commands pin a validated
// local endpoint instead of following a context that may change during a read.
package dockerview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Command describes one bounded Docker CLI invocation, never a shell string.
type Command struct {
	Args     []string
	Env      []string
	Limit    int
	Combined bool
}

type Runner func(context.Context, Command) ([]byte, error)
type text map[string]string

func words(en, zh string) text { return text{"en": en, "zh-CN": zh} }

type field struct {
	Name  text `json:"name"`
	Value any  `json:"value"`
}
type group struct {
	ID    string `json:"id"`
	Label text   `json:"label"`
}
type action struct {
	ID    string `json:"id"`
	Label text   `json:"label"`
}
type item struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Group   string   `json:"group"`
	Status  string   `json:"status"`
	Value   text     `json:"value"`
	Fields  []field  `json:"fields"`
	Log     string   `json:"log,omitempty"`
	Actions []action `json:"actions,omitempty"`
}
type view struct {
	Presentation string  `json:"presentation"`
	Groups       []group `json:"groups"`
	Items        []item  `json:"items"`
}
type container struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Health        string `json:"health"`
	ExitCode      int    `json:"exitCode"`
	OOMKilled     bool   `json:"oomKilled"`
	Error         string `json:"error"`
	Image         string `json:"image"`
	Created       string `json:"created"`
	Started       string `json:"started"`
	Finished      string `json:"finished"`
	RestartCount  int    `json:"restartCount"`
	RestartPolicy struct {
		Name              string
		MaximumRetryCount int
	} `json:"restartPolicy"`
	Ports map[string][]struct {
		HostIp   string
		HostPort string
	} `json:"ports"`
	Networks map[string]json.RawMessage `json:"networks"`
	Mounts   []struct {
		Type        string
		Source      string
		Destination string
		RW          bool
	} `json:"mounts"`
	Path string   `json:"path"`
	Args []string `json:"args"`
}

const metadataLimit = 1 << 20
const logLimit = 16 << 10
const pageLogLimit = 512 << 10
const inspectFormat = `{"id":{{json .Id}},"name":{{json .Name}},"status":{{json .State.Status}},"health":{{if index .State "Health"}}{{json .State.Health.Status}}{{else}}""{{end}},"exitCode":{{json .State.ExitCode}},"oomKilled":{{json .State.OOMKilled}},"error":{{json .State.Error}},"image":{{json .Config.Image}},"created":{{json .Created}},"started":{{json .State.StartedAt}},"finished":{{json .State.FinishedAt}},"restartCount":{{json .RestartCount}},"restartPolicy":{{json .HostConfig.RestartPolicy}},"ports":{{json .NetworkSettings.Ports}},"networks":{{json .NetworkSettings.Networks}},"mounts":{{json .Mounts}},"path":{{json .Path}},"args":{{json .Args}}}`

type client struct {
	endpoint string
	env      []string
	run      Runner
}

func Execute(ctx context.Context, args []string, run Runner) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	read := len(args) == 2 && args[0] == "docker" && slices.Contains([]string{"card", "detail"}, args[1])
	act := len(args) == 4 && args[0] == "docker" && args[1] == "action" && slices.Contains([]string{"start", "restart"}, args[2])
	if !read && !act {
		return nil, errors.New("unknown native Docker command")
	}
	fail := func(err error) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return json.Marshal(map[string]any{"error": words("Docker CLI was not found. Install Docker and make its CLI available to hostdeck.", "找不到 Docker 命令。请安装 Docker，并让 hostdeck 能找到它的命令行程序。")})
		}
		return json.Marshal(map[string]any{"error": words("Could not read Docker containers. Check that the local engine is running and accessible. "+err.Error(), "读不到 Docker 容器。请确认本机引擎已启动，且当前用户可以访问。"+err.Error())})
	}
	if act && !validTarget(args[3]) {
		return json.Marshal(map[string]any{"error": words("Invalid container action target.", "容器操作目标无效。")})
	}
	c, err := connect(ctx, run)
	if err != nil {
		return fail(err)
	}
	if act {
		return c.act(ctx, args[2], args[3])
	}
	rows, err := c.read(ctx)
	if err != nil {
		return fail(err)
	}
	if args[1] == "card" {
		return json.Marshal(card(rows))
	}
	v := view{Presentation: "compact", Groups: []group{{"running", words("Running", "运行中")}, {"stopped", words("Stopped", "已停止")}, {"paused", words("Paused", "已暂停")}, {"attention", words("Needs attention", "需要处理")}}, Items: make([]item, 0, len(rows))}
	for _, row := range rows {
		v.Items = append(v.Items, c.item(row))
	}
	c.logs(ctx, &v)
	return json.Marshal(v)
}

func connect(ctx context.Context, run Runner) (client, error) {
	endpoint := os.Getenv("DOCKER_HOST")
	selected := os.Getenv("DOCKER_CONTEXT")
	if selected != "" || endpoint == "" {
		if selected == "" {
			out, err := run(ctx, Command{Args: []string{"docker", "context", "show"}, Limit: 4096})
			if err != nil {
				return client{}, err
			}
			selected = strings.TrimSpace(string(out))
			if selected == "" || strings.ContainsAny(selected, "\r\n") {
				return client{}, errors.New("Docker returned an invalid context name")
			}
		}
		out, err := run(ctx, Command{Args: []string{"docker", "context", "inspect", "--format", "{{json .Endpoints.docker.Host}}", "--", selected}, Limit: 4096})
		if err != nil {
			return client{}, err
		}
		if err = json.Unmarshal(out, &endpoint); err != nil {
			return client{}, errors.New("Docker returned an invalid context endpoint")
		}
	}
	if !localEndpoint(endpoint) {
		return client{}, errors.New("this plugin accepts only a local Unix socket, local named pipe or literal loopback TCP endpoint")
	}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "DOCKER_CONTEXT") && !strings.EqualFold(key, "DOCKER_HOST") {
			env = append(env, entry)
		}
	}
	return client{endpoint, env, run}, nil
}

func localEndpoint(endpoint string) bool {
	if strings.HasPrefix(strings.ToLower(endpoint), "npipe:////./pipe/") {
		tail := endpoint[len("npipe:////./pipe/"):]
		return tail != "" && !strings.ContainsAny(tail, "?&#\\") && !strings.Contains(tail, "..")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "unix" {
		return u.Host == "" && strings.HasPrefix(u.Path, "/") && len(u.Path) > 1
	}
	if u.Scheme != "tcp" || u.Path != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	return ip != nil && ip.IsLoopback() && err == nil && port > 0 && port <= 65535
}

func (c client) command(ctx context.Context, limit int, combined bool, args ...string) ([]byte, error) {
	argv := append([]string{"docker", "--host", c.endpoint}, args...)
	out, err := c.run(ctx, Command{Args: argv, Env: c.env, Limit: limit + 1, Combined: combined})
	if err == nil && len(out) > limit && !combined {
		return nil, errors.New("Docker metadata exceeds its size limit")
	}
	return out, err
}

func validID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, ch := range id {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}
func (c client) itemID(id string) string {
	sum := sha256.Sum256([]byte(c.endpoint))
	return hex.EncodeToString(sum[:8]) + ":" + id
}

func (c client) read(ctx context.Context) ([]container, error) {
	out, err := c.command(ctx, metadataLimit, false, "container", "ls", "--all", "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	seen := map[string]bool{}
	for _, id := range ids {
		if !validID(id) || seen[id] {
			return nil, errors.New("Docker returned an invalid container list")
		}
		seen[id] = true
	}
	rows := make([]container, 0, len(ids))
	for first := 0; first < len(ids); first += 50 {
		batch := ids[first:min(first+50, len(ids))]
		batchRows, err := c.inspect(ctx, batch)
		if err != nil {
			return nil, err
		}
		rows = append(rows, batchRows...)

	}
	return rows, nil
}

func (c client) inspect(ctx context.Context, ids []string) ([]container, error) {
	args := append([]string{"container", "inspect", "--format", inspectFormat, "--"}, ids...)
	out, err := c.command(ctx, metadataLimit, false, args...)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	found := map[string]bool{}
	rows := []container{}
	for {
		var row container
		err := dec.Decode(&row)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("Docker returned malformed container information")
		}
		if !slices.Contains(ids, row.ID) || found[row.ID] || strings.Trim(strings.TrimSpace(row.Name), "/") == "" || row.Status == "" {
			return nil, errors.New("Docker returned incomplete or duplicate container information")
		}
		found[row.ID] = true
		rows = append(rows, row)
	}
	if len(found) != len(ids) {
		return nil, errors.New("Docker did not return every listed container")
	}
	return rows, nil
}

func classify(c container) (string, string, text) {
	switch c.Status {
	case "paused":
		return "paused", "warn", words("Paused", "已暂停")
	case "running":
		if c.Health == "unhealthy" {
			return "attention", "error", words("Running · unhealthy", "运行中 · 健康检查失败")
		}
		if c.Health == "starting" {
			return "running", "ok", words("Running · health check starting", "运行中 · 正在开始健康检查")
		}
		if c.Health != "" && c.Health != "healthy" {
			return "attention", "warn", words("Running · unknown health: "+c.Health, "运行中 · 未知健康状态："+c.Health)
		}
		return "running", "ok", words("Running", "运行中")
	case "created":
		return "stopped", "ok", words("Created · not started", "已创建 · 尚未启动")
	case "exited":
		if c.OOMKilled {
			return "attention", "error", words("Stopped · out of memory", "已停止 · 内存不足被终止")
		}
		if c.ExitCode != 0 {
			return "attention", "error", words(fmt.Sprintf("Stopped · exit %d", c.ExitCode), fmt.Sprintf("已停止 · 退出码 %d", c.ExitCode))
		}
		if c.Error != "" {
			return "attention", "error", words("Stopped · runtime error", "已停止 · 运行错误")
		}
		return "stopped", "ok", words("Stopped", "已停止")
	case "restarting":
		return "attention", "warn", words("Restarting", "正在重启")
	case "removing":
		return "attention", "warn", words("Removing", "正在移除")
	case "dead":
		return "attention", "error", words("Dead", "已损坏")
	default:
		return "attention", "warn", words("Unknown state: "+c.Status, "未知状态："+c.Status)
	}
}

func card(rows []container) any {
	running, stopped, paused, attention := 0, 0, 0, 0
	status := "ok"
	for _, r := range rows {
		g, s, _ := classify(r)
		if r.Status == "running" {
			running++
		}
		switch g {
		case "stopped":
			stopped++
		case "paused":
			paused++
		case "attention":
			attention++
		}
		if s == "error" {
			status = "error"
		} else if s == "warn" && status == "ok" {
			status = "warn"
		}
	}
	return map[string]any{"status": status, "value": strconv.Itoa(running), "label": words(fmt.Sprintf("running · %d containers", len(rows)), fmt.Sprintf("运行中 · 共 %d 个容器", len(rows))), "rows": []any{map[string]any{"name": words("Needs attention", "需要处理"), "value": attention}, map[string]any{"name": words("Cleanly stopped / not started", "正常未运行"), "value": stopped}, map[string]any{"name": words("Paused", "已暂停"), "value": paused}}}
}

func (c client) item(r container) item {
	g, status, value := classify(r)
	it := item{ID: c.itemID(r.ID), Name: strings.TrimPrefix(r.Name, "/"), Group: g, Status: status, Value: value}
	add := func(en, zh string, v any) {
		if s, ok := v.(string); ok && s == "" {
			return
		}
		it.Fields = append(it.Fields, field{words(en, zh), v})
	}
	add("Container ID", "容器 ID", r.ID)
	add("Image", "镜像", r.Image)
	add("State", "状态", value)
	if r.Health != "" {
		health := words(r.Health, r.Health)
		switch r.Health {
		case "healthy":
			health = words("Healthy", "健康")
		case "unhealthy":
			health = words("Unhealthy", "健康检查失败")
		case "starting":
			health = words("Starting", "检查中")
		}
		add("Health", "健康检查", health)
	}
	for _, entry := range []struct{ en, zh, value string }{{"Created", "创建时间", r.Created}, {"Started", "启动时间", r.Started}, {"Finished", "结束时间", r.Finished}} {
		if !strings.HasPrefix(entry.value, "0001-") {
			add(entry.en, entry.zh, entry.value)
		}
	}
	if r.Status == "exited" || r.Status == "dead" {
		add("Last exit", "上次退出", r.ExitCode)
	}
	add("Restart policy", "重启策略", r.RestartPolicy.Name)
	add("Restart count", "重启次数", r.RestartCount)
	if r.RestartPolicy.Name == "on-failure" && r.RestartPolicy.MaximumRetryCount > 0 {
		add("Maximum retries", "最大重试次数", r.RestartPolicy.MaximumRetryCount)
	}
	var ports []string
	for port, bindings := range r.Ports {
		if len(bindings) == 0 {
			ports = append(ports, port)
			continue
		}
		for _, bind := range bindings {
			ports = append(ports, net.JoinHostPort(bind.HostIp, bind.HostPort)+" → "+port)
		}
	}
	sort.Strings(ports)
	add("Ports", "端口", strings.Join(ports, "; "))
	var networks []string
	for name := range r.Networks {
		networks = append(networks, name)
	}
	sort.Strings(networks)
	add("Networks", "网络", strings.Join(networks, ", "))
	var mounts []string
	for _, m := range r.Mounts {
		mode := "ro"
		if m.RW {
			mode = "rw"
		}
		mounts = append(mounts, m.Type+": "+m.Source+" → "+m.Destination+" ("+mode+")")
	}
	sort.Strings(mounts)
	add("Mounts", "挂载", strings.Join(mounts, "; "))
	if r.Path != "" {
		argv, _ := json.Marshal(append([]string{r.Path}, r.Args...))
		add("Command arguments", "命令参数", string(argv))
	}
	add("Runtime error", "运行错误", r.Error)
	if id := offeredAction(r); id != "" {
		label := words("Start", "启动")
		if id == "restart" {
			label = words("Restart", "重启")
		}
		it.Actions = []action{{id, label}}
	}
	return it
}

func (c client) logs(ctx context.Context, v *view) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	remaining := pageLogLimit
	for i := range v.Items {
		it := &v.Items[i]
		_, id, _ := strings.Cut(it.ID, ":")
		note := func(en, zh string) {
			it.Fields = append(it.Fields, field{words("Log reading", "日志读取"), words(en, zh)})
		}
		if remaining == 0 {
			note("Log preview truncated at the 512 KiB page limit.", "日志预览已达到单页 512 KiB 上限。")
			continue
		}
		if ctx.Err() != nil {
			note("The log preview time budget was used up. Refresh to try again.", "本次日志预览读取时间已用尽，可刷新重试。")
			continue
		}
		readCtx, stop := context.WithTimeout(ctx, time.Second)
		out, err := c.command(readCtx, logLimit, true, "container", "logs", "--tail", "20", "--timestamps", "--", id)
		stop()
		if err != nil {
			note("Could not read this container's recent logs.", "读不到这个容器的最近日志。")
			continue
		}
		limit := min(logLimit, remaining)
		content := strings.ToValidUTF8(string(out), "�")
		if len(content) > limit {
			content = content[:limit]
			for len(content) > 0 && !utf8.ValidString(content) {
				content = content[:len(content)-1]
			}
			note("Log preview truncated at its size limit.", "日志预览已达到大小上限，后续内容已截断。")
		}
		it.Log = content
		remaining -= len(it.Log)
	}
}
