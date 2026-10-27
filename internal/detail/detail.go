// Package detail reads a plugin's detail page and runs the actions it offers.
package detail

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/run"
)

var actMu sync.Mutex

// ReadTimeout bounds the detail command. Windows PowerShell can take many
// seconds to start for the first time.
const ReadTimeout = 30 * time.Second

// ActTimeout bounds an action, which runs to the end even when the page that
// asked for it goes away. Tests shorten it.
var ActTimeout = 30 * time.Second

// View is a detail page as the page receives it.
type View struct {
	Items        []Item      `json:"items,omitempty"`
	Presentation string      `json:"presentation,omitempty"`
	Groups       []Group     `json:"groups,omitempty"`
	Note         plugin.Text `json:"note,omitempty"`
	Error        *card.Error `json:"error,omitempty"`
}

type Group struct {
	ID    string      `json:"id"`
	Label plugin.Text `json:"label"`
}

type Item struct {
	Group        string      `json:"group,omitempty"`
	ID           string      `json:"id"`
	Name         plugin.Text `json:"name"`
	Status       string      `json:"status,omitempty"`
	Value        Value       `json:"value,omitzero"`
	Label        plugin.Text `json:"label,omitempty"`
	Fields       []Field     `json:"fields,omitempty"`
	Log          string      `json:"log,omitempty"`
	LogAvailable bool        `json:"logAvailable,omitempty"`
	Actions      []Action    `json:"actions,omitempty"`
}

type Field struct {
	Name  plugin.Text `json:"name"`
	Value Value       `json:"value"`
}

type Action struct {
	ID    string      `json:"id"`
	Label plugin.Text `json:"label"`
}

// Read runs the plugin's detail command and returns the items it prints.
func Read(ctx context.Context, p plugin.Plugin) View {
	return read(ctx, p, "")
}

// ReadItem runs the same command for one item. The id is data passed in the
// environment, not a path, and a newline or a very long id is refused before
// the command starts.
func ReadItem(ctx context.Context, p plugin.Plugin, item string) View {
	if item == "" || len(item) > 256 || strings.ContainsAny(item, "\r\n") {
		return View{Error: &card.Error{Code: "bad_request", Detail: "item"}}
	}
	return read(ctx, p, item)
}

func read(ctx context.Context, p plugin.Plugin, item string) View {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	var printed struct {
		Items        []Item      `json:"items"`
		Presentation string      `json:"presentation"`
		Groups       []Group     `json:"groups"`
		Note         plugin.Text `json:"note"`
		Error        plugin.Text `json:"error"`
	}
	var err error
	if item == "" {
		err = run.JSON(ctx, p.Dir, p.Detail.Exec.Args(), &printed)
	} else {
		err = run.JSONEnv(ctx, p.Dir, p.Detail.Exec.Args(), detailEnv(item), &printed)
	}
	if err != nil {
		return View{Error: card.CommandError(ctx, err)}
	}
	if !printed.Error.IsZero() {
		return View{Error: &card.Error{Code: "reported", Text: printed.Error}}
	}
	if err := check(printed.Items); err != nil {
		return View{Error: &card.Error{Code: "command_output", Detail: err.Error()}}
	}
	v := View{Items: printed.Items, Presentation: printed.Presentation, Groups: printed.Groups, Note: printed.Note}
	if err := v.checkGroups(); err != nil {
		return View{Error: &card.Error{Code: "command_output", Detail: err.Error()}}
	}
	return v
}

// detailEnv copies the console environment and replaces any HOSTDECK_DETAIL_ITEM.
// Unix getenv returns the first value, so appending a second copy would be ignored.
func detailEnv(item string) []string {
	const key = "HOSTDECK_DETAIL_ITEM="
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, key) {
			env = append(env, e)
		}
	}
	return append(env, key+item)
}

func (v View) checkGroups() error {
	if v.Presentation != "" && v.Presentation != "compact" {
		return fmt.Errorf("unknown detail presentation %q", v.Presentation)
	}
	ids := make(map[string]bool)
	for _, g := range v.Groups {
		if strings.TrimSpace(g.ID) == "" || g.ID == "all" || !named(g.Label) || ids[g.ID] {
			return fmt.Errorf("invalid or repeated detail group %q", g.ID)
		}
		ids[g.ID] = true
	}
	for _, it := range v.Items {
		if (len(v.Groups) > 0 || it.Group != "") && !ids[it.Group] {
			return fmt.Errorf("item %q has unknown group %q", it.ID, it.Group)
		}
	}
	return nil
}

func check(items []Item) error {
	var ids []string
	for i, it := range items {
		switch {
		case it.ID == "":
			return fmt.Errorf("item %d has no id", i+1)
		case slices.Contains(ids, it.ID):
			return fmt.Errorf("two items have the id %q", it.ID)
		case !named(it.Name):
			return fmt.Errorf("item %q has no name", it.ID)
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

// named requires text in the heading, either shared or in at least one language.
func named(t plugin.Text) bool {
	if strings.TrimSpace(t.All) != "" {
		return true
	}
	for _, s := range t.ByLang {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

func (v View) offers(item, action string) bool {
	i := slices.IndexFunc(v.Items, func(it Item) bool { return it.ID == item })
	return i >= 0 && slices.ContainsFunc(v.Items[i].Actions, func(a Action) bool { return a.ID == action })
}

// Act runs action on item, if the detail, read again now, still offers that
// action on that item. The page asking for it may be old. What the action
// prints, if anything, is a message or an error of its own.
func Act(ctx context.Context, p plugin.Plugin, item, action string) (plugin.Text, *card.Error) {
	actMu.Lock()
	defer actMu.Unlock()
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
