// Package plugin reads plugin folders: <root>/<id>/plugin.yaml.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	ID   string `yaml:"-"`
	Name Text   `yaml:"name"`
	Icon string `yaml:"icon"`
	Card *Card  `yaml:"card"`
}

// Card is shown on the overview. Value is a gjson path into the JSON that
// HTTP returns.
type Card struct {
	HTTP  *HTTP  `yaml:"http"`
	Value string `yaml:"value"`
	Label Text   `yaml:"label"`
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
	case m.Card == nil:
		return errors.New("card is required")
	case m.Card.HTTP == nil:
		return errors.New("card.http is required")
	case m.Card.HTTP.URL == "":
		return errors.New("card.http.url is required")
	case m.Card.Value == "":
		return errors.New("card.value is required")
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
