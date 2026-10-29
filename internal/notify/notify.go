// Package notify watches the cards the user chose in notifications.json and
// tells the user's command and webhook when one of them changes.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/atomicfile"
	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
)

// ErrNoWayToTell refuses to watch a card while notifications.json has neither
// a command nor a webhook: the console never writes one.
var ErrNoWayToTell = errors.New("notifications.json has no command or webhook")

// Timeout bounds one run of the command, as it bounds a plugin's program, and
// one post to the webhook.
const Timeout = 30 * time.Second

// Reader reads a plugin's card as the overview does. ok is false when no
// loaded plugin of that ID has a card.
type Reader func(ctx context.Context, id string) (v card.View, interval time.Duration, ok bool)

// Config is notifications.json in hostdeck's folder.
type Config struct {
	Watch   []string `json:"watch"`
	Command []string `json:"command,omitempty"`
	Windows []string `json:"windows,omitempty"`
	Webhook *Webhook `json:"webhook,omitempty"`
}

// Webhook is where a change is posted. The URL and the header values may hold
// ${env:NAME}, filled in at each post, as in plugin.yaml.
type Webhook struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c Config) check() error {
	for _, args := range [][]string{c.Command, c.Windows} {
		if len(args) > 0 && args[0] == "@hostdeck" {
			return errors.New("@hostdeck runs plugins, not a notification command")
		}
	}
	if len(c.Watch) > 0 && len(c.Command) == 0 && c.Webhook == nil {
		return errors.New("watch needs a command or a webhook")
	}
	if h := c.Webhook; h != nil {
		if err := plugin.CheckEnv("webhook.url", h.URL); err != nil {
			return err
		}
		for name, v := range h.Headers {
			if err := plugin.CheckEnv("webhook.headers."+name, v); err != nil {
				return err
			}
		}
		// The whole address may come from ${env:NAME}, so it is checked filled in
		// and never repeated.
		url, _ := plugin.Expand(h.URL)
		if u, err := neturl.Parse(url); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("webhook.url must be an http or https address")
		}
	}
	return nil
}

type Watcher struct {
	dir  string
	read Reader
	now  func() time.Time

	writing sync.Mutex // one Watch at a time
	mu      sync.Mutex
	cfg     Config
	loadErr error // why the last Load refused the files
	cards   map[string]*watched
}

// The ways a change is told. Each keeps its own record of what it told, so
// one that failed is told again without the other.
const (
	byCommand = iota
	byWebhook
	ways
)

var wayNames = [ways]string{"command", "webhook"}

// ways lists the ways c tells a change.
func (c Config) ways() []int {
	var ws []int
	if len(c.Command) > 0 {
		ws = append(ws, byCommand)
	}
	if c.Webhook != nil {
		ws = append(ws, byWebhook)
	}
	return ws
}

// watched is what the watcher knows of one card.
type watched struct {
	seen   time.Time  // when the last reading it counted was read
	next   time.Time  // when to read the card again
	status string     // what the last reading said
	count  int        // how many readings in a row said it
	told   [ways]told // what each way last told
	gone   bool       // no loaded plugin had a card of this ID at the last check
}

// told is what one way of telling last told of a card.
type told struct {
	status string    // "" before it told anything
	at     time.Time // when the reading it told was read
	failed *Failure  // its last try, when it failed
}

// sent is data/notifications/sent.json: what each way last told of each
// card, so that a restart does not tell it again.
type sent struct {
	Version int                 `json:"version"`
	Cards   map[string]sentCard `json:"cards"`             // by the command
	Webhook map[string]sentCard `json:"webhook,omitempty"` // by the webhook
}

type sentCard struct {
	Status string    `json:"status"`
	Time   time.Time `json:"time,omitzero"` // when the reading told was read; none for a card fine when first seen
}

func (w *Watcher) sentFile() string {
	return filepath.Join(w.dir, "data", "notifications", "sent.json")
}

// New watches the cards of hostdeck's folder dir, read through read.
func New(dir string, read Reader, now func() time.Time) *Watcher {
	return &Watcher{dir: dir, read: read, now: now, cards: map[string]*watched{}}
}

