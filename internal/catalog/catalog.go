// Package catalog manages the plugins folder for the plugins page: it lists
// the plugins built into hostdeck and adds one, and lists the installed ones
// and disables or enables them. A disabled plugin is its folder renamed to
// start with a dot, which loading skips. Nothing comes from the network.
package catalog

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

var (
	ErrUnknown = errors.New("no such plugin")
	ErrAdded   = errors.New("a plugin folder of that name is already there")
	ErrTaken   = errors.New("the folder that name would be renamed to is already there")
)

// Entry is a built-in plugin as the plugins page lists it.
type Entry struct {
	ID          string      `json:"id"`
	Name        plugin.Text `json:"name"`
	Description plugin.Text `json:"description,omitzero"`
	// Added says that pluginDir has a folder of this name, enabled or
	// disabled, whatever it holds.
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
		var m about
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		entries = append(entries, Entry{ID: d.Name(), Name: m.Name, Description: m.Description, Added: present(pluginDir, d.Name())})
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
	if present(pluginDir, id) {
		return "", ErrAdded
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

// about is what the plugins page shows of a manifest.
type about struct {
	Name        plugin.Text `yaml:"name"`
	Description plugin.Text `yaml:"description"`
}

// present says whether pluginDir has a folder for id, enabled or disabled.
func present(pluginDir, id string) bool {
	for _, name := range []string{id, "." + id} {
		if _, err := os.Lstat(filepath.Join(pluginDir, name)); err == nil {
			return true
		}
	}
	return false
}

// Plugin is a folder in the plugins folder as the plugins page lists it.
type Plugin struct {
	ID          string      `json:"id"`
	Name        plugin.Text `json:"name"`
	Description plugin.Text `json:"description,omitzero"`
	Disabled    bool        `json:"disabled"`
}

// Installed lists the plugin folders in pluginDir by ID: each folder, and each
// folder whose name starts with a dot and that holds a plugin.yaml, as a
// disabled plugin. A manifest that does not read leaves the ID as the name.
func Installed(pluginDir string) ([]Plugin, error) {
	dirs, err := os.ReadDir(pluginDir)
	if err != nil {
		return nil, err
	}
	var plugins []Plugin
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		id, disabled := strings.CutPrefix(d.Name(), ".")
		data, err := os.ReadFile(filepath.Join(pluginDir, d.Name(), "plugin.yaml"))
		if disabled && (err != nil || !plainName(id)) {
			continue
		}
		var m about
		if err != nil || yaml.Unmarshal(data, &m) != nil || m.Name.IsZero() {
			m = about{Name: plugin.Text{All: id}}
		}
		plugins = append(plugins, Plugin{ID: id, Name: m.Name, Description: m.Description, Disabled: disabled})
	}
	slices.SortFunc(plugins, func(a, b Plugin) int { return strings.Compare(a.ID, b.ID) })
	return plugins, nil
}

// Disable renames the plugin folder id so that loading skips it.
func Disable(pluginDir, id string) error {
	return rename(pluginDir, id, "."+id)
}

// Enable renames the disabled plugin folder id back.
func Enable(pluginDir, id string) error {
	if _, err := os.Stat(filepath.Join(pluginDir, "."+id, "plugin.yaml")); err != nil {
		return ErrUnknown
	}
	return rename(pluginDir, "."+id, id)
}

// rename moves pluginDir/from to pluginDir/to, never replacing a folder.
func rename(pluginDir, from, to string) error {
	if !plainName(strings.TrimPrefix(from, ".")) {
		return ErrUnknown
	}
	if info, err := os.Stat(filepath.Join(pluginDir, from)); err != nil || !info.IsDir() {
		return ErrUnknown
	}
	if _, err := os.Lstat(filepath.Join(pluginDir, to)); err == nil {
		return ErrTaken
	}
	return os.Rename(filepath.Join(pluginDir, from), filepath.Join(pluginDir, to))
}

// plainName says whether id can name a plugin folder: one path element, not
// hidden, and no separator on any platform.
func plainName(id string) bool {
	return id != "" && !strings.HasPrefix(id, ".") && !strings.ContainsAny(id, `/\`)
}
