package appearance

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAMissingAppearanceUsesDefaultsWithoutCreatingAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "appearance.json")
	s := Open(file)
	if got, warning := s.Get(); got != Default() || warning != "" {
		t.Fatalf("missing file: %+v, %q", got, warning)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("read wrote file: %v", err)
	}
}

func TestASavedAppearanceSurvivesRestartAndNormalizesItsColour(t *testing.T) {
	file := filepath.Join(t.TempDir(), "appearance.json")
	s := Open(file)
	want := Settings{Version: 1, Preset: "forest", Mode: "dark", Accent: "#aa33cc"}
	next := want
	next.Accent = "#AA33CC"
	if err := s.Set(next); err != nil {
		t.Fatal(err)
	}
	if got, warning := Open(file).Get(); got != want || warning != "" {
		t.Fatalf("restart: %+v, %q; want %+v", got, warning, want)
	}
}

func TestInvalidAppearancesLeaveTheSavedFileAndReadingAlone(t *testing.T) {
	file := filepath.Join(t.TempDir(), "appearance.json")
	s := Open(file)
	if err := s.Set(Default()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(file)
	for _, raw := range []string{
		`null`, `{}`, `{"version":2,"preset":"default","mode":"system"}`,
		`{"version":1,"preset":"unknown","mode":"system"}`,
		`{"version":1,"preset":"default","mode":"auto"}`,
		`{"version":1,"preset":"default","mode":"system","accent":"red"}`,
		`{"version":1,"preset":"default","mode":"system","accent":""}`,
		`{"version":1,"preset":"default","mode":"system","accent":null}`,
		`{"version":1,"preset":"default","mode":"system","accent":"#fff"}`,
		`{"version":1,"preset":"default","mode":"system","accent":"#12345678"}`,
		`{"version":1,"preset":"default","mode":"system","css":"x"}`,
		`{"version":1,"preset":"default","mode":"system"} {}`,
	} {
		if _, err := Decode(strings.NewReader(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	invalid := Default()
	invalid.Mode = "bad"
	if err := s.Set(invalid); err == nil {
		t.Fatal("Set accepted invalid mode")
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) {
		t.Fatal("invalid settings changed file")
	}
	if got, _ := s.Get(); got != Default() {
		t.Fatalf("invalid settings changed reading: %+v", got)
	}
}

func TestABrokenAppearanceKeepsItsBytesUntilAnExplicitSave(t *testing.T) {
	for _, raw := range []string{`{broken`, `{"version":2,"preset":"default","mode":"system"}`} {
		file := filepath.Join(t.TempDir(), "appearance.json")
		if err := os.WriteFile(file, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		s := Open(file)
		if got, warning := s.Get(); got != Default() || warning == "" {
			t.Fatalf("broken file: %+v, %q", got, warning)
		}
		if data, _ := os.ReadFile(file); string(data) != raw {
			t.Fatal("read overwrote broken file")
		}
		if err := s.Set(Default()); err != nil {
			t.Fatal(err)
		}
		if _, warning := s.Get(); warning != "" {
			t.Fatalf("save kept warning: %s", warning)
		}
	}
}

func TestAFailedAppearanceSaveKeepsThePreviousFileAndReading(t *testing.T) {
	file := filepath.Join(t.TempDir(), "appearance.json")
	s := Open(file)
	if err := s.Set(Default()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(file)
	// A directory at the atomic writer's temporary path fails on both platforms.
	if err := os.Mkdir(file+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	next := Default()
	next.Preset = "dusk"
	if err := s.Set(next); err == nil {
		t.Fatal("save succeeded with blocked temporary path")
	}
	after, _ := os.ReadFile(file)
	if string(after) != string(before) {
		t.Fatal("failed save changed file")
	}
	if got, _ := s.Get(); got != Default() {
		t.Fatalf("failed save changed reading: %+v", got)
	}
}

func TestConcurrentAppearanceSavesPublishWholeSettings(t *testing.T) {
	file := filepath.Join(t.TempDir(), "appearance.json")
	s := Open(file)
	var writers sync.WaitGroup
	results := make(chan error, 16)
	for i := range 16 {
		writers.Go(func() {
			next := Default()
			next.Preset = []string{"default", "forest", "dusk", "sand"}[i%4]
			results <- s.Set(next)
		})
	}
	writers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	memory, warning := s.Get()
	disk, readWarning := Open(file).Get()
	if warning != "" || readWarning != "" || memory != disk {
		t.Fatalf("memory %+v (%s), disk %+v (%s)", memory, warning, disk, readWarning)
	}
}