// Load reads notifications.json, and what each way told before.
// Without notifications.json, nothing is watched. Files it refuses leave
// what was loaded in use, and the status says why.
func (w *Watcher) Load() error {
	err := w.load()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.loadErr = err
	return err
}

func (w *Watcher) load() error {
	cfg, err := w.config()
	if err != nil {
		return err
	}
	told := sent{Version: 1}
	data, err := os.ReadFile(w.sentFile())
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
	for way, cards := range [ways]map[string]sentCard{told.Cards, told.Webhook} {
		for id, c := range cards {
			w.card(id).told[way].status, w.card(id).told[way].at = c.Status, c.Time
		}
	}
	return nil
}

func (w *Watcher) configFile() string { return filepath.Join(w.dir, "notifications.json") }

// config reads notifications.json; one that is not there watches nothing.
func (w *Watcher) config() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(w.configFile())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return cfg, nil
	case err != nil:
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("notifications.json: %w", err)
	}
	if err := cfg.check(); err != nil {
		return cfg, fmt.Errorf("notifications.json: %w", err)
	}
	return cfg, nil
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
	told := sent{Version: 1, Cards: map[string]sentCard{}, Webhook: map[string]sentCard{}}
	for id, c := range w.cards {
		for way, cards := range [ways]map[string]sentCard{told.Cards, told.Webhook} {
			if t := c.told[way]; t.status != "" {
				cards[id] = sentCard{t.status, t.at}
			}
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
	w.mu.Lock()
	c := w.cards[id]
	c.gone = !ok
	if !ok || v.Pending {
		w.mu.Unlock()
		return
	}
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
	var tell []int
	var from [ways]string
	for _, way := range cfg.ways() {
		t := &c.told[way]
		switch {
		case c.count < 2 || status == t.status:
		case t.status == "" && status == "ok":
			// A card fine from the start has nothing to say.
			t.status = status
			w.remember()
		default:
			tell, from[way] = append(tell, way), t.status
		}
	}
	w.mu.Unlock()
	if len(tell) == 0 {
		return
	}
	var errs [ways]error
	var wg sync.WaitGroup
	for _, way := range tell {
		wg.Go(func() { errs[way] = w.tell(ctx, cfg, way, changeOf(id, v, from[way], status)) })
	}
	wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	toldAny := false
	for _, way := range tell {
		t := &c.told[way]
		if err := errs[way]; err != nil {
			log.Printf("notifications: %s changed to %s, and the %s failed: %v", id, status, wayNames[way], err)
			t.failed = &Failure{At: v.Updated, Detail: err.Error()}
			continue
		}
		t.status, t.at, t.failed, toldAny = status, v.Updated, nil, true
	}
	if toldAny {
		w.remember()
	}
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

// change is what the command and the webhook are told of one card.
type change struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NameZhCN string `json:"nameZhCN"`
	From     string `json:"from"`
	To       string `json:"to"`
	Value    string `json:"value"`
	Time     string `json:"time"`
}

func changeOf(id string, v card.View, from, to string) change {
	return change{
		ID:       id,
		Name:     inLanguage(v.Name, "en"),
		NameZhCN: inLanguage(v.Name, "zh-CN"),
		From:     from,
		To:       to,
		Value:    v.Value,
		Time:     v.Updated.UTC().Format(time.RFC3339),
	}
}

func (w *Watcher) tell(ctx context.Context, cfg Config, way int, ch change) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	if way == byWebhook {
		return post(ctx, cfg.Webhook, ch)
	}
	return w.run(ctx, cfg, ch)
}

func (w *Watcher) run(ctx context.Context, cfg Config, ch change) error {
	// exec keeps the last of duplicate keys, so these win over the console's own.
	env := append(os.Environ(),
		"HOSTDECK_ID="+ch.ID,
		"HOSTDECK_NAME="+ch.Name,
		"HOSTDECK_NAME_ZH_CN="+ch.NameZhCN,
		"HOSTDECK_FROM="+ch.From,
		"HOSTDECK_TO="+ch.To,
		"HOSTDECK_VALUE="+ch.Value,
		"HOSTDECK_TIME="+ch.Time,
	)
	_, err := run.External(ctx, run.Command{Dir: w.dir, Args: plugin.Exec{Command: cfg.Command, Windows: cfg.Windows}.Args(), Env: env})
	return err
}

