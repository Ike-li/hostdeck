package layout

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestALayoutIsKeptAcrossRuns(t *testing.T) {
	file := filepath.Join(t.TempDir(), "layout.json")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); !reflect.DeepEqual(got, Layout{}) {
		t.Errorf("before anything is arranged: %+v, want nothing", got)
	}

	want := Layout{Sidebar: []string{"grafana", "disk"}, Cards: []string{"disk", "grafana"}, Wide: []string{"disk"}}
	if err := s.Set(want); err != nil {
		t.Fatal(err)
	}
	again, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get(); !reflect.DeepEqual(got, want) {
		t.Errorf("reopened: %+v, want %+v", got, want)
	}
}

func TestABrokenLayoutFileIsNamed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "layout.json")
	os.WriteFile(file, []byte("{"), 0o644)
	if _, err := Open(file); err == nil || !strings.Contains(err.Error(), file) {
		t.Errorf("err %v, want one naming %s", err, file)
	}
}

func TestArrangedIDsComeFirstAndTheRestKeepTheirOrder(t *testing.T) {
	loaded := []string{"a", "b", "c", "d"}
	// x is not loaded; b and d were never arranged, or were added since.
	got := Arrange(loaded, []string{"c", "x", "a"}, func(id string) string { return id })
	if want := []string{"c", "a", "b", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if want := []string{"a", "b", "c", "d"}; !reflect.DeepEqual(loaded, want) {
		t.Errorf("the input became %v", loaded)
	}
}

func TestChangingWhatGetReturnsLeavesTheStoreAlone(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "layout.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.Set(Layout{Sidebar: []string{"a", "b"}})
	l := s.Get()
	l.Sidebar[0] = "changed"
	if got := s.Get().Sidebar; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("store holds %v, want [a b]", got)
	}
}
