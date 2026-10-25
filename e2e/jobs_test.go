package e2e

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestTheJobsPageCountsAttentionAndOffersRestartOrRun(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"#/.plugins"),
		chromedp.WaitVisible(chromedp.CSS(`#catalog-available .catalog-item[data-id="jobs"] .btn`)),
	)
	const add = `#catalog-available .catalog-item[data-id="jobs"] .btn`
	do(t, ctx,
		chromedp.Click(chromedp.CSS(add)),
		chromedp.Click(chromedp.CSS(add)),
		chromedp.WaitVisible(chromedp.CSS(`#nav .nav-item[data-id="jobs"]`)),
	)
	// Installation is real; the plugin program is a boundary fake so every
	// runner has the same jobs without restarting any real service.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writePlugin(t, dir, "jobs", fmt.Sprintf(`schema: 1
name: {en: Background jobs, zh-CN: 后台任务}
card:
  exec: {command: [%q, fake-plugin, jobs-card]}
detail:
  exec: {command: [%q, fake-plugin, jobs-detail]}
actions:
  exec: {command: [%q, fake-plugin, action]}
`, exe, exe, exe))
	cliCommand(t, 0, "plugin", "reload", "--url", url)
	do(t, ctx,
		chromedp.Click(chromedp.CSS(`#nav-overview`)),
		chromedp.WaitVisible(chromedp.CSS(`#cards .card[data-id="jobs"]`)),
	)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const card = document.querySelector('#cards .card[data-id="jobs"]');
		if (!card) return false;
		const label = card.querySelector('.card-label')?.textContent || '';
		return label.includes('需要处理') || !!card.querySelector('.card-error');
	})()`)); err != nil {
		t.Fatal(err)
	}
	if got := text(t, ctx, `#cards .card[data-id="jobs"] .card-value`); got != "1" {
		t.Fatalf("card %q, want how many jobs need attention", got)
	}

	do(t, ctx,
		chromedp.Click(chromedp.CSS(`#nav .nav-item[data-id="jobs"]`)),
		chromedp.WaitVisible(chromedp.CSS(`#service .detail-list`)),
	)
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`(() => {
		const view = document.querySelector('#service .detail-view');
		if (!view || view.getAttribute('aria-busy') === 'true') return false;
		const list = view.querySelector('.detail-list');
		if (!list || list.textContent.includes('读取中')) return false;
		return !!list.querySelector('.detail-item, .empty, [role="alert"]');
	})()`)); err != nil {
		t.Fatal(err)
	}
	if got := text(t, ctx, `#service-name`); got != "后台任务" {
		t.Fatalf("opened %q", got)
	}
	alert, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.querySelector('#service .detail-list [role="alert"]')?.textContent || ''`))
	if err != nil {
		t.Fatal(err)
	}
	if alert != "" {
		t.Fatalf("detail says %q", alert)
	}
	listed, err := chromedp.Run(ctx, chromedp.Evaluate[[]jobRow](`
 [...document.querySelectorAll('#service .detail-item')].map(row => ({
   name: row.querySelector('.detail-name').textContent,
   buttons: [...row.querySelectorAll('.detail-actions .btn.action')].map(b => b.textContent)
 }))`))
	if err != nil {
		t.Fatal(err)
	}
	want := []jobRow{{"Running job", []string{"重启"}}, {"Idle job", []string{"运行"}}, {"Failed job", []string{"运行"}}}
	if !reflect.DeepEqual(listed, want) {
		t.Fatalf("jobs %+v, want %+v", listed, want)
	}
}

type jobRow struct {
	Name    string   `json:"name"`
	Buttons []string `json:"buttons"`
}

const jobsCard = `{"status":"error","value":"1","label":{"en":"need attention","zh-CN":"需要处理"}}`
const jobsDetail = `{"items":[
 {"id":"running","name":"Running job","status":"ok","actions":[{"id":"restart","label":{"en":"Restart","zh-CN":"重启"}}]},
 {"id":"idle","name":"Idle job","status":"ok","actions":[{"id":"start","label":{"en":"Run","zh-CN":"运行"}}]},
 {"id":"failed","name":"Failed job","status":"error","actions":[{"id":"start","label":{"en":"Run","zh-CN":"运行"}}]}
]}`
