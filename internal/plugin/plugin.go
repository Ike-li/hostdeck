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
	"time"

	"go.yaml.in/yaml/v3"
)

// Schema is the manifest version this hostdeck reads.
const Schema = 1

// languages are the ones a Text may be written in.
var languages = []string{"en", "zh-CN"}

// Plugin is one folder under the plugin root; the folder name is its ID.
type Plugin struct {
	ID   string `yaml:"-"`
	Dir  string `yaml:"-"` // the plugin's folder, where its commands run
	Name Text   `yaml:"name"`
	// Description says what the plugin does, for the plugins page.
	Description Text     `yaml:"description"`
	Icon        string   `yaml:"icon"`
	Page        *Page    `yaml:"page"`
	Detail      *Detail  `yaml:"detail"`
	Actions     *Actions `yaml:"actions"`
	Card        *Card    `yaml:"card"`
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

// DefaultInterval is how long a card's value is kept when the manifest says
// nothing.
const DefaultInterval = Duration(time.Minute)

// Card is shown on the overview. A card either asks a URL, and Value is a
// gjson path into the JSON it returns, or runs a command that prints the
// whole card. Its value is kept for Interval before it is asked again.
type Card struct {
	Interval Duration   `yaml:"interval"`
	HTTP     *HTTP      `yaml:"http"`
	Exec     *Exec      `yaml:"exec"`
	Value    string     `yaml:"value"`
	Label    Text       `yaml:"label"`
	Warn     *Threshold `yaml:"warn"`
	Error    *Threshold `yaml:"error"`
	Rows     []Row      `yaml:"rows"`
}

// Row is a line under a declarative card's value; Value is a gjson path.
type Row struct {
	Name  Text       `yaml:"name"`
	Value string     `yaml:"value"`
	Warn  *Threshold `yaml:"warn"`
	Error *Threshold `yaml:"error"`
}

// Threshold is the one condition under which a value turns yellow or red:
// above or below a number, or equal or not equal to a value.
type Threshold struct {
	Above  *float64 `yaml:"above"`
	Below  *float64 `yaml:"below"`
	Equals *Scalar  `yaml:"equals"`
	Not    *Scalar  `yaml:"not"`
}

func (t *Threshold) check(field string) error {
	if t == nil {
		return nil
	}
	set := 0
	for _, ok := range []bool{t.Above != nil, t.Below != nil, t.Equals != nil, t.Not != nil} {
		if ok {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("%s needs exactly one of above, below, equals or not", field)
	}
	return nil
}

// Duration is written as Go writes durations: 30s, 5m, 1h.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil || v <= 0 {
		return fmt.Errorf("line %d: card.interval must be a duration such as 30s or 5m, not %q", n.Line, n.Value)
	}
	*d = Duration(v)
	return nil
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

func (s *Scalar) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: expected a single value", n.Line)
	}
	*s = Scalar(n.Value)
	return nil
}

func (s *Scalar) UnmarshalJSON(data []byte) error {
	var str string
	if json.Unmarshal(data, &str) == nil {
		*s = Scalar(str)
		return nil
	}
	*s = Scalar(data)
	return nil
}

// HTTP is a GET request. URL and the header values may hold ${env:NAME},
// which Expand fills in, so that a token stays out of the manifest.
type HTTP struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

var envRef = regexp.MustCompile(`\$\{([^}]*)\}`)
var envName = regexp.MustCompile(`^env:([A-Za-z_][A-Za-z0-9_]*)$`)

// Expand fills in every ${env:NAME} in s and returns the values it filled in,
// so that errors can hide them.
func Expand(s string) (string, []string) {
	var secrets []string
	out := envRef.ReplaceAllStringFunc(s, func(ref string) string {
		m := envName.FindStringSubmatch(envRef.FindStringSubmatch(ref)[1])
		if m == nil {
			return ref
		}
		v := os.Getenv(m[1])
		if v != "" {
			secrets = append(secrets, v)
		}
		return v
	})
	return out, secrets
}

