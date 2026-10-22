// Package detail reads a plugin's detail page and runs the actions it offers.
package detail

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
)

// ReadTimeout bounds the detail command. Windows PowerShell can take many
// seconds to start for the first time.
const ReadTimeout = 30 * time.Second

// ActTimeout bounds an action, which runs to the end even when the page that
// asked for it goes away. Tests shorten it.
var ActTimeout = 30 * time.Second

// View is a detail page as the page receives it.
type View struct {
	Items []Item      `json:"items,omitempty"`
	Error *card.Error `json:"error,omitempty"`
}

type Item struct {
	ID      string        `json:"id"`
	Name    plugin.Text   `json:"name"`
	Status  string        `json:"status,omitempty"`
	Value   plugin.Scalar `json:"value,omitempty"`
	Fields  []Field       `json:"fields,omitempty"`
	Log     string        `json:"log,omitempty"`
	Actions []Action      `json:"actions,omitempty"`
}

type Field struct {
	Name  plugin.Text   `json:"name"`
	Value plugin.Scalar `json:"value"`
}

type Action struct {
	ID    string      `json:"id"`
	Label plugin.Text `json:"label"`
}

// Read runs the plugin's detail command and returns the items it prints.
func Read(ctx context.Context, p plugin.Plugin) View {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	var printed struct {
		Items []Item      `json:"items"`
		Error plugin.Text `json:"error"`
	}
	if err := run.JSON(ctx, p.Dir, p.Detail.Exec.Args(), &printed); err != nil {
		return View{Error: card.CommandError(ctx, err)}
	}
	if !printed.Error.IsZero() {
		return View{Error: &card.Error{Code: "reported", Text: printed.Error}}
	}
	if err := check(printed.Items); err != nil {
		return View{Error: &card.Error{Code: "command_output", Detail: err.Error()}}
	}
	return View{Items: printed.Items}
}

func check(items []Item) error {
	var ids []string
	for i, it := range items {
		switch {
		case it.ID == "":
			return fmt.Errorf("item %d has no id", i+1)
		case slices.Contains(ids, it.ID):
			return fmt.Errorf("two items have the id %q", it.ID)
		case !card.ValidStatus(it.Status):
			return fmt.Errorf("item %q: status %q is not ok, warn or error", it.ID, it.Status)
		}
		ids = append(ids, it.ID)
		for _, a := range it.Actions {
			if a.ID == "" {
				return fmt.Errorf("item %q has an action without an id", it.ID)
			}
		}
	}
	return nil
}

func (v View) offers(item, action string) bool {
	i := slices.IndexFunc(v.Items, func(it Item) bool { return it.ID == item })
	return i >= 0 && slices.ContainsFunc(v.Items[i].Actions, func(a Action) bool { return a.ID == action })
}

// Act runs action on item, if the detail, read again now, still offers that
// action on that item. The page asking for it may be old. What the action
// prints, if anything, is a message or an error of its own.
func Act(ctx context.Context, p plugin.Plugin, item, action string) (plugin.Text, *card.Error) {
	v := Read(ctx, p)
	switch {
	case v.Error != nil:
		return plugin.Text{}, v.Error
	case !v.offers(item, action):
		return plugin.Text{}, &card.Error{Code: "not_offered", Detail: fmt.Sprintf("%s on %s", action, item)}
	}
	// The page may be reloaded or closed while the action runs, and an action
	// such as a restart, stopped halfway, could leave a service down.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ActTimeout)
	defer cancel()
	out, err := run.Output(ctx, p.Dir, append(slices.Clone(p.Actions.Exec.Args()), action, item))
	if err != nil {
		return plugin.Text{}, card.CommandError(ctx, err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return plugin.Text{}, nil
	}
	var printed struct {
		Message plugin.Text `json:"message"`
		Error   plugin.Text `json:"error"`
	}
	if err := run.Decode(out, &printed); err != nil {
		return plugin.Text{}, card.CommandError(ctx, err)
	}
	if !printed.Error.IsZero() {
		return plugin.Text{}, &card.Error{Code: "reported", Text: printed.Error}
	}
	return printed.Message, nil
}
