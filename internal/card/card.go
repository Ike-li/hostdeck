// Package card turns a plugin's card into what the overview shows.
package card

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
)

// View is one overview card as the page receives it.
type View struct {
	ID     string      `json:"id"`
	Name   plugin.Text `json:"name"`
	Icon   string      `json:"icon,omitempty"`
	Status string      `json:"status,omitempty"` // ok | warn | error
	Value  string      `json:"value,omitempty"`
	Label  plugin.Text `json:"label,omitzero"`
	Rows   []Row       `json:"rows,omitempty"`
	Error  *Error      `json:"error,omitempty"`
	// Pending says that a fetch is under way; what the card holds, if
	// anything, is from the fetch before.
	Pending bool `json:"pending,omitempty"`
	// Updated is when this view was read. Zero while the first read is still going.
	Updated time.Time `json:"updated,omitzero"`
}

type Row struct {
	Name   plugin.Text `json:"name"`
	Value  plugin.Text `json:"value"`
	Status string      `json:"status,omitempty"`
}

// Error says why a card has no value. The page words it from Code; Detail is
// the part that sentence includes. File, when set, is the manifest path and
// is shown apart from the sentence. A command's own error is in Text.
type Error struct {
	Code   string      `json:"code"`
	Detail string      `json:"detail,omitempty"`
	File   string      `json:"file,omitempty"`
	Text   plugin.Text `json:"text,omitzero"`
}

// maxBody bounds how much of a response is read; health and status endpoints
// answer with far less.
const maxBody = 4 << 20

// Fetch asks the card's URL and picks the value out of its JSON. Any failure
// becomes the card's error rather than an error of the whole overview.
func Fetch(ctx context.Context, p plugin.Plugin) View {
	if p.Card.Exec != nil {
		return runCard(ctx, p)
	}
	c := p.Card
	v := View{ID: p.ID, Name: p.Name, Icon: p.Icon, Label: c.Label}
	body, err := get(ctx, c.HTTP)
	if err != nil {
		v.Error = &Error{Code: "fetch_failed", Detail: err.Error()}
		return v
	}
	value, err := at(body, c.HTTP, c.Value)
	if err != nil {
		v.Error = &Error{Code: "fetch_failed", Detail: err.Error()}
		return v
	}
	status, err := statusOf(value, c.Warn, c.Error)
	if err != nil {
		v.Error = &Error{Code: "threshold", Detail: fmt.Sprintf("%s: %v", c.Value, err)}
		return v
	}
	v.Value, v.Status = value, status
	for _, r := range c.Rows {
		value, err := at(body, c.HTTP, r.Value)
		if err != nil {
			return View{ID: p.ID, Name: p.Name, Icon: p.Icon, Error: &Error{Code: "fetch_failed", Detail: err.Error()}}
		}
		status, err := statusOf(value, r.Warn, r.Error)
		if err != nil {
			return View{ID: p.ID, Name: p.Name, Icon: p.Icon, Error: &Error{Code: "threshold", Detail: fmt.Sprintf("%s: %v", r.Value, err)}}
		}
		v.Rows = append(v.Rows, Row{Name: r.Name, Value: plugin.Text{All: value}, Status: status})
	}
	return v
}

