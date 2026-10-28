// Package observation owns bounded service observations independently of page lifetimes.
package observation

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Ike-li/hostdeck/internal/atomicfile"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

const FragmentLimit = 256 << 10

var ErrSession = errors.New("observation does not match this operation")

type Identity struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
}

func (a Identity) Equal(b Identity) bool {
	return a.PID > 1 && a.PID == b.PID && !a.Started.IsZero() && a.Started.Equal(b.Started)
}

type Target struct {
	Identity Identity
	State    string
	ExitCode *int
	Signal   string
	Runs     string
	Files    []string
	Reason   string
	Text     string // Bounded service-management events, not application output.
}
type Source struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	State string `json:"state"`
}
type Snapshot struct {
	ID             string      `json:"id"`
	Plugin         string      `json:"plugin"`
	Item           string      `json:"item"`
	Action         string      `json:"action,omitempty"`
	OperationTime  time.Time   `json:"operationTime,omitempty"`
	Result         string      `json:"result,omitempty"`
	Message        plugin.Text `json:"message,omitzero"`
	Error          string      `json:"error,omitempty"`
	CollectedAt    time.Time   `json:"collectedAt"`
	Identity       Identity    `json:"identity"`
	State          string      `json:"state"`
	ExitCode       *int        `json:"exitCode,omitempty"`
	Signal         string      `json:"signal,omitempty"`
	Runs           string      `json:"runs,omitempty"`
	Sources        []Source    `json:"sources"`
	ExistingLog    string      `json:"existingLog,omitempty"`
	Log            string      `json:"log"`
	Cursor         string      `json:"cursor"`
	Reset          bool        `json:"reset"`
	AfterOperation bool        `json:"afterOperation"`
	Partial        bool        `json:"partial"`
}
type fileState struct {
	info   os.FileInfo
	size   int64
	anchor []byte
}
type session struct {
	snap    Snapshot
	files   map[string]fileState
	target  Target
	used    bool
	touched time.Time
}
type Store struct {
	mu       sync.Mutex
	root     string
	sessions map[string]*session
}

