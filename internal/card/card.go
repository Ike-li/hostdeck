// Package card turns a plugin's card into what the overview shows.
package card

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	Status string        `json:"status"`
	Value  plugin.Scalar `json:"value"`
	Label  plugin.Text   `json:"label"`
	Rows   []struct {
		Name   plugin.Text   `json:"name"`
		Value  plugin.Scalar `json:"value"`
		Status string        `json:"status"`
	} `json:"rows"`
	Error plugin.Text `json:"error"`
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
		v.Rows = append(v.Rows, Row{Name: r.Name, Value: string(r.Value), Status: r.Status})
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
