// Package cli provides the local, machine-readable connection commands.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

var commands = map[string][]string{
	"page add":    {"address", "name", "mode"},
	"page update": {"id", "name", "icon", "proxy", "link", "card-url"},
	"plugin list": {}, "plugin catalog": {},
	"plugin install": {"id", "from"}, "plugin validate": {"from"},
	"plugin enable": {"id"}, "plugin disable": {"id"}, "plugin reload": {},
}

// Failure is stable enough for an agent to branch on Code; Message is for people.
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type result struct {
	OK    bool     `json:"ok"`
	Data  any      `json:"data,omitempty"`
	Error *Failure `json:"error,omitempty"`
}

type client struct {
	origin string
	http   *http.Client
}

// Run writes one JSON result. Exit 0 is success, 1 an operation failure and
// 2 invalid command arguments. It never starts the console server.
func Run(args []string, out io.Writer) int {
	finish := func(data any, problem *Failure, code int) int {
		if json.NewEncoder(out).Encode(result{OK: problem == nil, Data: data, Error: problem}) != nil {
			return 1
		}
		return code
	}
	usage := func(message string) int { return finish(nil, &Failure{"usage", message}, 2) }
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		return finish(help(), nil, 0)
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") && (args[0] == "page" || args[0] == "plugin") {
		return finish(help(), nil, 0)
	}
	if len(args) < 2 {
		return usage("expected page or plugin and a subcommand; use hostdeck help")
	}
	action := args[0] + " " + args[1]
	fields, known := commands[action]
	if !known {
		return usage("unknown command; use hostdeck help")
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", "http://127.0.0.1:4678", "running console origin")
	values := map[string]*string{}
	for _, name := range fields {
		value := ""
		if name == "mode" {
			value = "proxy"
		}
		values[name] = flags.String(name, value, name)
	}
	if err := flags.Parse(args[2:]); errors.Is(err, flag.ErrHelp) {
		return finish(help(), nil, 0)
	} else if err != nil {
		return usage(err.Error())
	}
	if flags.NArg() != 0 {
		return usage("unexpected positional arguments; use named flags")
	}
	value := func(name string) string {
		if v := values[name]; v != nil {
			return *v
		}
		return ""
	}
	visited := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	for _, required := range []string{"id", "address"} {
		if _, exists := values[required]; exists && value(required) == "" {
			return usage("--" + required + " is required")
		}
	}
	if v := value("id"); v != "" && !validID(v) {
		return usage("--id must be one non-hidden folder name")
	}
	if action == "page add" && value("mode") != "proxy" && value("mode") != "link" {
		return usage("--mode must be proxy or link")
	}
	if action == "page update" {
		changes := len(visited)
		if visited["url"] {
			changes--
		}
		if visited["id"] {
			changes--
		}
		if changes == 0 {
			return usage("provide at least one field to update")
		}
		if visited["proxy"] && visited["link"] {
			return usage("--proxy and --link cannot be combined")
		}
	}
	if action == "plugin validate" && value("from") == "" {
		return usage("--from is required")
	}
	if action == "plugin validate" {
		p, err := plugin.ReadFolder(value("from"))
		if err != nil {
			return finish(nil, &Failure{"invalid_manifest", err.Error()}, 1)
		}
		return finish(map[string]any{"valid": true, "schema": plugin.Schema, "path": p.Dir, "name": p.Name}, nil, 0)
	}
	c, problem := connect(*base)
	if problem != nil {
		return usage(problem.Message)
	}
	defer c.http.CloseIdleConnections()
	var data any
	switch action {
	case "page add":
		var raw json.RawMessage
		raw, problem = c.call(http.MethodPost, "/api/pages", map[string]string{"address": value("address"), "name": value("name"), "mode": value("mode")})
		if problem == nil {
			var added struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &added) != nil || !validID(added.ID) {
				problem = &Failure{"invalid_response", "page response has no plugin ID"}
			} else {
				data, problem = c.installed(added.ID, false)
			}
		}
	case "page update":
		body := map[string]string{}
		for flag, key := range map[string]string{"name": "name", "icon": "icon", "proxy": "proxy", "link": "link", "card-url": "cardUrl"} {
			if visited[flag] {
				body[key] = value(flag)
			}
		}
		_, problem = c.call(http.MethodPost, "/api/plugins/"+url.PathEscape(value("id"))+"/page", body)
		if problem == nil {
			data, problem = c.installed(value("id"), false)
		}
	case "plugin list", "plugin catalog":
		var raw json.RawMessage
		raw, problem = c.call(http.MethodGet, "/api/catalog", nil)
		data = raw
		if problem == nil && !isCatalog(raw) {
			problem = &Failure{"invalid_response", "console did not return a plugin catalog"}
		}
	case "plugin install":
		if value("from") != "" {
			var raw json.RawMessage
			raw, problem = c.call(http.MethodGet, "/api/catalog", nil)
			if problem != nil {
				break
			}
			var listing struct {
				PluginDir string `json:"pluginDir"`
			}
			if json.Unmarshal(raw, &listing) != nil || !filepath.IsAbs(listing.PluginDir) {
				problem = &Failure{"invalid_response", "catalog has no absolute plugin directory"}
				break
			}
			var path string
			path, problem = installLocal(listing.PluginDir, value("id"), value("from"))
			if problem != nil {
				break
			}
			data = map[string]any{"id": value("id"), "path": path, "saved": true}
			_, problem = c.call(http.MethodPost, "/api/plugins/reload", nil)
			if problem == nil {
				data, problem = c.installed(value("id"), false)
			}
			break
		}
		_, problem = c.call(http.MethodPost, "/api/catalog/"+url.PathEscape(value("id")), nil)
		if problem == nil {
			data, problem = c.installed(value("id"), false)
		}
	case "plugin enable", "plugin disable":
		disabled := action == "plugin disable"
		_, problem = c.call(http.MethodPost, "/api/plugins/"+url.PathEscape(value("id"))+"/"+args[1], nil)
		if problem == nil {
			data, problem = c.installed(value("id"), disabled)
		}
	case "plugin reload":
		var raw json.RawMessage
		raw, problem = c.call(http.MethodPost, "/api/plugins/reload", nil)
		data = raw
		if problem == nil {
			if !isCatalog(raw) {
				problem = &Failure{"invalid_response", "reload did not return a plugin catalog"}
				break
			}
			var listing struct {
				Installed []struct {
					Loaded   bool            `json:"loaded"`
					Disabled bool            `json:"disabled"`
					Broken   bool            `json:"broken"`
					Error    json.RawMessage `json:"error"`
				} `json:"installed"`
			}
			if json.Unmarshal(raw, &listing) != nil {
				problem = &Failure{"invalid_response", "reload did not return a catalog"}
				break
			}
			for _, p := range listing.Installed {
				if !p.Disabled && (!p.Loaded || p.Broken || hasError(p.Error)) {
					problem = &Failure{"plugins_unavailable", "reload finished with unavailable plugins; inspect data.installed"}
					break
				}
			}
		}
	}
	if problem != nil {
		if problem.Code == "reload_failed" && action != "plugin reload" && data == nil {
			saved := map[string]any{"saved": true}
			if id := value("id"); id != "" {
				saved["id"] = id
			}
			data = saved
		}
		return finish(data, problem, 1)
	}
	return finish(data, nil, 0)
}

