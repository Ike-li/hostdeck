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
	"github.com/Ike-li/hostdeck/internal/plugin"
)

func load(t *testing.T, id string) plugin.Plugin {
	t.Helper()
	plugins, err := plugin.Load(".")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(plugins, func(p plugin.Plugin) bool { return p.ID == id })
	if i < 0 {
		t.Fatalf("no example %s", id)
	}
	return plugins[i]
}

func TestTheDiskExampleShowsTheFreeSpaceOfThisMachine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
