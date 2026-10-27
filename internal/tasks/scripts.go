package tasks

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
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
type ScriptInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
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
		out = append(out, ScriptInfo{Name: name, Size: info.Size()})
	}
	slices.SortFunc(out, func(a, b ScriptInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
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

// DeleteScript removes one regular script file.
func DeleteScript(dir, name string) error {
	if err := checkScriptName(name); err != nil {
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
	info, err := e.root.Lstat("scripts/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrScriptMissing
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrScriptRefused
	}
	return e.root.Remove("scripts/" + name)
}
