package tasks

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/process"
)

// RunScript starts one command for a script file and returns its run id.
// A nil command uses the platform default. Any other command must be that
// default, or the current platform's argv of a saved task that names this
// file as one exact argument. The program name cannot be @hostdeck.
// The run is recorded in state.json and does not change tasks.json.
func RunScript(dir, name string, command []string) (string, error) {
	if err := checkScriptName(name); err != nil {
		return "", err
	}
	e, err := active(dir)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping || e.retired || !e.valid {
		return "", errors.New("task plugin is not active")
	}
	if err := e.writableLocked(); err != nil {
		return "", err
	}
	info, err := e.root.Lstat("scripts/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrScriptMissing
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", ErrScriptRefused
	}
	if command == nil {
		var ok bool
		command, ok = defaultCommand(name, runtime.GOOS)
		if !ok {
			return "", &formError{"this script has no default command"}
		}
	}
	if err := e.allowScriptCommand(name, command); err != nil {
		return "", err
	}
	for _, current := range e.state.Scripts {
		if current.File == name && activeStatus(current.Status) {
			return "", errors.New("this script is already running")
		}
	}
	if e.scriptRunning[name] != nil || len(e.state.Active) >= 4 {
		return "", errors.New("this script is already running or all four execution slots are busy")
	}
	if err := e.pruneLocked(1); err != nil {
		return "", err
	}
	id := randomID("run-")
	if err := e.root.MkdirAll("data/logs", 0700); err != nil {
		return "", err
	}
	outPath, errPath := "data/logs/"+id+".stdout.log", "data/logs/"+id+".stderr.log"
	out, err := e.root.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	errout, err := e.root.OpenFile(errPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		out.Close()
		e.root.Remove(outPath)
		return "", err
	}
	now := e.now()
	run := ScriptRun{ID: id, File: name, Command: append([]string(nil), command...), AcceptedAt: now, Status: "starting", Logs: logs{Stdout: &outPath, Stderr: &errPath}}
	e.state.Scripts = append(e.state.Scripts, run)
	e.state.Active[id] = process.Identity{Token: id}
	if err = e.saveLocked(); err != nil {
		e.state.Scripts = e.state.Scripts[:len(e.state.Scripts)-1]
		delete(e.state.Active, id)
		out.Close()
		errout.Close()
		e.root.Remove(outPath)
		e.root.Remove(errPath)
		e.stateErr = err
		return "", err
	}
	dir, env, secrets := e.dir, os.Environ(), []string(nil)
	var envErr error
	timeout := 30 * time.Minute
	if task, ok := e.taskForCommand(command); ok {
		if d, err := time.ParseDuration(task.Timeout); err == nil && d > 0 {
			timeout = d
		}
		dir = localDir(e.dir, task.Exec.Dir)
		env, secrets, envErr = environment(task)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	r := &running{id: id, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	e.scriptRunning[name] = r
	go e.executeScript(name, command, dir, env, secrets, envErr, r, out, errout)
	return id, nil
}

func (e *entry) allowScriptCommand(name string, command []string) error {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" || command[0] == "@hostdeck" {
		return &formError{"an external executable is required"}
	}
	for _, arg := range command {
		if strings.ContainsRune(arg, 0) {
			return &formError{"argument contains NUL"}
		}
	}
	if def, ok := defaultCommand(name, runtime.GOOS); ok && slices.Equal(command, def) {
		return nil
	}
	target := "scripts/" + name
	for _, t := range e.cfg.Tasks {
		argv := shownCommand(t.Exec, runtime.GOOS)
		if len(argv) == 0 || argv[0] == "@hostdeck" || !slices.Equal(argv, command) {
			continue
		}
		if slices.Contains(argv, target) {
			return nil
		}
	}
	return &formError{"command is not the default or a saved command for this script"}
}

func (e *entry) taskForCommand(command []string) (Task, bool) {
	for _, t := range e.cfg.Tasks {
		if slices.Equal(shownCommand(t.Exec, runtime.GOOS), command) {
			return t, true
		}
	}
	return Task{}, false
}

func (e *entry) scriptRecord(id string) *ScriptRun {
	for i := range e.state.Scripts {
		if e.state.Scripts[i].ID == id {
			return &e.state.Scripts[i]
		}
	}
	return nil
}

func (e *entry) executeScript(name string, command []string, dir string, env, secrets []string, envErr error, r *running, out, errout *os.File) {
	defer r.cancel()
	stdout, stderr := newLog(out, secrets), newLog(errout, secrets)
	result := process.Outcome{Error: envErr, Clean: true}
	if envErr == nil {
		if executable, exeErr := os.Executable(); exeErr == nil {
			result = process.Run(r.ctx, executable, process.Spec{Command: command, Dir: dir, Env: env, Token: r.id}, process.Hooks{
				Stdout: stdout,
				Stderr: stderr,
				Prepared: func(identity process.Identity) error {
					e.mu.Lock()
					defer e.mu.Unlock()
					e.state.Active[r.id] = identity
					return e.saveLocked()
				},
				Started: func(int) error {
					e.mu.Lock()
					defer e.mu.Unlock()
					record := e.scriptRecord(r.id)
					if record == nil {
						return errors.New("script run disappeared")
					}
					now := e.now()
					record.StartedAt = &now
					record.Status = "running"
					return e.saveLocked()
				},
			})
		} else {
			result.Error = exeErr
		}
	}
	e.finish(r, stdout, stderr, secrets, result, finishTarget{running: e.scriptRunning, key: name})
}

func (e *entry) scriptLog(r *ScriptRun) string {
	var b strings.Builder
	if r.ExitCode != nil {
		b.WriteString("exit=")
		b.WriteString(strconv.Itoa(*r.ExitCode))
		b.WriteByte('\n')
	}
	if r.Diagnostic != "" {
		b.WriteString(r.Diagnostic)
		b.WriteByte('\n')
	}
	for i, p := range []*string{r.Logs.Stdout, r.Logs.Stderr} {
		if p == nil {
			continue
		}
		preview := e.preview(*p)
		if preview == "" {
			continue
		}
		b.WriteString([]string{"stdout", "stderr"}[i])
		b.WriteByte('\n')
		b.WriteString(preview)
		b.WriteByte('\n')
	}
	s := b.String()
	if len(s) > 16<<10 {
		s = strings.ToValidUTF8(s[:16<<10], "")
	}
	return s
}
