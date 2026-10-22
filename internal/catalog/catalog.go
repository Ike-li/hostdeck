// Package catalog manages the plugins folder for the plugins page: it lists
// the plugins built into hostdeck and adds one, lists the installed ones and
// disables or enables them, and edits the name, icon and addresses an
// installed plugin is allowed to change. A disabled plugin is its folder
// renamed to start with a dot, which loading skips. Nothing comes from the
// network.
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

	"github.com/Ike-li/hostdeck/internal/atomicfile"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

var (
	ErrNotWeb   = errors.New("not an http or https address")
	ErrNotLocal = errors.New("not on this machine or the local network")
	ErrUnknown  = errors.New("no such plugin")
	ErrAdded    = errors.New("a plugin folder of that name is already there")
	ErrTaken    = errors.New("the folder that name would be renamed to is already there")
	// ErrBoth means the edit named both an embed address and a link.
	ErrBoth = errors.New("proxy and link are both set")
	// ErrNoCardURL means the manifest has no declarative card.http.url to change.
	ErrNoCardURL = errors.New("this plugin has no declarative card address")
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
	return addPage(pluginDir, address, name, true)
}

// AddLink adds a plugin that opens address in a new tab, and returns its ID.
// The address may leave out http://. It must be http or https and need not be
// on this machine. The manifest holds a name and page.link, and no command.
func AddLink(pluginDir, address, name string) (string, error) {
	return addPage(pluginDir, address, name, false)
}

func addPage(pluginDir, address, name string, embed bool) (string, error) {
	var pageURL string
	var err error
	if embed {
		pageURL, err = localPage(address)
	} else {
		if !strings.Contains(address, "://") {
			address = "http://" + address
		}
		pageURL, err = webPage(address)
	}
	if err != nil {
		return "", err
	}
	u, err := url.Parse(pageURL)
	if err != nil || u.Hostname() == "" {
		return "", ErrNotWeb
	}
	if name = strings.TrimSpace(name); name == "" {
		name = u.Host
	}
	key := "proxy"
	if !embed {
		key = "link"
	}
	manifest, err := yaml.Marshal(map[string]any{"schema": 1, "name": name, "page": map[string]string{key: pageURL}})
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

// PageChange is what the console may write into an existing manifest. A nil
// field stays as it is. Proxy and Link cannot both be set. CardURL changes
// card.http.url only when that key is already there.
type PageChange struct {
	Name    *string
	Icon    *string
	Proxy   *string
	Link    *string
	CardURL *string
}

// EditPage writes the allowlisted fields of one plugin's manifest and leaves
// every other line, including its commands, as it was. The folder is not
// renamed, so a remembered forwarding port stays with this plugin.
func EditPage(pluginDir, id string, change PageChange) error {
	if !plainName(id) {
		return ErrUnknown
	}
	if change.Proxy != nil && change.Link != nil {
		return ErrBoth
	}
	if change.Proxy != nil {
		proxy, err := localPage(*change.Proxy)
		if err != nil {
			return err
		}
		change.Proxy = &proxy
	}
	if change.Link != nil {
		link, err := webPage(*change.Link)
		if err != nil {
			return err
		}
		change.Link = &link
	}
	if change.CardURL != nil {
		cardURL, err := webPage(*change.CardURL)
		if err != nil {
			return err
		}
		change.CardURL = &cardURL
	}
	path := filepath.Join(pluginDir, id, "plugin.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return ErrUnknown
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	root := mappingOf(&doc)
	if root == nil {
		return fmt.Errorf("plugin.yaml is not a mapping")
	}
	if change.CardURL != nil && cardHTTP(root) == nil {
		return ErrNoCardURL
	}
	if change.Name != nil {
		setScalar(root, "name", *change.Name)
	}
	if change.Icon != nil {
		setScalar(root, "icon", *change.Icon)
	}
	if change.Proxy != nil {
		page := ensureMapping(root, "page")
		setScalar(page, "proxy", *change.Proxy)
		deleteKey(page, "link")
	}
	if change.Link != nil {
		page := ensureMapping(root, "page")
		setScalar(page, "link", *change.Link)
		deleteKey(page, "proxy")
	}
	if change.CardURL != nil {
		setScalar(cardHTTP(root), "url", *change.CardURL)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, out)
}

// webPage reports whether address is an http or https URL.
func webPage(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", ErrNotWeb
	}
	return u.String(), nil
}

// localPage checks address the way AddPage does and returns it as a URL.
func localPage(address string) (string, error) {
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
	return u.String(), nil
}

// mappingOf is the document's mapping, if that is what it holds.
func mappingOf(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

func field(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// cardHTTP is the card.http mapping when it already has a url.
func cardHTTP(root *yaml.Node) *yaml.Node {
	card := field(root, "card")
	if card == nil || card.Kind != yaml.MappingNode {
		return nil
	}
	httpNode := field(card, "http")
	if httpNode == nil || httpNode.Kind != yaml.MappingNode || field(httpNode, "url") == nil {
		return nil
	}
	return httpNode
}

func deleteKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = slices.Delete(mapping.Content, i, i+2)
			return
		}
	}
}

// setScalar writes key as a single string, replacing a language map or any
// other value already there.
func setScalar(mapping *yaml.Node, key, value string) {
	if n := field(mapping, key); n != nil {
		*n = yaml.Node{Kind: yaml.ScalarNode, Value: value}
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

// ensureMapping returns the mapping at key, replacing whatever else was there.
func ensureMapping(parent *yaml.Node, key string) *yaml.Node {
	if n := field(parent, key); n != nil && n.Kind == yaml.MappingNode {
		return n
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	if old := field(parent, key); old != nil {
		*old = *n
		return old
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		n,
	)
	return n
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
// or actions key is recorded as present. The page also reads the icon and the
// addresses it is allowed to edit, and does not read a command.
type about struct {
	Name        plugin.Text `yaml:"name"`
	Description plugin.Text `yaml:"description"`
	Icon        string      `yaml:"icon"`
	Page        yaml.Node   `yaml:"page"`
	Card        yaml.Node   `yaml:"card"`
	Detail      yaml.Node   `yaml:"detail"`
	Actions     yaml.Node   `yaml:"actions"`
}

// editable is the icon and the addresses the console may change. Commands
// are not decoded.
func (m about) editable() (icon, proxy, link, cardURL string) {
	icon = m.Icon
	var page struct {
		Proxy string `yaml:"proxy"`
		Link  string `yaml:"link"`
	}
	if !m.Page.IsZero() {
		m.Page.Decode(&page)
	}
	var card struct {
		HTTP *struct {
			URL string `yaml:"url"`
		} `yaml:"http"`
	}
	if !m.Card.IsZero() {
		m.Card.Decode(&card)
	}
	if card.HTTP != nil {
		cardURL = card.HTTP.URL
	}
	return icon, page.Proxy, page.Link, cardURL
}

func (m about) offers() Offers {
	return Offers{Page: !m.Page.IsZero(), Card: !m.Card.IsZero(), Detail: !m.Detail.IsZero(), Actions: !m.Actions.IsZero()}
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
	Icon        string      `json:"icon,omitempty"`
	Proxy       string      `json:"proxy,omitempty"`
	Link        string      `json:"link,omitempty"`
	CardURL     string      `json:"cardUrl,omitempty"`
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
		icon, proxy, link, cardURL := m.editable()
		plugins = append(plugins, Plugin{
			ID: id, Name: m.Name, Description: m.Description, Icon: icon,
			Proxy: proxy, Link: link, CardURL: cardURL,
			Disabled: disabled, Offers: m.offers(),
		})
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
