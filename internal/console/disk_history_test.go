package console

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/tidwall/gjson"
)

func diskHistoryConsole(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir, root := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"roots": []string{root}})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	var scans []any
	for i, at := range []string{"2026-10-24T12:00:00Z", "2026-10-25T12:00:00Z", "2026-10-26T00:00:00Z", "2026-10-26T12:00:00Z"} {
		scans = append(scans, map[string]any{"time": at, "allocated": true, "sizes": map[string]any{
			".": map[string]any{"bytes": 1000 + i*100}, "Library": map[string]any{"bytes": 500 + i*50},
			filepath.Join("Library", "Containers"): map[string]any{"bytes": 200 + i*20}, "code": map[string]any{"bytes": 300 + i*10},
		}})
	}
	data, _ := json.Marshal(map[string]any{"version": 1, "tracking": false, "scans": map[string]any{root: scans}})
	if err := os.WriteFile(filepath.Join(dir, "data", "history.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p := plugin.Plugin{ID: "renamed-disk", Name: plugin.Text{All: "My disk"}, Dir: dir, Detail: &plugin.Detail{Exec: &plugin.Exec{Command: []string{"@hostdeck", "disk-history", "detail"}}}}
	return Handler(Config{Plugins: []plugin.Plugin{p}}), dir
}

func TestDiskHistoryReadsAnActualTimeRangeAndOnlyDirectChildrenWithoutWriting(t *testing.T) {
	h, dir := diskHistoryConsole(t)
	before, _ := os.ReadFile(filepath.Join(dir, "data", "history.json"))
	resp := get(h, "http://127.0.0.1:4678/api/disk-history/renamed-disk?range=24h", nil)
	defer resp.Body.Close()
	data, _ := json.Marshal(decodeJSON(t, resp))
	if resp.StatusCode != 200 || gjson.GetBytes(data, "change").Int() != 200 || gjson.GetBytes(data, "points.#").Int() != 3 || gjson.GetBytes(data, "children.#").Int() != 2 {
		t.Fatalf("range and children: HTTP %d %s", resp.StatusCode, data)
	}
	if gjson.GetBytes(data, "from").String() != "2026-10-25T12:00:00Z" || gjson.GetBytes(data, "children.0.path").String() != "Library" {
		t.Fatalf("baseline/order: %s", data)
	}
	child := get(h, "http://127.0.0.1:4678/api/disk-history/renamed-disk?path=Library&range=7d", nil)
	defer child.Body.Close()
	data, _ = json.Marshal(decodeJSON(t, child))
	if gjson.GetBytes(data, "children.#").Int() != 1 || gjson.GetBytes(data, "change").Int() != 150 {
		t.Fatalf("drilldown: %s", data)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "data", "history.json"))
	entries, _ := os.ReadDir(filepath.Join(dir, "data"))
	if string(before) != string(after) || len(entries) != 1 {
		t.Fatal("reading changed the saved state or acquired a writer lock")
	}
	plugins := get(h, "http://127.0.0.1:4678/api/plugins", nil)
	defer plugins.Body.Close()
	data, _ = json.Marshal(decodeJSON(t, plugins))
	if gjson.GetBytes(data, "0.view").String() != "disk-history" {
		t.Fatalf("native view was not identified: %s", data)
	}
}

func TestDiskHistoryRefusesUnknownTargetsPathsRangesAndForeignPages(t *testing.T) {
	h, _ := diskHistoryConsole(t)
	for _, query := range []string{"target=unknown", "path=..", "path=Library%2F..", "path=missing", "range=forever", "from=broken&to=broken"} {
		resp := get(h, "http://127.0.0.1:4678/api/disk-history/renamed-disk?"+query, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d", query, resp.StatusCode)
		}
	}
	resp := get(h, "http://127.0.0.1:4678/api/disk-history/renamed-disk", map[string]string{"Sec-Fetch-Site": "same-site"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign page: HTTP %d", resp.StatusCode)
	}
	resp = get(h, "http://127.0.0.1:4678/api/disk-history/unknown", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown plugin: HTTP %d", resp.StatusCode)
	}
}

func TestDiskHistoryNeverTurnsUnreadableOrCappedSummariesIntoDeletions(t *testing.T) {
	h, dir := diskHistoryConsole(t)
	file := filepath.Join(dir, "data", "history.json")
	data, _ := os.ReadFile(file)
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	for _, readings := range state["scans"].(map[string]any) {
		scans := readings.([]any)
		last := scans[len(scans)-1].(map[string]any)
		last["truncated"] = true
		sizes := last["sizes"].(map[string]any)
		sizes["."] = map[string]any{"bytes": 1300, "partial": true, "missed": "different"}
		delete(sizes, "code")
	}
	data, _ = json.Marshal(state)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	resp := get(h, "http://127.0.0.1:4678/api/disk-history/renamed-disk", nil)
	defer resp.Body.Close()
	data, _ = json.Marshal(decodeJSON(t, resp))
	if gjson.GetBytes(data, "change").Type != gjson.Null || gjson.GetBytes(data, "points.2.connected").Bool() || gjson.GetBytes(data, "periods.1.bytes").Type != gjson.Null {
		t.Fatalf("unreadable observations compared: %s", data)
	}
	for _, child := range gjson.GetBytes(data, "children").Array() {
		if child.Get("path").String() == "code" && (child.Get("bytes").Type != gjson.Null || child.Get("change").Type != gjson.Null) {
			t.Fatalf("missing capped summary became deletion: %s", child)
		}
	}
}

func TestSelectingADiskPeriodChangesTheDirectoryComparisonButKeepsTheCurve(t *testing.T) {
	h, _ := diskHistoryConsole(t)
	resp := get(h, "http://127.0.0.1:4678/api/disk-history/renamed-disk?from=2026-10-26T00:00:00Z&to=2026-10-26T12:00:00Z", nil)
	defer resp.Body.Close()
	data, _ := json.Marshal(decodeJSON(t, resp))
	if gjson.GetBytes(data, "change").Int() != 100 || gjson.GetBytes(data, "children.0.change").Int() != 50 || gjson.GetBytes(data, "points.#").Int() != 3 {
		t.Fatalf("selected interval: %s", data)
	}
	resp = get(h, "http://127.0.0.1:4678/api/disk-history/renamed-disk?range=1h&from=2026-10-24T12:00:00Z&to=2026-10-26T12:00:00Z", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("selected period escaped the range: HTTP %d", resp.StatusCode)
	}
}
