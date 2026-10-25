package e2e

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestTheJobsPageCountsAttentionAndOffersRestartOrRun(t *testing.T) {
	url := startConsole(t, t.TempDir())
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
	if got := text(t, ctx, `#cards .card[data-id="jobs"]`); !strings.Contains(got, "需要处理") {
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
	listed, err := chromedp.Run(ctx, chromedp.Evaluate[jobsPage](`({
		names: [...document.querySelectorAll('#service .detail-name')].map((n) => n.textContent),
		buttons: [...document.querySelectorAll('#service .detail-actions .btn.action')].map((b) => b.textContent),
	})`))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range listed.Names {
		if strings.HasPrefix(name, "com.apple.") || strings.HasPrefix(name, "application.") {
			t.Errorf("listed %q, want Apple's agents and ordinary applications left out", name)
		}
	}
	for _, label := range listed.Buttons {
		if label != "重启" && label != "运行" {
			t.Errorf("button %q, want 重启 or 运行", label)
		}
	}
}

type jobsPage struct {
	Names   []string `json:"names"`
	Buttons []string `json:"buttons"`
}
