// Package card turns a plugin's card into what the overview shows.
package card

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/tidwall/gjson"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

// View is one overview card as the page receives it.
type View struct {
	ID    string      `json:"id"`
	Name  plugin.Text `json:"name"`
	Icon  string      `json:"icon,omitempty"`
	Value string      `json:"value,omitempty"`
	Label plugin.Text `json:"label,omitzero"`
	Error *Error      `json:"error,omitempty"`
}

// Error says why a card has no value. The page words it from Code; Detail is
// the underlying message, shown as is.
type Error struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// maxBody bounds how much of a response is read; health and status endpoints
// answer with far less.
const maxBody = 4 << 20

// Fetch asks the card's URL and picks the value out of its JSON. Any failure
// becomes the card's error rather than an error of the whole overview.
func Fetch(ctx context.Context, p plugin.Plugin) View {
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
