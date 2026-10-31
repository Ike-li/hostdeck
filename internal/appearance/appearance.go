// Package appearance keeps the console's shared colour preferences.
package appearance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/Ike-li/hostdeck/internal/atomicfile"
)

// Settings is version 1 of appearance.json. An empty Accent uses the preset.
type Settings struct {
	Version int    `json:"version"`
	Preset  string `json:"preset"`
	Mode    string `json:"mode"`
	Accent  string `json:"accent,omitempty"`
}

func Default() Settings { return Settings{Version: 1, Preset: "default", Mode: "system"} }

var colour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func normalized(s Settings) (Settings, error) {
	if s.Version != 1 {
		return Settings{}, fmt.Errorf("unsupported appearance version %d", s.Version)
	}
	switch s.Preset {
	case "default", "forest", "dusk", "sand":
	default:
		return Settings{}, fmt.Errorf("unknown appearance preset %q", s.Preset)
	}
	switch s.Mode {
	case "system", "light", "dark":
	default:
		return Settings{}, fmt.Errorf("unknown appearance mode %q", s.Mode)
	}
	if s.Accent != "" && !colour.MatchString(s.Accent) {
		return Settings{}, errors.New("accent must be a six-digit hex colour")
	}
	s.Accent = strings.ToLower(s.Accent)
	return s, nil
}

// Decode refuses unknown fields and trailing JSON, just as the stored contract does.
func Decode(r io.Reader) (Settings, error) {
	type wireSettings Settings
	var raw struct {
		wireSettings
		Accent json.RawMessage `json:"accent"`
	}
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&raw); err != nil {
		return Settings{}, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Settings{}, errors.New("expected one appearance object")
	}
	s := Settings(raw.wireSettings)
	if raw.Accent != nil {
		if err := json.Unmarshal(raw.Accent, &s.Accent); err != nil {
			return Settings{}, err
		}
		if !colour.MatchString(s.Accent) {
			return Settings{}, errors.New("accent must be a six-digit hex colour")
		}
	}
	return normalized(s)
}

type Store struct {
	mu       sync.Mutex
	file     string
	settings Settings
	warning  string
}

// Open leaves unreadable files alone and keeps the console usable with defaults.
func Open(file string) *Store {
	s := &Store{file: file, settings: Default()}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return s
	}
	if err == nil {
		s.settings, err = Decode(strings.NewReader(string(data)))
	}
	if err != nil {
		s.settings, s.warning = Default(), err.Error()
	}
	return s
}

// Get also works for a console fixture without persistent appearance settings.
func (s *Store) Get() (Settings, string) {
	if s == nil {
		return Default(), ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, s.warning
}

// Set publishes a validated setting only after the whole file is durable.
func (s *Store) Set(next Settings) error {
	if s == nil {
		return errors.New("appearance storage is unavailable")
	}
	value, err := normalized(next)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.file, append(data, '\n')); err != nil {
		return err
	}
	s.settings, s.warning = value, ""
	return nil
}
