package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestManagementSeparatesWorkspaceVisibilityFromEnablement(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "web", "schema: 1\nname: Web\npage: {link: 'https://example.test/'}\n")
	if err := os.WriteFile(filepath.Join(dir, "layout.json"), []byte(`{"sidebar":["web"],"hidden":["web"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	const row = `#catalog-installed .catalog-item[data-id="web"]`
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(row)))
	check := func(state, visibility string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {const row=document.querySelector('#catalog-installed .catalog-item[data-id="web"]');return row.querySelector('.catalog-state').textContent.includes(`+fmt.Sprintf("%q", state)+`) && row.querySelector('.catalog-visibility')?.textContent.includes(`+fmt.Sprintf("%q", visibility)+`);})()`))
		if err != nil || !got {
			t.Fatalf("want separate enablement %q and visibility %q: %v %v", state, visibility, got, err)
		}
	}
	check("已启用", "工作区已隐藏")
	if got := text(t, ctx, row+` .show`); got != "恢复到工作区" {
		t.Fatalf("restore visibility says %q", got)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(row+` .show`)), chromedp.WaitVisible(chromedp.CSS(`#nav .nav-item[data-id="web"]`)))
	check("已启用", "工作区可见")
	do(t, ctx, chromedp.Click(chromedp.CSS(row+` .more-trigger`)), chromedp.Click(chromedp.CSS(row+` .hide`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#catalog-installed .catalog-item[data-id="web"] .catalog-visibility')?.textContent.includes('工作区已隐藏')`)); err != nil {
		t.Fatal(err)
	}
	check("已启用", "工作区已隐藏")
	savedLayout(t, dir, "hidden", []string{"web"})
	if _, err := os.Stat(filepath.Join(dir, "plugins", "web", "plugin.yaml")); err != nil {
		t.Fatalf("hiding must not disable the plugin: %v", err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(row+` .more-trigger`)), chromedp.Click(chromedp.CSS(row+` .catalog-toggle`)), chromedp.Click(chromedp.CSS(row+` .catalog-toggle`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#catalog-installed .catalog-item[data-id="web"] .catalog-state')?.dataset.state === 'disabled'`)); err != nil {
		t.Fatal(err)
	}
	check("已停用", "启用后仍隐藏")
	if _, err := os.Stat(filepath.Join(dir, "plugins", ".web", "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(row+` .catalog-toggle`)), chromedp.Click(chromedp.CSS(row+` .catalog-toggle`)))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#catalog-installed .catalog-item[data-id="web"] .catalog-state')?.dataset.state === 'enabled'`)); err != nil {
		t.Fatal(err)
	}
	check("已启用", "工作区已隐藏")
	savedLayout(t, dir, "hidden", []string{"web"})
}

func TestTheManagementDestinationFitsItsSidebarLabelInBothLanguages(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-add`)))
	for _, language := range []string{"en", "zh-CN"} {
		do(t, ctx, chromedp.Click(chromedp.CSS(`[data-lang="`+language+`"]`)))
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.lang === `+fmt.Sprintf("%q", language))); err != nil {
			t.Fatal(err)
		}
		fits, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {const name=document.querySelector('#nav-catalog .nav-name');return name.scrollWidth <= name.clientWidth;})()`))
		if err != nil || !fits {
			t.Fatalf("management destination is truncated in %s: %v %v", language, fits, err)
		}
	}
}
