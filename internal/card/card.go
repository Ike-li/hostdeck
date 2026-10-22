// Package card turns a plugin's card into what the overview shows.
package card

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"slices"

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
}

type Row struct {
	Name   plugin.Text `json:"name"`
	Value  string      `json:"value"`
	Status string      `json:"status,omitempty"`
}

// Error says why a card has no value. The page words it from Code; Detail is
// the underlying message, shown as is. A command's own error is in Text.
type Error struct {
	Code   string      `json:"code"`
	Detail string      `json:"detail,omitempty"`
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
	v := View{ID: p.ID, Name: p.Name, Icon: p.Icon, Label: p.Card.Label}
	value, err := fetchValue(ctx, p.Card)
	if err != nil {
		v.Error = &Error{Code: "fetch_failed", Detail: err.Error()}
		return v
	}
	v.Value = value
	return v
}

func fetchValue(ctx context.Context, c *plugin.Card) (string, error) {
	url := c.HTTP.URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	if !gjson.ValidBytes(body) {
		return "", fmt.Errorf("GET %s: the response is not JSON", url)
	}
	r := gjson.GetBytes(body, c.Value)
	if !r.Exists() {
		return "", fmt.Errorf("GET %s: the response has nothing at %q", url, c.Value)
	}
	return r.String(), nil
}

// printed is what an exec card's command prints.
type printed struct {
	Status string      `json:"status"`
	Value  scalar      `json:"value"`
	Label  plugin.Text `json:"label"`
	Rows   []struct {
		Name   plugin.Text `json:"name"`
		Value  scalar      `json:"value"`
		Status string      `json:"status"`
	} `json:"rows"`
	Error plugin.Text `json:"error"`
}

// scalar is a value printed as a string or as a number.
type scalar string

func (s *scalar) UnmarshalJSON(data []byte) error {
	var str string
	if json.Unmarshal(data, &str) == nil {
		*s = scalar(str)
		return nil
	}
	*s = scalar(data)
	return nil
}

var statuses = []string{"", "ok", "warn", "error"}

// bom is what Windows PowerShell may put before its output.
var bom = []byte("\ufeff")

// runCard runs the card's command in the plugin's folder and shows what it
// prints. The command for Windows, when the manifest has one, runs there.
func runCard(ctx context.Context, p plugin.Plugin) View {
	v := View{ID: p.ID, Name: p.Name, Icon: p.Icon}
	command := p.Card.Exec.Command
	if runtime.GOOS == "windows" && len(p.Card.Exec.Windows) > 0 {
		command = p.Card.Exec.Windows
	}
	out, err := run.Output(ctx, p.Dir, command)
	var exit *run.ExitError
	switch {
	case ctx.Err() != nil:
		v.Error = &Error{Code: "command_timeout"}
	case errors.As(err, &exit):
		v.Error = &Error{Code: "command_exit", Detail: exit.Error()}
	case err != nil:
		v.Error = &Error{Code: "command_start", Detail: err.Error()}
	}
	if err != nil {
		return v
	}
	var pr printed
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(out, bom)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pr); err != nil {
		v.Error = &Error{Code: "command_output", Detail: err.Error()}
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
		v.Rows = append(v.Rows, Row{Name: r.Name, Value: string(r.Value), Status: r.Status})
	}
	return v
}

func unknownStatus(pr printed) (string, bool) {
	if !slices.Contains(statuses, pr.Status) {
		return pr.Status, true
	}
	for _, r := range pr.Rows {
		if !slices.Contains(statuses, r.Status) {
			return r.Status, true
		}
	}
	return "", false
}
