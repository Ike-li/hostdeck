// Package catalog manages the plugins folder for the plugins page: it lists
// the plugins built into hostdeck and adds one, and lists the installed ones
// and disables or enables them. A disabled plugin is its folder renamed to
// start with a dot, which loading skips. Nothing comes from the network.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

var (
	ErrNotWeb   = errors.New("not an http or https address")
	ErrNotLocal = errors.New("not on this machine or the local network")
	ErrUnknown  = errors.New("no such plugin")
	ErrAdded    = errors.New("a plugin folder of that name is already there")
	ErrTaken    = errors.New("the folder that name would be renamed to is already there")
)

// Offers says which parts a plugin's manifest declares. The plugins page
// names them; nothing here is run.
type Offers struct {
	Page    bool `json:"page,omitempty"`
	Card    bool `json:"card,omitempty"`
	Detail  bool `json:"detail,omitempty"`
	Actions bool `json:"actions,omitempty"`
}

// Entry is a built-in plugin as the plugins page lists it.
type Entry struct {
	ID          string      `json:"id"`
	Name        plugin.Text `json:"name"`
	Description plugin.Text `json:"description,omitzero"`
	// Added says that pluginDir has a folder of this name, enabled or
	// disabled, whatever it holds.
	Added bool `json:"added"`
	Offers
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
		entries = append(entries, Entry{ID: d.Name(), Name: m.Name, Description: m.Description, Added: present(pluginDir, d.Name()), Offers: m.offers()})
	}
	return entries, nil
}

// Add copies the built-in plugin id into pluginDir and returns where. It never
// replaces a folder that is there.
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
	if err := place(pluginDir, id, func(dir string) error { return os.CopyFS(dir, sub) }); err != nil {
		return "", err
	}
	return target, nil
}

// place makes the folder pluginDir/id with fill. The folder is filled under a
// name Load skips and then renamed, so a failure leaves no half-written plugin
// behind.
func place(pluginDir, id string, fill func(dir string) error) error {
	tmp, err := os.MkdirTemp(pluginDir, ".adding-")
	if err != nil {
		return err
	}
	err = os.Chmod(tmp, 0o755)
	if err == nil {
		err = fill(tmp)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(pluginDir, id))
	}
	if err != nil {
		os.RemoveAll(tmp)
	}
	return err
}

// AddPage adds a plugin that embeds the web page at address, which may leave
// out http://, and returns its ID. The page must be on this machine or the
// local network: the proxy sends it 127.0.0.1 as its host, which a site on the
// internet would not answer to. Without a name, the page is named by its host.
func AddPage(pluginDir, address, name string) (string, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", ErrNotWeb
	}
	if !onLocalNetwork(u.Hostname()) {
		return "", ErrNotLocal
	}
	if name = strings.TrimSpace(name); name == "" {
		name = u.Host
	}
	manifest, err := yaml.Marshal(map[string]any{"schema": 1, "name": name, "page": map[string]string{"proxy": u.String()}})
	if err != nil {
		return "", err
	}
	base := pageID(u)
	id := base
	for n := 2; present(pluginDir, id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	err = place(pluginDir, id, func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "plugin.yaml"), manifest, 0o644)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// pageID names a page's folder after its host and port, in letters, digits
// and dashes.
func pageID(u *url.URL) string {
	var b strings.Builder
	for _, r := range strings.ToLower(u.Hostname() + "-" + u.Port()) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case !strings.HasSuffix(b.String(), "-") && b.Len() > 0:
			b.WriteByte('-')
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// onLocalNetwork says whether every address host stands for is loopback,
// private or link-local. A name that does not resolve in a few seconds is not.
func onLocalNetwork(host string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		if a = a.Unmap(); !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() {
			return false
		}
	}
	return true
}

// about is what the plugins page shows of a manifest. A page, card, detail
// or actions key is recorded only as present: the page does not read them.
type about struct {
	Name        plugin.Text `yaml:"name"`
	Description plugin.Text `yaml:"description"`
	Page        *yaml.Node  `yaml:"page"`
	Card        *yaml.Node  `yaml:"card"`
	Detail      *yaml.Node  `yaml:"detail"`
	Actions     *yaml.Node  `yaml:"actions"`
}

func (m about) offers() Offers {
	return Offers{Page: m.Page != nil, Card: m.Card != nil, Detail: m.Detail != nil, Actions: m.Actions != nil}
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
	Offers
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
		plugins = append(plugins, Plugin{ID: id, Name: m.Name, Description: m.Description, Disabled: disabled, Offers: m.offers()})
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
