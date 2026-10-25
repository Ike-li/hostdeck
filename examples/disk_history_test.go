package examples

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/internal/card"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/Ike-li/hostdeck/internal/detail"
	"github.com/Ike-li/hostdeck/internal/diskhistory"
	"github.com/Ike-li/hostdeck/internal/plugin"
)

func historyPlugin(t *testing.T) (plugin.Plugin, string) {
	t.Helper()
	dir, err := catalog.Add(Files, t.TempDir(), "disk-history")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { diskhistory.Sync(filepath.Dir(dir), nil) })
	target := t.TempDir()
	config, _ := json.Marshal(map[string]any{"roots": []string{target}})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := plugin.ReadFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p, target
}

func readHistory(t *testing.T, p plugin.Plugin) detail.View {
	t.Helper()
	v := detail.Read(context.Background(), p)
	if v.Error != nil {
		t.Fatalf("detail: %+v", v.Error)
	}
	return v
}

func scanHistory(t *testing.T, p plugin.Plugin) detail.View {
	t.Helper()
	v := readHistory(t, p)
	if len(v.Items) == 0 {
		t.Fatal("missing scan target")
	}
	_, err := detail.Act(context.Background(), p, v.Items[0].ID, "scan")
	if err != nil {
		t.Fatalf("scan: %+v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v = readHistory(t, p)
		if strings.Contains(v.Items[0].Log, "UTC") {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("scan never published: %+v", v)
	return v
}

func TestDiskHistoryInstallsWithoutAnInterpreterAndDoesNotScanOnARead(t *testing.T) {
	p, _ := historyPlugin(t)
	c := card.Fetch(context.Background(), p)
	if c.Error != nil || c.Label.ByLang["en"] == "" || c.Label.ByLang["zh-CN"] == "" {
		t.Fatalf("card %+v", c)
	}
	v := readHistory(t, p)
	if len(v.Items) != 1 || len(v.Items[0].Actions) < 2 || v.Items[0].Log != "" {
		t.Fatalf("unscanned view %+v", v)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "data")); !os.IsNotExist(err) {
		t.Fatalf("a read wrote data: %v", err)
	}
}

func TestDiskHistoryRecordsChangesAndRetainsHistoryAfterReload(t *testing.T) {
	p, target := historyPlugin(t)
	if err := os.Mkdir(filepath.Join(target, "目录"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(target, "目录", "file")
	if err := os.WriteFile(file, []byte(strings.Repeat("a", 1024)), 0600); err != nil {
		t.Fatal(err)
	}
	v := scanHistory(t, p)
	if v.Items[0].Value != "1.02 KB" {
		t.Fatalf("first scan %+v", v.Items[0])
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("a", 2048)), 0600); err != nil {
		t.Fatal(err)
	}
	_, actErr := detail.Act(context.Background(), p, v.Items[0].ID, "scan")
	if actErr != nil {
		t.Fatalf("second scan: %+v", actErr)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v = readHistory(t, p)
		if v.Items[0].Value == "2.05 KB" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v.Items[0].Value != "2.05 KB" || strings.Count(v.Items[0].Log, "UTC") != 2 {
		t.Fatalf("second scan %+v", v.Items[0])
	}
	c := card.Fetch(context.Background(), p)
	if c.Error != nil || c.Value != "+1.02 KB" || len(c.Rows) != 1 || !strings.Contains(c.Rows[0].Value, "+1.02 KB") {
		t.Fatalf("change card %+v", c)
	}
	diskhistory.Sync(filepath.Dir(p.Dir), nil)
	reloaded, err := plugin.ReadFolder(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := readHistory(t, reloaded); got.Items[0].Log != v.Items[0].Log {
		t.Fatalf("lost saved history %+v", got)
	}
}
