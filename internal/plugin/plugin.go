// Package plugin reads plugin folders: <root>/<id>/plugin.yaml.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Schema is the manifest version this hostdeck reads.
const Schema = 1

// languages are the ones a Text may be written in.
var languages = []string{"en", "zh-CN"}

// Plugin is one folder under the plugin root; the folder name is its ID.
type Plugin struct {
	ID      string   `yaml:"-"`
	Dir     string   `yaml:"-"` // the plugin's folder, where its commands run
	Name    Text     `yaml:"name"`
	Icon    string   `yaml:"icon"`
	Page    *Page    `yaml:"page"`
	Detail  *Detail  `yaml:"detail"`
	Actions *Actions `yaml:"actions"`
	Card    *Card    `yaml:"card"`
}

// Detail is a sidebar entry that the console draws itself, from the items its
// command prints.
type Detail struct {
	Exec *Exec `yaml:"exec"`
}

// Actions run on items of the detail page. The command gets the action's ID
// and the item's ID as its last two arguments.
type Actions struct {
	Exec *Exec `yaml:"exec"`
}

// Page is what the plugin's sidebar entry opens: Proxy is a local web UI that
// the console embeds, Link opens in a new tab.
type Page struct {
	Proxy string `yaml:"proxy"`
	Link  string `yaml:"link"`
}

// Card is shown on the overview. A card either asks a URL, and Value is a
// gjson path into the JSON it returns, or runs a command that prints the
// whole card.
type Card struct {
	HTTP  *HTTP  `yaml:"http"`
	Exec  *Exec  `yaml:"exec"`
	Value string `yaml:"value"`
	Label Text   `yaml:"label"`
}

// Exec is a command and its arguments. Windows, when given, replaces Command
// on Windows, which has no sh.
type Exec struct {
	Command []string `yaml:"command"`
	Windows []string `yaml:"windows"`
}

// Args is the command to run on this machine.
func (e Exec) Args() []string {
	if runtime.GOOS == "windows" && len(e.Windows) > 0 {
		return e.Windows
	}
	return e.Command
}

// Scalar is a value that a plugin's command prints as a string or a number.
type Scalar string

func (s *Scalar) UnmarshalJSON(data []byte) error {
	var str string
	if json.Unmarshal(data, &str) == nil {
		*s = Scalar(str)
		return nil
	}
	*s = Scalar(data)
	return nil
}

type HTTP struct {
	URL string `yaml:"url"`
}

// Text is shown to people: either one string for every language, or one string
// per language.
type Text struct {
	All    string
	ByLang map[string]string
}

func (t *Text) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		t.All = n.Value
		return nil
	}
	var byLang map[string]string
	if err := n.Decode(&byLang); err != nil {
		return err
	}
	for lang := range byLang {
		if !slices.Contains(languages, lang) {
			return fmt.Errorf("line %d: unknown language %q, use one of %s", n.Line, lang, strings.Join(languages, ", "))
		}
	}
	t.ByLang = byLang
	return nil
}

// MarshalJSON writes a string or an object of languages, as the manifest had it;
// the page picks the language.
func (t Text) MarshalJSON() ([]byte, error) {
	if t.ByLang != nil {
		return json.Marshal(t.ByLang)
	}
	return json.Marshal(t.All)
}

// UnmarshalJSON reads what a plugin's command prints: a string, or an object
// of languages.
func (t *Text) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return json.Unmarshal(data, &t.ByLang)
	}
	return json.Unmarshal(data, &t.All)
}

func (t Text) IsZero() bool {
	return t.All == "" && len(t.ByLang) == 0
}

// manifest is what plugin.yaml holds.
type manifest struct {
	Schema int `yaml:"schema"`
	Plugin `yaml:",inline"`
}

// Load reads every folder under root, in name order. A folder whose name starts
// with a dot is skipped.
func Load(root string) ([]Plugin, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var plugins []Plugin
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(root, e.Name(), "plugin.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		p, err := parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		p.ID = e.Name()
		p.Dir = filepath.Join(root, e.Name())
		plugins = append(plugins, p)
	}
	return plugins, nil
}

func parse(data []byte) (Plugin, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m manifest
	if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return Plugin{}, reword(err)
	}
	if err := m.validate(); err != nil {
		return Plugin{}, err
	}
	return m.Plugin, nil
}

func (m manifest) validate() error {
	switch {
	case m.Schema == 0:
		return fmt.Errorf("schema is missing; this hostdeck reads schema: %d", Schema)
	case m.Schema != Schema:
		return fmt.Errorf("schema %d is not one this hostdeck reads; it reads schema: %d", m.Schema, Schema)
	case m.Name.IsZero():
		return errors.New("name is required")
	case m.Page == nil && m.Detail == nil && m.Card == nil:
		return errors.New("page, detail or card is required")
	case m.Page != nil && m.Detail != nil:
		return errors.New("a plugin has one sidebar entry: page or detail, not both")
	case m.Detail != nil && (m.Detail.Exec == nil || len(m.Detail.Exec.Command) == 0):
		return errors.New("detail.exec.command is required")
	case m.Actions != nil && m.Detail == nil:
		return errors.New("actions act on the items of a detail page, and this plugin has no detail")
	case m.Actions != nil && (m.Actions.Exec == nil || len(m.Actions.Exec.Command) == 0):
		return errors.New("actions.exec.command is required")
	}
	if m.Page != nil {
		if err := m.Page.validate(); err != nil {
			return err
		}
	}
	if m.Card == nil {
		return nil
	}
	switch c := m.Card; {
	case (c.HTTP == nil) == (c.Exec == nil):
		return errors.New("card needs exactly one of card.http or card.exec")
	case c.Exec != nil && len(c.Exec.Command) == 0:
		return errors.New("card.exec.command is required")
	case c.Exec != nil && (c.Value != "" || !c.Label.IsZero()):
		return errors.New("card.value and card.label go with card.http; a card.exec command prints its own")
	case c.HTTP != nil && c.HTTP.URL == "":
		return errors.New("card.http.url is required")
	case c.HTTP != nil && c.Value == "":
		return errors.New("card.value is required")
	}
	return nil
}

func (pg *Page) validate() error {
	switch {
	case (pg.Proxy == "") == (pg.Link == ""):
		return errors.New("page needs exactly one of page.proxy or page.link")
	case pg.Proxy != "":
		return checkURL("page.proxy", pg.Proxy)
	default:
		return checkURL("page.link", pg.Link)
	}
}

func checkURL(field, s string) error {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an http or https URL, not %q", field, s)
	}
	return nil
}

var unknownField = regexp.MustCompile(`^(line \d+): field (\S+) not found in type \S+$`)

// reword replaces the Go type names in the YAML decoder's messages with the
// field's name as written in the manifest.
func reword(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return err
	}
	msgs := make([]string, len(te.Errors))
	for i, msg := range te.Errors {
		msgs[i] = unknownField.ReplaceAllString(msg, `$1: unknown field "$2"`)
	}
	return errors.New(strings.Join(msgs, "; "))
}
