package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/chromedp/chromedp"
)

func TestScheduledTaskConfigurationIsReadableInBothLanguagesAndNarrowWindows(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.Mkdir(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := catalog.Add(examples.Files, pluginDir, "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	config := `{"version":1,"tasks":[{"id":"backup","name":"Backup 中文","enabled":true,"schedule":{"cron":"0 2 * * *","timezone":"Asia/Shanghai"},"exec":{"command":["missing-task-program","argument with spaces"]}}]}`
	if err := os.WriteFile(filepath.Join(p, "tasks.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/scheduled-tasks"), chromedp.WaitVisible(chromedp.CSS(`.detail-item`)))
			for _, width := range []int64{1280, 520} {
				do(t, ctx, chromedp.EmulateViewport(width, 800))
				ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.detail-name').textContent==='Backup 中文' && document.querySelector('.detail-view').textContent.includes('Asia/Shanghai') && document.querySelector('.detail-view').textContent.includes('missing-task-program') && document.querySelectorAll('.detail-actions button').length===0 && document.documentElement.scrollWidth<=innerWidth`))
				if err != nil || !ok {
					t.Fatalf("configuration at %dpx: %v %v", width, ok, err)
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(p, "data")); !os.IsNotExist(err) {
		t.Fatalf("preview created runtime data: %v", err)
	}
}
