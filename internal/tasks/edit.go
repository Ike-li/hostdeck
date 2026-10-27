package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ike-li/hostdeck/internal/process"
)

// ErrTaskForm is a task the page is not allowed to store.
// ErrTaskMissing means that id is not in the current configuration.
var (
	ErrTaskForm    = errors.New("task form is not valid")
	ErrTaskMissing = errors.New("task does not exist")
)

// TaskForm is the page's task editor. Command is one line, split on
// whitespace and double quotes. A nil command on update keeps the argv.
type TaskForm struct {
	ID            string       `json:"id,omitempty"`
	Name          string       `json:"name"`
	Enabled       *bool        `json:"enabled,omitempty"`
	Schedule      ScheduleForm `json:"schedule"`
	Timeout       string       `json:"timeout,omitempty"`
	Command       *string      `json:"command,omitempty"`
	CommandLocked bool         `json:"command_locked,omitempty"`
}

// ScheduleForm is exactly one of every, or cron plus an explicit timezone.
type ScheduleForm struct {
	Cron     *string `json:"cron,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
	Every    *string `json:"every,omitempty"`
}

type formError struct{ text string }

func (e *formError) Error() string { return e.text }
func (e *formError) Unwrap() error { return ErrTaskForm }

// CreateTask appends one task and refreshes this process's schedule.
// The id is chosen from the name and is not reused while it still appears
// in the configuration or in saved runs.
func CreateTask(dir string, form TaskForm) (string, error) {
	name, err := nameFrom(form.Name)
	if err != nil {
		return "", err
	}
	if form.Command == nil {
		return "", &formError{"a command is required"}
	}
	argv, err := commandFrom(*form.Command)
	if err != nil {
		return "", err
	}
	sched, ok, err := scheduleFrom(form.Schedule)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &formError{"choose exactly one of cron and every"}
	}
	enabled := true
	if form.Enabled != nil {
		enabled = *form.Enabled
	}
	e, err := active(dir)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.writableLocked(); err != nil {
		return "", err
	}
	cfg, err := e.readAuthoredLocked()
	if err != nil {
		return "", err
	}
	id, err := idFromName(name, func(id string) bool { return e.takenLocked(cfg.Tasks, id) })
	if err != nil {
		return "", err
	}
	on := enabled
	cfg.Tasks = append(cfg.Tasks, Task{
		ID:       id,
		Name:     name,
		Enabled:  &on,
		Schedule: sched,
		Exec:     applyCommand(Execution{}, argv, runtime.GOOS, true),
		Timeout:  timeoutFrom(form.Timeout),
	})
	if err := e.publishLocked(cfg); err != nil {
		return "", err
	}
	e.refreshLocked()
	if e.configErr != nil {
		return "", e.configErr
	}
	return id, nil
}

// UpdateTask changes one task's name, enabled flag, schedule, timeout and
// the current platform's argv. It keeps the id, env, directory and the other
// platform's argv. A nil command leaves the argv as stored. Fields the form
// leaves out stay as they were.
func UpdateTask(dir, id string, form TaskForm) error {
	name, err := nameFrom(form.Name)
	if err != nil {
		return err
	}
	var argv []string
	if form.Command != nil {
		argv, err = commandFrom(*form.Command)
		if err != nil {
			return err
		}
	}
	sched, hasSchedule, err := scheduleFrom(form.Schedule)
	if err != nil {
		return err
	}
	e, err := active(dir)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.writableLocked(); err != nil {
		return err
	}
	cfg, err := e.readAuthoredLocked()
	if err != nil {
		return err
	}
	idx := -1
	for i := range cfg.Tasks {
		if cfg.Tasks[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrTaskMissing
	}
	task := cfg.Tasks[idx]
	task.Name = name
	if form.Enabled != nil {
		on := *form.Enabled
		task.Enabled = &on
	}
	if hasSchedule {
		task.Schedule = sched
	}
	if form.Command != nil {
		task.Exec = applyCommand(task.Exec, argv, runtime.GOOS, false)
	}
	if strings.TrimSpace(form.Timeout) != "" || form.Command != nil || hasSchedule || form.Enabled != nil {
		task.Timeout = timeoutFrom(form.Timeout)
	}
	cfg.Tasks[idx] = task
	if err := e.publishLocked(cfg); err != nil {
		return err
	}
	e.refreshLocked()
	if e.configErr != nil {
		return e.configErr
	}
	return nil
}

// DeleteTask removes one task from tasks.json and refreshes the schedule.
// A run that is still going is cancelled as task_removed. Its history stays.
func DeleteTask(dir, id string) error {
	e, err := active(dir)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.writableLocked(); err != nil {
		return err
	}
	cfg, err := e.readAuthoredLocked()
	if err != nil {
		return err
	}
	kept := make([]Task, 0, len(cfg.Tasks))
	found := false
	for _, task := range cfg.Tasks {
		if task.ID == id {
			found = true
			continue
		}
		kept = append(kept, task)
	}
	if !found {
		return ErrTaskMissing
	}
	cfg.Tasks = kept
	if err := e.publishLocked(cfg); err != nil {
		return err
	}
	e.refreshLocked()
	if e.configErr != nil {
		return e.configErr
	}
	return nil
}

// ListTasks returns every task form without taking the owner lock.
func ListTasks(dir string) ([]TaskForm, error) {
	e, err := active(dir)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg, err := e.readAuthoredLocked()
	if err != nil {
		return nil, err
	}
	out := make([]TaskForm, 0, len(cfg.Tasks))
	for _, task := range cfg.Tasks {
		out = append(out, formFrom(task, runtime.GOOS))
	}
	return out, nil
}

// ReadTask returns the form for one task without taking the owner lock.
// A command that cannot be shown as one line is left locked.
func ReadTask(dir, id string) (TaskForm, error) {
	e, err := active(dir)
	if err != nil {
		return TaskForm{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg, err := e.readAuthoredLocked()
	if err != nil {
		return TaskForm{}, err
	}
	for _, task := range cfg.Tasks {
		if task.ID == id {
			return formFrom(task, runtime.GOOS), nil
		}
	}
	return TaskForm{}, ErrTaskMissing
}

func formFrom(task Task, goos string) TaskForm {
	form := TaskForm{
		ID:       task.ID,
		Name:     task.Name,
		Enabled:  task.Enabled,
		Timeout:  task.Timeout,
		Schedule: ScheduleForm{Cron: task.Schedule.Cron, Timezone: task.Schedule.Timezone, Every: task.Schedule.Every},
	}
	if line, ok := quoteCommand(shownCommand(task.Exec, goos)); ok {
		form.Command = &line
	} else {
		form.CommandLocked = true
	}
	return form
}

func (e *entry) writableLocked() error {
	if e.stateErr != nil {
		return e.stateErr
	}
	if e.ownerErr != nil {
		return e.ownerErr
	}
	if err := e.claimLocked(); err != nil {
		return err
	}
	for id, identity := range e.state.Active {
		if !e.ownsActive(id) && identity.PID != 0 && !process.Gone(identity) {
			return errors.New("old task process tree still needs local inspection")
		}
	}
	return nil
}

func (e *entry) readAuthoredLocked() (Config, error) {
	f, err := e.root.Open("tasks.json")
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
	var c Config
	if err := strictJSON(b, &c, false); err != nil {
		return Config{}, err
	}
	if c.Tasks == nil {
		c.Tasks = []Task{}
	}
	return c, nil
}

// publishLocked checks the candidate and, only then, replaces tasks.json.
// parseConfig rewrites a timeout such as 30m to 30m0s on its own copy;
// the file keeps the bytes validated here.
func (e *entry) publishLocked(c Config) error {
	if c.Tasks == nil {
		c.Tasks = []Task{}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := parseConfig(b); err != nil {
		return &formError{err.Error()}
	}
	if len(b) > maxConfig {
		return &formError{"tasks.json exceeds 1 MiB"}
	}
	return e.replaceLocked("tasks.json", "tasks.json.tmp", b)
}

func (e *entry) replaceLocked(name, tmp string, b []byte) error {
	_ = e.root.Remove(tmp)
	f, err := e.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer e.root.Remove(tmp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	until := time.Now().Add(2 * time.Second)
	for {
		err = e.root.Rename(tmp, name)
		if err == nil || !errors.Is(err, fs.ErrPermission) || time.Now().After(until) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *entry) takenLocked(tasks []Task, id string) bool {
	for _, t := range tasks {
		if t.ID == id {
			return true
		}
	}
	if _, ok := e.state.Tasks[id]; ok {
		return true
	}
	for _, r := range e.state.Runs {
		if r.TaskID == id {
			return true
		}
	}
	return false
}

func nameFrom(s string) (string, error) {
	name := strings.TrimSpace(s)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return "", &formError{"a task needs a name"}
	}
	return name, nil
}

func timeoutFrom(s string) string {
	if strings.TrimSpace(s) == "" {
		return "30m"
	}
	return strings.TrimSpace(s)
}

func scheduleFrom(form ScheduleForm) (Schedule, bool, error) {
	if form.Cron == nil && form.Every == nil && form.Timezone == nil {
		return Schedule{}, false, nil
	}
	cron, every, zone := "", "", ""
	if form.Cron != nil {
		cron = strings.TrimSpace(*form.Cron)
	}
	if form.Every != nil {
		every = strings.TrimSpace(*form.Every)
	}
	if form.Timezone != nil {
		zone = strings.TrimSpace(*form.Timezone)
	}
	switch {
	case every != "" && cron == "" && zone == "":
		return Schedule{Every: &every}, true, nil
	case every == "" && cron != "":
		if zone == "" {
			return Schedule{}, false, &formError{"cron requires an explicit IANA timezone"}
		}
		return Schedule{Cron: &cron, Timezone: &zone}, true, nil
	default:
		return Schedule{}, false, &formError{"choose exactly one of cron and every"}
	}
}

func commandFrom(line string) ([]string, error) {
	args, err := splitCommand(line)
	if err != nil {
		return nil, err
	}
	if err := checkCommand(args); err != nil {
		return nil, &formError{err.Error()}
	}
	return args, nil
}

func splitCommand(line string) ([]string, error) {
	var args []string
	var b strings.Builder
	quote, started := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quote = !quote
			started = true
		case !quote && unicode.IsSpace(r):
			if started {
				args = append(args, b.String())
				b.Reset()
				started = false
			}
		default:
			b.WriteRune(r)
			started = true
		}
	}
	if quote {
		return nil, &formError{"command has an unclosed quote"}
	}
	if started {
		args = append(args, b.String())
	}
	return args, nil
}

func quoteCommand(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, "\"\n\r") {
			return "", false
		}
		if arg == "" || strings.IndexFunc(arg, unicode.IsSpace) >= 0 {
			parts[i] = `"` + arg + `"`
		} else {
			parts[i] = arg
		}
	}
	return strings.Join(parts, " "), true
}

// applyCommand writes argv on the platform the page is editing.
// On Windows it changes exec.windows only when that array is already there.
func applyCommand(exec Execution, argv []string, goos string, creating bool) Execution {
	next := append([]string(nil), argv...)
	if goos == "windows" && !creating && exec.Windows != nil {
		exec.Windows = next
		return exec
	}
	exec.Command = next
	return exec
}

func shownCommand(exec Execution, goos string) []string {
	if goos == "windows" && exec.Windows != nil {
		return exec.Windows
	}
	return exec.Command
}

func idFromName(name string, taken func(string) bool) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "task"
	}
	base = fitID(base, "")
	id := base
	for n := 2; taken(id); n++ {
		if n > 4096 {
			return "", &formError{"could not choose a task id"}
		}
		id = fitID(base, fmt.Sprintf("-%d", n))
	}
	if !taskID.MatchString(id) {
		return "", &formError{"could not choose a task id"}
	}
	return id, nil
}

func fitID(base, suffix string) string {
	room := 64 - len(suffix)
	if room < 1 {
		room = 1
	}
	stem := base
	if len(stem) > room {
		stem = strings.TrimRight(stem[:room], "-")
	}
	if stem == "" {
		stem = "t"
	}
	return stem + suffix
}