// hookClient does not follow redirects: a webhook answers where it is.
var hookClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// post sends ch to the webhook as JSON; any answer but 2xx is a failure.
// Errors name the URL as notifications.json writes it and never hold a value
// that ${env:NAME} filled in.
func post(ctx context.Context, hook *Webhook, ch change) (err error) {
	url, secrets := plugin.Expand(hook.URL)
	defer func() {
		if err != nil {
			err = errors.New(card.Redact(fmt.Sprintf("POST %s: %v", hook.URL, err), secrets))
		}
	}()
	body, err := json.Marshal(ch)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range hook.Headers {
		value, s := plugin.Expand(value)
		secrets = append(secrets, s...)
		req.Header.Set(name, value)
	}
	resp, err := hookClient.Do(req)
	if err != nil {
		var ue *neturl.Error
		if errors.As(err, &ue) {
			return ue.Err // without the URL it would repeat, filled in
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		line, _, _ := strings.Cut(string(answer), "\n")
		if line = strings.TrimSpace(line); line != "" {
			return fmt.Errorf("%s: %s", resp.Status, line)
		}
		return errors.New(resp.Status)
	}
	return nil
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

// Status is what the watcher knows, for the console to show.
type Status struct {
	Error   string                `json:"error,omitempty"`   // why notifications.json or sent.json was not loaded
	Missing []string              `json:"missing,omitempty"` // watched IDs no loaded plugin has a card for
	Cards   map[string]CardStatus `json:"cards"`
}

// CardStatus is what the watcher knows of one watched card.
type CardStatus struct {
	Checked time.Time  `json:"checked,omitzero"`  // when the last reading it counted was read
	Command *WayStatus `json:"command,omitempty"` // when notifications.json has a command
	Webhook *WayStatus `json:"webhook,omitempty"` // when it has a webhook
}

// WayStatus is what one way of telling last told of a card.
type WayStatus struct {
	Told   string    `json:"told,omitempty"`   // what it last told
	ToldAt time.Time `json:"toldAt,omitzero"`  // when the reading it told was read
	Failed *Failure  `json:"failed,omitempty"` // its last try, when it failed
}

// Failure is a try at telling that did not succeed.
type Failure struct {
	At     time.Time `json:"at"` // when the reading it was to tell was read
	Detail string    `json:"detail"`
}

// Status says what the watcher knows of every watched card.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := Status{Cards: map[string]CardStatus{}}
	if w.loadErr != nil {
		st.Error = w.loadErr.Error()
	}
	for _, id := range w.cfg.Watch {
		c := w.card(id)
		if c.gone {
			st.Missing = append(st.Missing, id)
			continue
		}
		cs := CardStatus{Checked: c.seen}
		for _, way := range w.cfg.ways() {
			t := c.told[way]
			ws := &WayStatus{Told: t.status, ToldAt: t.at, Failed: t.failed}
			if way == byWebhook {
				cs.Webhook = ws
			} else {
				cs.Command = ws
			}
		}
		st.Cards[id] = cs
	}
	return st
}

// Watch puts id in the watch list of notifications.json, or with on false
// takes it out, keeping the commands, and loads the file. It reads the file
// again first, so a hand edit since the last load is kept.
func (w *Watcher) Watch(id string, on bool) error {
	w.writing.Lock()
	defer w.writing.Unlock()
	cfg, err := w.config()
	if err != nil {
		return err
	}
	if len(cfg.ways()) == 0 {
		return ErrNoWayToTell
	}
	cfg.Watch = slices.DeleteFunc(cfg.Watch, func(watched string) bool { return watched == id })
	if on {
		cfg.Watch = append(cfg.Watch, id)
	}
	if cfg.Watch == nil {
		cfg.Watch = []string{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(w.configFile(), append(data, '\n')); err != nil {
		return err
	}
	return w.Load()
}
