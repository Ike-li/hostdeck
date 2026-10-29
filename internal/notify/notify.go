// Package notify watches the cards the user chose in notifications.json and
// runs the user's command when one of them changes.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/atomicfile"
	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
)

// Timeout bounds one run of the command, as it bounds a plugin's program.
const Timeout = 30 * time.Second

// Reader reads a plugin's card as the overview does. ok is false when no
// loaded plugin of that ID has a card.
type Reader func(ctx context.Context, id string) (v card.View, interval time.Duration, ok bool)

// Config is notifications.json in hostdeck's folder.
type Config struct {
	Watch   []string `json:"watch"`
	Command []string `json:"command"`
	Windows []string `json:"windows"`
}

func (c Config) check() error {
	for _, args := range [][]string{c.Command, c.Windows} {
		if len(args) > 0 && args[0] == "@hostdeck" {
			return errors.New("@hostdeck runs plugins, not a notification command")
		}
	}
	if len(c.Watch) > 0 && len(c.Command) == 0 {
		return errors.New("watch needs a command to run")
	}
	return nil
}

type Watcher struct {
	dir  string
	read Reader
	now  func() time.Time

	mu    sync.Mutex
	cfg   Config
	cards map[string]*watched
}

// watched is what the watcher knows of one card.
type watched struct {
	seen   time.Time // when the last reading it counted was read
	next   time.Time // when to read the card again
	status string    // what the last reading said
	count  int       // how many readings in a row said it
	sent   string    // what the command was last told; "" before it was told anything
}

// sent is data/notifications/sent.json: what the command was last told of
// each card, so that a restart does not tell it again.
type sent struct {
	Version int                 `json:"version"`
	Cards   map[string]sentCard `json:"cards"`
}

type sentCard struct {
	Status string `json:"status"`
}

func (w *Watcher) sentFile() string {
	return filepath.Join(w.dir, "data", "notifications", "sent.json")
}

// New watches the cards of hostdeck's folder dir, read through read.
func New(dir string, read Reader, now func() time.Time) *Watcher {
	return &Watcher{dir: dir, read: read, now: now, cards: map[string]*watched{}}
}

// Load reads notifications.json, and what the command was told before.
// Without notifications.json, nothing is watched.
func (w *Watcher) Load() error {
	var cfg Config
	data, err := os.ReadFile(filepath.Join(w.dir, "notifications.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return fmt.Errorf("notifications.json: %w", err)
		}
		if err := cfg.check(); err != nil {
			return fmt.Errorf("notifications.json: %w", err)
		}
	}
	told := sent{Version: 1}
	data, err = os.ReadFile(w.sentFile())
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, &told); err != nil {
			return fmt.Errorf("%s: %w", w.sentFile(), err)
		}
		if told.Version != 1 {
			return fmt.Errorf("%s: version %d, not 1", w.sentFile(), told.Version)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cfg = cfg
	for id, c := range told.Cards {
		w.card(id).sent = c.Status
	}
	return nil
}

func (w *Watcher) card(id string) *watched {
	c := w.cards[id]
	if c == nil {
		c = &watched{}
		w.cards[id] = c
	}
	return c
}

// remember writes what the command was last told of every card. A record it
// cannot write is logged: after a restart, that change would be told again.
func (w *Watcher) remember() {
	if err := w.write(); err != nil {
		log.Printf("notifications: %v", err)
	}
}

func (w *Watcher) write() error {
	told := sent{Version: 1, Cards: map[string]sentCard{}}
	for id, c := range w.cards {
		if c.sent != "" {
			told.Cards[id] = sentCard{c.sent}
		}
	}
	data, err := json.MarshalIndent(told, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(w.sentFile()), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(w.sentFile(), data)
}

// Run checks once a second until ctx ends, which also ends a command under way.
func (w *Watcher) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Check reads every watched card that is due, and runs the command for each
// change two readings in a row have seen.
func (w *Watcher) Check(ctx context.Context) {
	w.mu.Lock()
	cfg, now := w.cfg, w.now()
	var due []string
	for _, id := range cfg.Watch {
		if !now.Before(w.card(id).next) {
			due = append(due, id)
		}
	}
	w.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range due {
		wg.Go(func() { w.check(ctx, cfg, id) })
	}
	wg.Wait()
}

func (w *Watcher) check(ctx context.Context, cfg Config, id string) {
	reading, cancel := context.WithTimeout(ctx, card.FetchTimeout+time.Second)
	v, interval, ok := w.read(reading, id)
	cancel()
	if !ok || v.Pending {
		return
	}
	w.mu.Lock()
	c := w.cards[id]
	c.next = v.Updated.Add(interval)
	// The cache may still hand out a reading it was kept past its interval, or
	// one the overview asked for; a reading counts once.
	if !v.Updated.After(c.seen) {
		w.mu.Unlock()
		return
	}
	c.seen = v.Updated
	status := statusOf(v)
	if status == c.status {
		c.count++
	} else {
		c.status, c.count = status, 1
	}
	from := c.sent
	tell := c.count >= 2 && status != from
	if tell && from == "" && status == "ok" {
		// A card fine from the start has nothing to say.
		c.sent, tell = status, false
		w.remember()
	}
	w.mu.Unlock()
	if !tell {
		return
	}
	if err := w.tell(ctx, cfg, id, v, from, status); err != nil {
		log.Printf("notifications: %s changed to %s, and the command failed: %v", id, status, err)
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c.sent = status
	w.remember()
}

// statusOf is the card's colour on the overview, with a card that could not
// be read apart from one that reads red.
func statusOf(v card.View) string {
	switch {
	case v.Error != nil:
		return "unreadable"
	case v.Status == "warn", v.Status == "error":
		return v.Status
	}
	return "ok"
}

func (w *Watcher) tell(ctx context.Context, cfg Config, id string, v card.View, from, to string) error {
	vars := map[string]string{
		"HOSTDECK_ID":         id,
		"HOSTDECK_NAME":       inLanguage(v.Name, "en"),
		"HOSTDECK_NAME_ZH_CN": inLanguage(v.Name, "zh-CN"),
		"HOSTDECK_FROM":       from,
		"HOSTDECK_TO":         to,
		"HOSTDECK_VALUE":      v.Value,
		"HOSTDECK_TIME":       v.Updated.UTC().Format(time.RFC3339),
	}
	// exec keeps the last of duplicate keys, so these win over the console's own.
	env := os.Environ()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	_, err := run.External(ctx, run.Command{Dir: w.dir, Args: plugin.Exec{Command: cfg.Command, Windows: cfg.Windows}.Args(), Env: env})
	return err
}

// inLanguage picks a name as the page does: the language, else English,
// else whichever there is.
func inLanguage(t plugin.Text, lang string) string {
	if t.ByLang == nil {
		return t.All
	}
	for _, l := range []string{lang, "en", "zh-CN"} {
		if s, ok := t.ByLang[l]; ok {
			return s
		}
	}
	return ""
}
