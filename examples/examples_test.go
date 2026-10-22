// Package examples holds plugins that work as they are; these tests run them
// on the machine at hand.
package examples

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

func load(t *testing.T, id string) plugin.Plugin {
	t.Helper()
	plugins, broken, err := plugin.Load(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range broken {
		t.Errorf("example %s does not load: %v", b.ID, b.Err)
	}
	i := slices.IndexFunc(plugins, func(p plugin.Plugin) bool { return p.ID == id })
	if i < 0 {
		t.Fatalf("no example %s", id)
	}
	return plugins[i]
}

func TestTheDiskExampleShowsTheFreeSpaceOfThisMachine(t *testing.T) {
	// Generous: Windows PowerShell took 6.4 s to start for the first time on
	// an idle CI runner, and longer while other test packages ran.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	v := card.Fetch(ctx, load(t, "disk"))

	if v.Error != nil {
		t.Fatalf("error card: %+v", v.Error)
	}
	if !strings.HasSuffix(v.Value, " GB") || !slices.Contains([]string{"ok", "warn", "error"}, v.Status) {
		t.Errorf("value %q, status %q; want a size in GB and a status", v.Value, v.Status)
	}
	// Chinese text survives the trip through the command's output, which on
	// Windows depends on the script choosing UTF-8.
	if zh := v.Label.ByLang["zh-CN"]; !strings.HasPrefix(zh, "可用") {
		t.Errorf("Chinese label %q, want it to start with 可用", zh)
	}
	if len(v.Rows) != 1 || v.Rows[0].Name.ByLang["zh-CN"] != "已用" || !strings.HasSuffix(v.Rows[0].Value, "%") {
		t.Errorf("rows %+v, want one row saying how much is used", v.Rows)
	}
}

// The open action itself is not run here: it would open a window on the
// desktop of whoever runs the tests.
func TestTheDiskExampleListsTheHomeFoldersDiskWithAnOpenAction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	v := detail.Read(ctx, load(t, "disk"))

	if v.Error != nil {
		t.Fatalf("error: %+v", v.Error)
	}
	i := slices.IndexFunc(v.Items, func(it detail.Item) bool { return strings.Contains(it.Name.ByLang["zh-CN"], "主目录") })
	if i < 0 {
		t.Fatalf("items %+v, want the home folder's disk, named in Chinese", v.Items)
	}
	home := v.Items[i]
	if !slices.ContainsFunc(home.Fields, func(f detail.Field) bool {
		return f.Name.ByLang["zh-CN"] == "已用" && strings.HasSuffix(string(f.Value), "%")
	}) {
		t.Errorf("fields %+v, want how much is used", home.Fields)
	}
	if len(home.Actions) != 1 || home.Actions[0].ID != "open" {
		t.Errorf("actions %+v, want open", home.Actions)
	}
}
