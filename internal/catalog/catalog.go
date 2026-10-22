// Package catalog lists the plugins built into hostdeck and adds one to the
// plugins folder, for the plugins page. Nothing comes from the network.
package catalog

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

var (
	ErrUnknown = errors.New("not a plugin built into hostdeck")
	ErrAdded   = errors.New("a plugin folder of that name is already there")
)

// Entry is a built-in plugin as the plugins page lists it.
type Entry struct {
	ID          string      `json:"id"`
	Name        plugin.Text `json:"name"`
	Description plugin.Text `json:"description,omitzero"`
	// Added says that pluginDir has a folder of this name, whatever it holds.
	Added bool `json:"added"`
}

// List returns the plugins in builtIn, one per top-level folder. Only their
// name and description are read: a plugin that needs an environment variable
// is listed whether or not it is set.
func List(builtIn fs.FS, pluginDir string) ([]Entry, error) {
	dirs, err := fs.ReadDir(builtIn, ".")
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		data, err := fs.ReadFile(builtIn, path.Join(d.Name(), "plugin.yaml"))
		if err != nil {
			return nil, err
		}
		var m struct {
			Name        plugin.Text `yaml:"name"`
			Description plugin.Text `yaml:"description"`
		}
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		_, err = os.Lstat(filepath.Join(pluginDir, d.Name()))
		entries = append(entries, Entry{ID: d.Name(), Name: m.Name, Description: m.Description, Added: err == nil})
	}
	return entries, nil
}

// Add copies the built-in plugin id into pluginDir and returns where. It never
// replaces a folder that is there. The copy is made under a name Load skips
// and then renamed, so a failure leaves no half-written plugin behind.
func Add(builtIn fs.FS, pluginDir, id string) (string, error) {
	if !fs.ValidPath(id) || id == "." || strings.Contains(id, "/") {
		return "", ErrUnknown
	}
	if info, err := fs.Stat(builtIn, id); err != nil || !info.IsDir() {
		return "", ErrUnknown
	}
	target := filepath.Join(pluginDir, id)
	if _, err := os.Lstat(target); err == nil {
		return "", ErrAdded
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	sub, err := fs.Sub(builtIn, id)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(pluginDir, ".adding-")
	if err != nil {
		return "", err
	}
	err = os.Chmod(tmp, 0o755)
	if err == nil {
		err = os.CopyFS(tmp, sub)
	}
	if err == nil {
		err = os.Rename(tmp, target)
	}
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return target, nil
}