func validID(id string) bool {
	return id != "" && !strings.HasPrefix(id, ".") && !strings.ContainsAny(id, "/\\:\x00\r\n") && strings.TrimSpace(id) == id
}

func help() any {
	return map[string]any{"usage": "hostdeck <page|plugin> <command> [flags]", "commands": commands,
		"url": "--url http://127.0.0.1:4678 selects the running console; commands do not start it",
		"flags": map[string]string{
			"--id":      "required for install, update, enable and disable; one non-hidden plugin folder name",
			"--address": "required by page add; HTTP/HTTPS address, with optional scheme",
			"--mode":    "page add: proxy (default) embeds a local/LAN page; link opens any HTTP/HTTPS page externally",
			"--from":    "local source folder containing plugin.yaml; required by validate; install without it selects a built-in ID",
			"--name / --icon / --proxy / --link / --card-url": "page update changes only supplied fields; proxy and link are mutually exclusive",
		},
		"notes":     []string{"Arguments are named flags after the command; JSON is always written to stdout.", "list and catalog return the installed plugins, available built-ins and actual data directory.", "validate checks schema: 1 with the CLI process environment and never runs plugin commands.", "Local install refuses existing enabled/disabled IDs, symlinks and sources containing the destination; it never downloads plugins.", "Mutations reload immediately; plugin reload activates manual file edits without restarting. An error may retain data.saved=true.", "loaded describes hostdeck loading, not external service health; inspect ok/error and actual card/page readings."},
		"exitCodes": map[string]string{"0": "success", "1": "operation failed; inspect error.code and any saved data", "2": "invalid arguments"}}
}

