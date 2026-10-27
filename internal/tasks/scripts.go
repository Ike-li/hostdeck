package tasks

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

const maxScript = 1 << 20

var scriptName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}(?:\.[A-Za-z0-9]{1,8})?$`)

// ErrScriptName, ErrScriptContent, ErrScriptExists, ErrScriptMissing and
// ErrScriptRefused describe a script the page is not allowed to store.
// ErrTasksUnavailable means this process is not the writable owner.
var (
	ErrScriptName       = errors.New("script name is not a single file name")
	ErrScriptContent    = errors.New("script must be UTF-8 text of at most 1 MiB")
	ErrScriptExists     = errors.New("script already exists")
	ErrScriptMissing    = errors.New("script does not exist")
	ErrScriptRefused    = errors.New("script is not a regular file")
	ErrTasksUnavailable = errors.New("scheduled tasks are not writable in this console")
)

// ScriptInfo is one file directly inside the plugin's scripts directory.
// Command and Runnable describe the platform default used by Run and by the
// task form's prefilled command. Result and Log are the latest one-shot run.
type ScriptInfo struct {
	Name     string      `json:"name"`
	Size     int64       `json:"size"`
	Command  string      `json:"command,omitempty"`
	Runnable bool        `json:"runnable"`
	Status   string      `json:"status,omitempty"`
	Result   plugin.Text `json:"result,omitempty"`
	ExitCode *int        `json:"exit_code,omitempty"`
	Reason   string      `json:"reason,omitempty"`
	Log      string      `json:"log,omitempty"`
}

// defaultCommand is the argv offered for Run and for a new task. A file
// without one can still be scheduled once the form has a command.
func defaultCommand(name, goos string) ([]string, bool) {
	file := "scripts/" + name
	switch scriptExt(name) {
	case ".py":
		if goos == "windows" {
			return []string{"py", "-3", file}, true
		}
		return []string{"python3", file}, true
	case ".js", ".mjs":
		return []string{"node", file}, true
	case ".sh":
		if goos == "windows" {
			return nil, false
		}
		return []string{"bash", file}, true
	case ".ps1":
		return []string{"powershell", "-NoProfile", "-File", file}, true
	default:
		return nil, false
	}
}

func scriptExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return strings.ToLower(name[i:])
}

func checkScriptName(name string) error {
	if len(name) > 64 || strings.ContainsAny(name, `/\`) || !scriptName.MatchString(name) {
		return ErrScriptName
	}
	return nil
}

func checkScriptContent(content string) error {
	if len(content) > maxScript || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return ErrScriptContent
	}
	return nil
}

func active(dir string) (*entry, error) {
	if lookup(dir) == nil {
		return nil, ErrTasksUnavailable
	}
	return lookup(dir), nil
}

// ListScripts returns the regular files in scripts/, skipping anything the
// page is not allowed to name.
func ListScripts(dir string) ([]ScriptInfo, error) {
	e, err := active(dir)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	dirFile, err := e.root.Open("scripts")
	if errors.Is(err, fs.ErrNotExist) {
		return []ScriptInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer dirFile.Close()
	entries, err := dirFile.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := []ScriptInfo{}
	for _, entry := range entries {
		name := entry.Name()
		info, infoErr := entry.Info()
		if infoErr != nil || checkScriptName(name) != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, e.decorateScript(ScriptInfo{Name: name, Size: info.Size()}))
	}
	slices.SortFunc(out, func(a, b ScriptInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (e *entry) decorateScript(info ScriptInfo) ScriptInfo {
	if command, ok := defaultCommand(info.Name, runtime.GOOS); ok {
		if line, ok := quoteCommand(command); ok {
			info.Command = line
		}
		info.Runnable = true
	}
	var last *ScriptRun
	for i := len(e.state.Scripts) - 1; i >= 0; i-- {
		if e.state.Scripts[i].File == info.Name {
			last = &e.state.Scripts[i]
			break
		}
	}
	if last == nil {
		return info
	}
	info.Status = last.Status
	info.Result = runStatus(last.Status)
	info.ExitCode = last.ExitCode
	if last.Reason != nil {
		info.Reason = *last.Reason
	}
	info.Log = e.scriptLog(last)
	return info
}

// ReadScript returns the text of one script file.
func ReadScript(dir, name string) (string, error) {
	if err := checkScriptName(name); err != nil {
		return "", err
	}
	e, err := active(dir)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
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
	if info.Size() > maxScript {
		return "", ErrScriptContent
	}
	f, err := e.root.Open("scripts/" + name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxScript+1))
	if err != nil {
		return "", err
	}
	if err := checkScriptContent(string(b)); err != nil {
		return "", err
	}
	return string(b), nil
}

// CreateScript writes a new file. UpdateScript replaces one that is already there.
func CreateScript(dir, name, content string) error { return writeScript(dir, name, content, true) }
func UpdateScript(dir, name, content string) error { return writeScript(dir, name, content, false) }

func writeScript(dir, name, content string, create bool) error {
	if err := checkScriptName(name); err != nil {
		return err
	}
	if err := checkScriptContent(content); err != nil {
		return err
	}
	e, err := active(dir)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stateErr != nil {
		return e.stateErr
	}
	if e.ownerErr != nil {
		return e.ownerErr
	}
	if err := e.claimLocked(); err != nil {
		return err
	}
	if err := e.root.MkdirAll("scripts", 0700); err != nil {
		return err
	}
	info, err := e.root.Lstat("scripts/" + name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !create {
			return ErrScriptMissing
		}
	case err != nil:
		return err
	case info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular():
		return ErrScriptRefused
	case create:
		return ErrScriptExists
	}
	const tmp = "scripts/.write"
	_ = e.root.Remove(tmp)
	f, err := e.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer e.root.Remove(tmp)
	_, err = io.WriteString(f, content)
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
		err = e.root.Rename(tmp, "scripts/"+name)
		if err == nil || !errors.Is(err, fs.ErrPermission) || time.Now().After(until) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// DeleteScript removes one regular script file. A run that is still going is
// cancelled and waited on first, so it cannot write state for a deleted script.
func DeleteScript(dir, name string) error {
	if err := checkScriptName(name); err != nil {
		return err
	}
	e, err := active(dir)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.stateErr != nil {
		e.mu.Unlock()
		return e.stateErr
	}
	if e.ownerErr != nil {
		e.mu.Unlock()
		return e.ownerErr
	}
	if err := e.claimLocked(); err != nil {
		e.mu.Unlock()
		return err
	}
	info, err := e.root.Lstat("scripts/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		e.mu.Unlock()
		return ErrScriptMissing
	}
	if err != nil {
		e.mu.Unlock()
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		e.mu.Unlock()
		return ErrScriptRefused
	}
	var wait <-chan struct{}
	if r := e.scriptRunning[name]; r != nil {
		if r.reason == "" {
			r.reason = "stop"
		}
		r.cancel()
		wait = r.done
	}
	e.mu.Unlock()
	if wait != nil {
		<-wait
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.root.Remove("scripts/" + name)
}