var httpClient = &http.Client{
	Transport: &http.Transport{Proxy: nil},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// get asks h and returns its JSON. Errors name the URL as the manifest writes
// it and never hold a value that ${env:NAME} filled in.
func get(ctx context.Context, h *plugin.HTTP) (body []byte, err error) {
	url, secrets := plugin.Expand(h.URL)
	defer func() {
		if err != nil {
			msg := err.Error()
			if url != h.URL && url != "" {
				quoted := strconv.Quote(url)
				msg = strings.ReplaceAll(msg, url, h.URL)
				msg = strings.ReplaceAll(msg, quoted[1:len(quoted)-1], h.URL)
			}
			err = errors.New(redact(fmt.Sprintf("GET %s: %s", h.URL, msg), secrets))
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range h.Headers {
		value, s := plugin.Expand(value)
		secrets = append(secrets, s...)
		req.Header.Set(name, value)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		var ue *neturl.Error
		if errors.As(err, &ue) {
			return nil, ue.Err // without the URL it would repeat, filled in
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, errors.New(resp.Status)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("the response exceeded %d bytes", maxBody)
	}
	if !gjson.ValidBytes(body) {
		return nil, errors.New("the response is not JSON")
	}
	return body, nil
}

// redact hides each secret in msg, also as Go quotes it and as URLs escape
// it, the forms in which error messages repeat a URL.
func redact(msg string, secrets []string) string {
	for _, s := range secrets {
		if len(s) < 3 {
			continue
		}
		quoted := strconv.Quote(s)
		for _, form := range []string{s, quoted[1 : len(quoted)-1], neturl.QueryEscape(s), neturl.PathEscape(s)} {
			msg = strings.ReplaceAll(msg, form, "***")
		}
	}
	return msg
}

func at(body []byte, h *plugin.HTTP, path string) (string, error) {
	r := gjson.GetBytes(body, path)
	if !r.Exists() {
		return "", fmt.Errorf("GET %s: the response has nothing at %q", h.URL, path)
	}
	return r.String(), nil
}

// statusOf is "error" or "warn" when value meets that threshold, error first,
// "ok" when it meets neither, and "" when there are none.
func statusOf(value string, warn, bad *plugin.Threshold) (string, error) {
	if warn == nil && bad == nil {
		return "", nil
	}
	for _, c := range []struct {
		t      *plugin.Threshold
		status string
	}{{bad, "error"}, {warn, "warn"}} {
		hit, err := meets(value, c.t)
		if err != nil {
			return "", err
		}
		if hit {
			return c.status, nil
		}
	}
	return "ok", nil
}

func meets(value string, t *plugin.Threshold) (bool, error) {
	switch {
	case t == nil:
		return false, nil
	case t.Equals != nil:
		return value == string(*t.Equals), nil
	case t.Not != nil:
		return value != string(*t.Not), nil
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false, fmt.Errorf("%q is not a number, so above and below cannot apply", value)
	}
	if t.Above != nil {
		return n > *t.Above, nil
	}
	return n < *t.Below, nil
}

// printed is what an exec card's command prints.
type printed struct {
	Status string        `json:"status"`
	Value  plugin.Scalar `json:"value"`
	Label  plugin.Text   `json:"label"`
	Rows   []struct {
		Name   plugin.Text  `json:"name"`
		Value  printedValue `json:"value"`
		Status string       `json:"status"`
	} `json:"rows"`
	Error plugin.Text `json:"error"`
}

// printedValue is a row value a card program may print: a string, a number,
// or localized text. A number is kept as its digits. Strings and numbers
// leave the API as strings; only localized text leaves as an object.
type printedValue struct{ plugin.Text }

func (v *printedValue) UnmarshalJSON(data []byte) error {
	v.Text = plugin.Text{}
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '{' {
		return v.Text.UnmarshalJSON(data)
	}
	var scalar plugin.Scalar
	if err := scalar.UnmarshalJSON(data); err != nil {
		return err
	}
	v.All = string(scalar)
	return nil
}

var statuses = []string{"", "ok", "warn", "error"}

// ValidStatus says whether a plugin may print status.
func ValidStatus(status string) bool { return slices.Contains(statuses, status) }

// CommandError says why running a plugin's command under ctx failed.
func CommandError(ctx context.Context, err error) *Error {
	var exit *run.ExitError
	var output *run.OutputError
	switch {
	case ctx.Err() != nil:
		return &Error{Code: "command_timeout"}
	case errors.As(err, &exit):
		return &Error{Code: "command_exit", Detail: exit.Error()}
	case errors.As(err, &output):
		return &Error{Code: "command_output", Detail: output.Error()}
	}
	return &Error{Code: "command_start", Detail: err.Error()}
}

// runCard runs the card's command in the plugin's folder and shows what it
// prints.
func runCard(ctx context.Context, p plugin.Plugin) View {
	v := View{ID: p.ID, Name: p.Name, Icon: p.Icon}
	var pr printed
	if err := run.JSON(ctx, p.Dir, p.Card.Exec.Args(), &pr); err != nil {
		v.Error = CommandError(ctx, err)
		return v
	}
	if !pr.Error.IsZero() {
		v.Error = &Error{Code: "reported", Text: pr.Error}
		return v
	}
	if s, ok := unknownStatus(pr); ok {
		v.Error = &Error{Code: "command_output", Detail: fmt.Sprintf("status %q is not ok, warn or error", s)}
		return v
	}
	v.Status, v.Value, v.Label = pr.Status, string(pr.Value), pr.Label
	for _, r := range pr.Rows {
		v.Rows = append(v.Rows, Row{Name: r.Name, Value: r.Value.Text, Status: r.Status})
	}
	return v
}

func unknownStatus(pr printed) (string, bool) {
	if !ValidStatus(pr.Status) {
		return pr.Status, true
	}
	for _, r := range pr.Rows {
		if !ValidStatus(r.Status) {
			return r.Status, true
		}
	}
	return "", false
}

// FetchTimeout bounds one fetch of a card. Whoever asks may wait less: a card
// that takes longer comes back pending and arrives on a later request. Windows
// PowerShell can take many seconds to start for the first time.
const FetchTimeout = 30 * time.Second

// Cache keeps each card's last view for its interval, so that opening the
// overview again does not ask every service again.
type Cache struct {
	now     func() time.Time
	mu      sync.Mutex
	epoch   uint64 // bumped by Drop; in-flight fetches capture it
	entries map[string]*entry
}

type entry struct {
	view    View
	until   time.Time     // the view is kept until then; a failed fetch is not kept
	running chan struct{} // closed when the fetch under way ends
}

func NewCache(now func() time.Time) *Cache {
	return &Cache{now: now, entries: map[string]*entry{}}
}

// Drop forgets every kept view, so the next Get asks again before the interval.
// A fetch that is already running still finishes for its waiters, but must not
// extend until after this Drop.
func (c *Cache) Drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	for _, e := range c.entries {
		e.until = time.Time{}
	}
}

// Get returns the plugin's card: the kept one while its interval lasts, else
// a new one. If the fetch outlasts ctx, the card comes back pending, with its
// last value if it has one, and the fetch goes on for the next request.
func (c *Cache) Get(ctx context.Context, p plugin.Plugin) View {
	c.mu.Lock()
	e := c.entries[p.ID]
	if e == nil {
		e = &entry{}
		c.entries[p.ID] = e
	}
	if e.running == nil && c.now().Before(e.until) {
		defer c.mu.Unlock()
		return e.view
	}
	if e.running == nil {
		e.running = make(chan struct{})
		go c.fetch(p, e, c.epoch)
	}
	running := e.running
	c.mu.Unlock()

	select {
	case <-running:
	case <-ctx.Done():
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v := e.view
	if e.running != nil {
		if v.ID == "" {
			v = View{ID: p.ID, Name: p.Name, Icon: p.Icon}
		}
		v.Pending = true
	}
	return v
}

func (c *Cache) fetch(p plugin.Plugin, e *entry, epoch uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), FetchTimeout)
	defer cancel()
	v := Fetch(ctx, p)
	v.Updated = c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	e.view, e.until = v, time.Time{}
	if v.Error == nil && epoch == c.epoch {
		e.until = c.now().Add(time.Duration(p.Card.Interval))
	}
	close(e.running)
	e.running = nil
}