func New(root string) *Store { return &Store{root: root, sessions: map[string]*session{}} }
func token() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func validID(id string) bool {
	if len(id) != 48 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func tail(s string) string {
	if len(s) > FragmentLimit {
		s = s[len(s)-FragmentLimit:]
		for len(s) > 0 && !utf8.RuneStart(s[0]) {
			s = s[1:]
		}
	}
	return strings.ToValidUTF8(s, "�")
}

func (s *Store) Create(plugin, item, action string, t Target) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, v := range s.sessions {
		if time.Since(v.touched) > time.Hour {
			delete(s.sessions, id)
		}
	}
	if len(s.sessions) >= 512 {
		return "", errors.New("too many observations")
	}
	id := token()
	s.sessions[id] = &session{snap: Snapshot{ID: id, Plugin: plugin, Item: item, Action: action, State: "unknown"}, target: t, files: map[string]fileState{}, touched: time.Now()}
	return id, nil
}
func (s *Store) Binding(id string) (string, string, string, Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.load(id)
	if err != nil {
		return "", "", "", Identity{}, err
	}
	identity := v.target.Identity
	if identity.Started.IsZero() {
		identity = v.snap.Identity
	}
	return v.snap.Plugin, v.snap.Item, v.snap.Action, identity, nil
}
func (s *Store) Begin(id, plugin, item, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.load(id)
	if err != nil {
		return err
	}
	if v.used || v.snap.Plugin != plugin || v.snap.Item != item || v.snap.Action != action || action == "" {
		return ErrSession
	}
	v.used = true
	v.snap.OperationTime = time.Now().UTC()
	v.snap.Result = "submitting"
	// Capture the identity and position before the action, including files that
	// have not been created yet. A replacement is a new generation, never an offset.
	for _, path := range v.target.Files {
		if f, err := openRegular(path); err == nil {
			info, _ := f.Stat()
			if info != nil {
				chunk := make([]byte, min(int64(FragmentLimit), info.Size()))
				n, _ := f.ReadAt(chunk, max(0, info.Size()-FragmentLimit))
				v.snap.ExistingLog = tail(v.snap.ExistingLog + string(chunk[:n]))
			}
			if info != nil {
				v.files[path] = fileState{info: info, size: info.Size(), anchor: readAnchor(f, info.Size())}
			}
			f.Close()
		}
	}
	v.snap.Log = ""
	v.snap.Cursor = token()
	return s.save(v)
}
func (s *Store) Finish(id, result string, message plugin.Text) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.load(id)
	if err != nil {
		return err
	}
	v.snap.Result = result
	v.snap.Message = message
	return s.save(v)
}
func (s *Store) save(v *session) error {
	if v.snap.OperationTime.IsZero() {
		return nil
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(v.snap)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(s.root, v.snap.ID+".json"), data)
}
func (s *Store) load(id string) (*session, error) {
	if !validID(id) {
		return nil, ErrSession
	}
	if v := s.sessions[id]; v != nil {
		return v, nil
	}
	data, err := os.ReadFile(filepath.Join(s.root, id+".json"))
	if err != nil || len(data) > 4<<20 {
		return nil, ErrSession
	}
	var snap Snapshot
	if json.Unmarshal(data, &snap) != nil || snap.ID != id {
		return nil, ErrSession
	}
	v := &session{snap: snap, used: true, files: map[string]fileState{}, touched: time.Now()}
	s.sessions[id] = v
	return v, nil
}
func (s *Store) Read(ctx context.Context, id, cursor string, t Target) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.load(id)
	if err != nil {
		return Snapshot{}, err
	}
	if err = ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if v.target.Identity.PID > 1 && v.snap.Plugin != "" && !t.Identity.Started.IsZero() && v.target.Identity.PID == t.Identity.PID && !v.target.Identity.Equal(t.Identity) {
		t = Target{Identity: v.target.Identity, State: "unknown", Reason: "identity_changed"}
	}
	before := v.snap.Log
	reset := cursor == "" || cursor != v.snap.Cursor
	v.touched = time.Now()
	v.snap.CollectedAt = v.touched.UTC()
	v.snap.State = t.State
	if v.snap.State == "" {
		v.snap.State = "unknown"
	}
	v.snap.Identity = t.Identity
	v.snap.ExitCode = t.ExitCode
	v.snap.Signal = t.Signal
	v.snap.Runs = t.Runs
	sources := []Source{}
	body := before
	preview := ""
	previewRead := false
	previewFailed := false
	for _, path := range slices.Compact(slices.Sorted(slices.Values(t.Files))) {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%s", t.Identity.PID, t.Identity.Started.UTC(), path)))
		source := Source{ID: hex.EncodeToString(digest[:]), Name: filepath.Base(path)}
		f, openErr := openRegular(path)
		if openErr != nil {
			previewFailed = true
			switch {
			case errors.Is(openErr, os.ErrNotExist):
				source.State = "waiting"
			case errors.Is(openErr, os.ErrPermission):
				source.State = "permission"
			default:
				source.State = "not_regular"
			}
			sources = append(sources, source)
			continue
		}
		info, statErr := f.Stat()
		if statErr != nil {
			f.Close()
			source.State = "unreadable"
			sources = append(sources, source)
			continue
		}
		old, seen := v.files[path]
		changed := seen && len(old.anchor) > 0 && !bytes.Equal(old.anchor, readAnchor(f, old.size))
		offset := int64(0)
		if v.used && seen && os.SameFile(old.info, info) && info.Size() >= old.size && !changed {
			offset = old.size
		}
		if seen && (!os.SameFile(old.info, info) || info.Size() < old.size || changed) {
			reset = true
			body = ""
		}
		if !v.used {
			offset = max(0, info.Size()-FragmentLimit)
		}
		if info.Size()-offset > FragmentLimit {
			offset = info.Size() - FragmentLimit
			v.snap.Partial = true
			reset = true
			body = ""
		}
		chunk := make([]byte, min(int64(FragmentLimit), max(0, info.Size()-offset)))
		n, readErr := f.ReadAt(chunk, offset)
		anchor := readAnchor(f, offset+int64(n))
		f.Close()
		if readErr != nil && readErr != io.EOF {
			source.State = "unreadable"
		} else if info.Size() == 0 {
			source.State = "empty"
		} else {
			source.State = "available"
		}
		if !v.used {
			previewRead = true
			preview += string(chunk[:n])
		} else if n > 0 {
			body += string(chunk[:n])
		}
		v.files[path] = fileState{info: info, size: offset + int64(n), anchor: anchor}
		sources = append(sources, source)
	}
	if !v.used && previewRead && !previewFailed {
		body = preview
	}
	if t.Text != "" {
		body = tail(t.Text)
		sources = append(sources, Source{State: "events"})
	}
	if len(sources) == 0 {
		reason := t.Reason
		if reason == "" {
			reason = "unconfigured"
		}
		sources = append(sources, Source{State: reason})
	}
	v.snap.Sources = sources
	v.snap.Log = tail(body)
	v.snap.AfterOperation = v.used
	v.snap.Partial = v.snap.Partial || v.used
	out := v.snap
	if !reset && strings.HasPrefix(v.snap.Log, before) {
		out.Log = strings.TrimPrefix(v.snap.Log, before)
	} else {
		reset = true
	}
	v.snap.Cursor = token()
	out.Cursor = v.snap.Cursor
	out.Reset = reset
	if err = s.save(v); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}
func (s *Store) Recent(plugin string) ([]Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return []Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Snapshot{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		v, err := s.load(id)
		if err != nil {
			return nil, err
		}
		if v.snap.Plugin == plugin && !v.snap.OperationTime.IsZero() {
			out = append(out, v.snap)
		}
	}
	slices.SortFunc(out, func(a, b Snapshot) int { return b.OperationTime.Compare(a.OperationTime) })
	return out, nil
}

func readAnchor(f *os.File, offset int64) []byte {
	if offset <= 0 {
		return nil
	}
	b := make([]byte, min(int64(64), offset))
	n, _ := f.ReadAt(b, offset-int64(len(b)))
	return b[:n]
}