func isCatalog(raw json.RawMessage) bool {
	var c struct {
		PluginDir string            `json:"pluginDir"`
		Installed []json.RawMessage `json:"installed"`
	}
	return json.Unmarshal(raw, &c) == nil && filepath.IsAbs(c.PluginDir) && c.Installed != nil
}

func connect(base string) (*client, *Failure) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, &Failure{"usage", "--url must be an http://127.0.0.1 console origin with no credentials, path, query or fragment"}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, &Failure{"usage", "--url has an invalid port"}
		}
	}
	return &client{strings.TrimSuffix(base, "/"), &http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("console redirects are refused") }}}, nil
}

func (c *client) call(method, path string, body any) (json.RawMessage, *Failure) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, &Failure{"request_failed", err.Error()}
		}
	}
	req, err := http.NewRequest(method, c.origin+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, &Failure{"request_failed", err.Error()}
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", c.origin)
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Failure{"connection_failed", err.Error()}
	}
	defer resp.Body.Close()
	const limit = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(raw) > limit || !json.Valid(raw) {
		return nil, &Failure{"invalid_response", fmt.Sprintf("HTTP %d: console did not return bounded JSON", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failed struct {
			Error struct {
				Code   string `json:"code"`
				Detail string `json:"detail"`
			} `json:"error"`
		}
		json.Unmarshal(raw, &failed)
		if failed.Error.Code == "" {
			failed.Error.Code = "request_failed"
		}
		message := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, failed.Error.Code)
		if failed.Error.Detail != "" {
			message += ": " + failed.Error.Detail
		}
		return nil, &Failure{failed.Error.Code, message}
	}
	return raw, nil
}

// installed checks the state after a write instead of treating a saved file as
// proof that the plugin loaded. A failure retains saved=true for recovery.
func (c *client) installed(id string, disabled bool) (any, *Failure) {
	data := map[string]any{"id": id, "saved": true, "available": false}
	raw, problem := c.call(http.MethodGet, "/api/catalog", nil)
	if problem != nil {
		return data, problem
	}
	var listing struct {
		PluginDir string            `json:"pluginDir"`
		Installed []json.RawMessage `json:"installed"`
	}
	if json.Unmarshal(raw, &listing) != nil || !filepath.IsAbs(listing.PluginDir) {
		return data, &Failure{"invalid_response", "catalog has no absolute plugin directory"}
	}
	data["path"] = filepath.Join(listing.PluginDir, id)
	for _, raw := range listing.Installed {
		var entry struct {
			ID       string          `json:"id"`
			Loaded   bool            `json:"loaded"`
			Disabled bool            `json:"disabled"`
			Broken   bool            `json:"broken"`
			Error    json.RawMessage `json:"error"`
		}
		if json.Unmarshal(raw, &entry) != nil || entry.ID != id {
			continue
		}
		data["plugin"] = raw
		data["loaded"] = entry.Loaded
		data["disabled"] = entry.Disabled
		if entry.Disabled {
			data["path"] = filepath.Join(listing.PluginDir, "."+id)
		}
		if entry.Disabled != disabled || (!disabled && (!entry.Loaded || entry.Broken || hasError(entry.Error))) {
			if hasError(entry.Error) {
				var reported struct {
					Code   string `json:"code"`
					Detail string `json:"detail"`
				}
				if json.Unmarshal(entry.Error, &reported) == nil && reported.Code != "" {
					return data, &Failure{reported.Code, "data was saved: " + reported.Detail + "; inspect data.plugin"}
				}
			}
			return data, &Failure{"not_loaded", "data was saved but the plugin is not available; inspect data.plugin"}
		}
		data["available"] = !disabled
		return data, nil
	}
	return data, &Failure{"not_loaded", "data was saved but the plugin is absent from the catalog"}
}

func hasError(raw json.RawMessage) bool { return len(raw) != 0 && string(raw) != "null" }
