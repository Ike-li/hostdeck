// Package layout remembers how the user arranged the console: the order of
// the sidebar, the order of the overview's cards, and which cards are wide.
package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sync"

	"github.com/Ike-li/hostdeck/internal/atomicfile"
)

// Layout lists plugin IDs. The console page saves the IDs it shows, so a
// plugin that is not loaded keeps its place only until its list is saved again.
type Layout struct {
	Sidebar []string `json:"sidebar,omitempty"`
	Cards   []string `json:"cards,omitempty"`
	Wide    []string `json:"wide,omitempty"`
}

// Store holds the layout and the file it is kept in.
type Store struct {
	file string
	mu   sync.Mutex
	l    Layout
}

// Open reads file; a file that is not there is a layout with nothing arranged.
func Open(file string) (*Store, error) {
	s := &Store{file: file}
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &s.l); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	return s, nil
}

func (s *Store) Get() Layout {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Layout{slices.Clone(s.l.Sidebar), slices.Clone(s.l.Cards), slices.Clone(s.l.Wide)}
}

func (s *Store) Set(l Layout) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.file, data); err != nil {
		return err
	}
	s.l = l
	return nil
}

// Arrange returns items in order, by their IDs; items whose ID order does not
// hold follow, as they came.
func Arrange[T any](items []T, order []string, id func(T) string) []T {
	at := make(map[string]int, len(order))
	for i, o := range order {
		if _, ok := at[o]; !ok {
			at[o] = i
		}
	}
	rank := func(item T) int {
		if i, ok := at[id(item)]; ok {
			return i
		}
		return len(order)
	}
	arranged := slices.Clone(items)
	slices.SortStableFunc(arranged, func(a, b T) int { return rank(a) - rank(b) })
	return arranged
}
