// Package tasks implements the native scheduled-script plugin.
package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

const maxConfig = 1 << 20

type Config struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

type Task struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Enabled  *bool     `json:"enabled"`
	Schedule Schedule  `json:"schedule"`
	Exec     Execution `json:"exec"`
	Timeout  string    `json:"timeout,omitempty"`
}

type Schedule struct {
	Cron     *string `json:"cron,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
	Every    *string `json:"every,omitempty"`
}

type Execution struct {
	Command []string          `json:"command"`
	Windows []string          `json:"windows,omitempty"`
	Dir     string            `json:"dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

var taskID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var envRef = regexp.MustCompile(`^\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}$`)

func Load(dir string) (Config, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return Config{}, err
	}
	defer r.Close()
	f, err := r.Open("tasks.json")
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxConfig+1))
	if err != nil {
		return Config{}, err
	}
	if len(b) > maxConfig {
		return Config{}, errors.New("tasks.json exceeds 1 MiB")
	}
	return parseConfig(b)
}

func parseConfig(b []byte) (Config, error) {
	var c Config
	if !utf8.Valid(b) {
		return c, errors.New("tasks.json is not UTF-8")
	}
	if err := strictJSON(b, &c, false); err != nil {
		return c, err
	}
	var presence struct {
		Tasks []struct {
			Timeout json.RawMessage
			Exec    struct{ Dir json.RawMessage }
		}
	}
	if err := json.Unmarshal(b, &presence); err != nil {
		return c, err
	}
	for _, t := range presence.Tasks {
		if string(t.Timeout) == `""` || string(t.Exec.Dir) == `""` {
			return c, errors.New("an explicit empty timeout or directory is not a default")
		}
	}
	if c.Version != 1 || c.Tasks == nil || len(c.Tasks) > 64 {
		return c, errors.New("version must be 1 and tasks must be an array of at most 64 tasks")
	}
	seen := map[string]bool{}
	for i := range c.Tasks {
		t := &c.Tasks[i]
		if !taskID.MatchString(t.ID) || seen[t.ID] {
			return c, fmt.Errorf("invalid or duplicate task id at index %d", i)
		}
		seen[t.ID] = true
		t.Name = strings.TrimSpace(t.Name)
		if t.Name == "" || utf8.RuneCountInString(t.Name) > 200 || t.Enabled == nil {
			return c, fmt.Errorf("task %s requires a name and enabled boolean", t.ID)
		}
		if err := t.Schedule.validate(); err != nil {
			return c, fmt.Errorf("task %s schedule: %w", t.ID, err)
		}
		if err := checkCommand(t.Exec.Command); err != nil {
			return c, fmt.Errorf("task %s command: %w", t.ID, err)
		}
		if t.Exec.Windows != nil {
			if err := checkCommand(t.Exec.Windows); err != nil {
				return c, fmt.Errorf("task %s windows: %w", t.ID, err)
			}
		}
		if t.Exec.Dir == "" {
			t.Exec.Dir = "."
		}
		if strings.ContainsRune(t.Exec.Dir, 0) {
			return c, errors.New("working directory contains NUL")
		}
		keys := map[string]bool{}
		for k, v := range t.Exec.Env {
			fold := strings.ToUpper(k)
			if !envName.MatchString(k) || keys[fold] || !envRef.MatchString(v) {
				return c, fmt.Errorf("task %s has invalid environment references", t.ID)
			}
			keys[fold] = true
		}
		if t.Timeout == "" {
			t.Timeout = "30m"
		}
		d, err := time.ParseDuration(t.Timeout)
		if err != nil || d < time.Second || d > 24*time.Hour {
			return c, fmt.Errorf("task %s timeout must be between 1s and 24h", t.ID)
		}
		t.Timeout = d.String()
	}
	return c, nil
}

func checkCommand(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" || args[0] == "@hostdeck" {
		return errors.New("an external executable is required")
	}
	for _, a := range args {
		if strings.ContainsRune(a, 0) {
			return errors.New("argument contains NUL")
		}
	}
	return nil
}

// Check tokens before decoding: encoding/json otherwise accepts duplicate keys
// and silently converts null into zero-valued strings, booleans and structs.
func strictJSON(b []byte, v any, state bool) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("JSON nesting exceeds 32")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		if t == nil && !state {
			return errors.New("null is not a configuration value")
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				keys := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					key, ok := k.(string)
					if !ok || keys[key] {
						return errors.New("duplicate JSON object key")
					}
					keys[key] = true
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			default:
				return errors.New("unexpected closing delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	if !state {
		d.DisallowUnknownFields()
	}
	return d.Decode(v)
}