func checkEnv(field, s string) error {
	for _, ref := range envRef.FindAllStringSubmatch(s, -1) {
		m := envName.FindStringSubmatch(ref[1])
		if m == nil {
			return fmt.Errorf("%s: %q must be written ${env:NAME}", field, ref[0])
		}
		if _, ok := os.LookupEnv(m[1]); !ok {
			return &EnvMissing{Field: field, Name: m[1]}
		}
	}
	return nil
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

// Broken is a plugin folder that did not load. Name is the manifest's name
// when that much could be read. Err names the file and says why.
type Broken struct {
	ID   string
	Name Text
	Err  error
}

// Load reads every folder under root, in name order; a folder whose name
// starts with a dot is skipped. A folder that does not load is set aside in
// broken, so one mistake does not stop the others. err is for root itself.
func Load(root string) (plugins []Plugin, broken []Broken, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(root, e.Name(), "plugin.yaml")
		p, err := load(path)
		if err != nil {
			data, _ := os.ReadFile(path)
			broken = append(broken, Broken{ID: e.Name(), Name: nameIn(data), Err: err})
			continue
		}
		p.ID = e.Name()
		p.Dir = filepath.Join(root, e.Name())
		plugins = append(plugins, p)
	}
	return plugins, broken, nil
}

// nameIn reads only the name, so a manifest that fails to load can still be
// shown by the name its author wrote.
func nameIn(data []byte) Text {
	var head struct {
		Name Text `yaml:"name"`
	}
	if yaml.Unmarshal(data, &head) != nil || head.Name.IsZero() {
		return Text{}
	}
	return head.Name
}

// EnvMissing is a ${env:NAME} whose variable is not set.
type EnvMissing struct {
	Field string
	Name  string
}

func (e *EnvMissing) Error() string {
	return fmt.Sprintf("%s uses ${env:%s}, which is not set", e.Field, e.Name)
}

// Located is an error in one file. Path is that file; Err is the reason,
// without the path.
type Located struct {
	Path string
	Err  error
}

func (e *Located) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *Located) Unwrap() error { return e.Err }

// ReadFolder checks one plugin with the same rules as loading the console.
// It reads only the manifest and never runs a program or contacts a service.
func ReadFolder(dir string) (Plugin, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Plugin{}, err
	}
	p, err := load(filepath.Join(abs, "plugin.yaml"))
	if err != nil {
		return Plugin{}, err
	}
	p.Dir, p.ID = abs, filepath.Base(abs)
	return p, nil
}

func load(path string) (Plugin, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plugin{}, err
	}
	p, err := parse(data)
	if err != nil {
		return Plugin{}, &Located{Path: path, Err: err}
	}
	return p, nil
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
	if m.Card != nil && m.Card.Interval == 0 {
		m.Card.Interval = DefaultInterval
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
	c := m.Card
	switch {
	case (c.HTTP == nil) == (c.Exec == nil):
		return errors.New("card needs exactly one of card.http or card.exec")
	case c.Exec != nil && len(c.Exec.Command) == 0:
		return errors.New("card.exec.command is required")
	case c.Exec != nil && (c.Value != "" || !c.Label.IsZero() || c.Warn != nil || c.Error != nil || c.Rows != nil):
		return errors.New("card.value, card.label, card.warn, card.error and card.rows go with card.http; a card.exec command prints its own")
	case c.Exec != nil:
		return nil
	case c.HTTP.URL == "":
		return errors.New("card.http.url is required")
	case c.Value == "":
		return errors.New("card.value is required")
	}
	if err := checkEnv("card.http.url", c.HTTP.URL); err != nil {
		return err
	}
	// Checked with the references left unfilled, so that no value reaches
	// the message.
	if checkURL("card.http.url", envRef.ReplaceAllString(c.HTTP.URL, "env")) != nil {
		return fmt.Errorf("card.http.url must be an http or https URL, not %q", c.HTTP.URL)
	}
	for name, v := range c.HTTP.Headers {
		if err := checkEnv("card.http.headers."+name, v); err != nil {
			return err
		}
	}
	if err := errors.Join(c.Warn.check("card.warn"), c.Error.check("card.error")); err != nil {
		return err
	}
	for i, r := range c.Rows {
		field := fmt.Sprintf("card.rows[%d]", i)
		if r.Value == "" {
			return fmt.Errorf("%s.value is required", field)
		}
		if err := errors.Join(r.Warn.check(field+".warn"), r.Error.check(field+".error")); err != nil {
			return err
		}
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
