package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
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

func TestEditingUsesAFocusedDialogAndCancelKeepsTheSavedPage(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "web", "schema: 1\nname: Web\ndescription: A web page with a description.\npage: {link: 'https://example.test/'}\n")
	before, err := os.ReadFile(filepath.Join(dir, "plugins", "web", "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := openBrowser(t, "en")
	url := startConsole(t, dir)
	const edit = `#catalog-installed .catalog-item[data-id="web"] .page-edit-open`
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-installed .catalog-item`)))
	check := func(js string) {
		t.Helper()
		got, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err != nil || !got {
			t.Fatalf("%s: %v %v", js, got, err)
		}
	}
	check(`!!document.querySelector('#catalog-installed .page-edit-open') && !document.querySelector('#catalog-installed details.page-edit')`)
	do(t, ctx, chromedp.Click(chromedp.CSS(edit)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open]`)))
	check(`document.querySelector('#page-editor').matches(':modal') && document.activeElement.matches('#page-editor input[name="name"]') && document.querySelector('#editor-title').textContent.includes('Web') && document.querySelector('#editor-description').textContent.includes('description') && [...document.querySelectorAll('#page-editor input, #page-editor select')].every(field=>field.labels.length)`)
	do(t, ctx, chromedp.Focus(chromedp.CSS(`#page-editor button[type="submit"]`)), chromedp.KeyEvent(kb.Tab))
	check(`document.activeElement.matches('#page-editor input[name="name"]')`)
	do(t, ctx, chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(chromedp.ModifierShift)))
	check(`document.activeElement.matches('#page-editor button[type="submit"]')`)
	do(t, ctx, chromedp.KeyEvent(kb.Escape))
	check(`!document.querySelector('#page-editor').open && document.activeElement.matches('#catalog-installed .page-edit-open')`)
	do(t, ctx, chromedp.Click(chromedp.CSS(edit)), chromedp.SendKeys(chromedp.CSS(`#page-editor input[name="name"]`), " discarded"), chromedp.Click(chromedp.CSS(`#editor-cancel`)))
	check(`!document.querySelector('#page-editor').open && document.activeElement.matches('#catalog-installed .page-edit-open') && document.querySelector('#catalog-installed .card-name').textContent === 'Web'`)
	after, err := os.ReadFile(filepath.Join(dir, "plugins", "web", "plugin.yaml"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("cancel changed the saved page: %s %v", after, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(edit)))
	check(`document.querySelector('#page-editor input[name="name"]').value === 'Web'`)
	for _, width := range []int{1280, 768, 520} {
		do(t, ctx, chromedp.EmulateViewport(int64(width), 700))
		check(`(() => {const d=document.querySelector('#page-editor'),r=d.getBoundingClientRect();return r.left>=0 && r.right<=innerWidth && r.top>=0 && r.bottom<=innerHeight && d.scrollWidth<=d.clientWidth+1 && d.querySelector('button[type="submit"]').getBoundingClientRect().height>=44;})()`)
	}
}

func TestLongEditorContentScrollsWithoutHidingItsActions(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "web", "schema: 1\nname: "+strings.Repeat("A very long name ", 40)+"\ndescription: "+strings.Repeat("A detailed explanation. ", 100)+"\npage: {link: 'https://example.test/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(520, 700), chromedp.Navigate(url+"#/.plugins"), chromedp.Click(chromedp.CSS(`#catalog-installed .page-edit-open`)))
	fits, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {const d=document.querySelector('#page-editor'),body=d.querySelector('.editor-body'),actions=d.querySelector('.editor-actions'),b=actions.getBoundingClientRect(),r=d.getBoundingClientRect();return d.open && r.top>=0 && r.bottom<=innerHeight && d.querySelector('.editor-head').getBoundingClientRect().height<=128 && body.clientHeight>=100 && body.scrollHeight>body.clientHeight && b.top>=r.top && b.bottom<=r.bottom && document.documentElement.scrollWidth<=innerWidth;})()`))
	if err != nil || !fits {
		t.Fatalf("long editor content should scroll above visible actions: %v %v", fits, err)
	}
}

func TestAnInvalidManifestRemainsAPluginWithAnActionableError(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "bad", "schema: 1\nname: Broken\nunknown: true\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	const row = `#catalog-installed .catalog-item[data-id="bad"]`
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(row)))
	if kind := text(t, ctx, row+` .catalog-kind`); kind != "Plugin" {
		t.Fatalf("invalid manifest is described as %q, want Plugin", kind)
	}
	if reason := text(t, ctx, row+` .catalog-state-detail`); !strings.Contains(reason, "unknown") {
		t.Fatalf("missing actionable manifest error: %q", reason)
	}
}

func TestBuiltInPluginsUseAlignedRowsAndKeepTheirSetupInstructionsReadable(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "site", "schema: 1\nname: Site\npage: {link: 'https://example.test/'}\n")
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-available .add`)))
	aligned, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
 const rows=[...document.querySelectorAll('#catalog-available .catalog-item')];
 const installed=document.querySelector('#catalog-installed .catalog-item').getBoundingClientRect();
 return rows.length>=2 && rows.every((row,i)=>{
  const r=row.getBoundingClientRect(),previous=i?rows[i-1].getBoundingClientRect():null;
  const summary=row.querySelector('.catalog-summary'),actions=row.querySelector('.catalog-actions');
  return Math.abs(r.left-installed.left)<1 && Math.abs(r.width-installed.width)<1 && (!previous || r.top>=previous.bottom) && summary && actions && Math.abs(summary.getBoundingClientRect().left-rows[0].querySelector('.catalog-summary').getBoundingClientRect().left)<1 && Math.abs(actions.getBoundingClientRect().right-rows[0].querySelector('.catalog-actions').getBoundingClientRect().right)<1;
 });
 })()`))
	if err != nil || !aligned {
		t.Fatalf("built-ins should use full-width rows with aligned identity, features and actions: %v %v", aligned, err)
	}
	for _, language := range []string{"en", "zh-CN"} {
		do(t, ctx, chromedp.EmulateViewport(1280, 900), chromedp.Click(chromedp.CSS(`[data-lang="`+language+`"]`)))
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.lang === `+fmt.Sprintf("%q", language))); err != nil {
			t.Fatal(err)
		}
		for _, width := range []int{1280, 768, 520} {
			do(t, ctx, chromedp.EmulateViewport(int64(width), 900))
			fits, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.documentElement.scrollWidth<=innerWidth && [...document.querySelectorAll('#catalog-available .catalog-item')].every(row=>{const description=row.querySelector('.catalog-description'),button=row.querySelector('.add'),r=row.getBoundingClientRect(),b=button.getBoundingClientRect();return row.scrollWidth<=row.clientWidth+1 && description.scrollHeight<=description.clientHeight+1 && b.height>=44 && b.left>=r.left && b.right<=r.right;})`))
			if err != nil || !fits {
				t.Fatalf("built-in instructions or actions are clipped at %dpx in %s: %v %v", width, language, fits, err)
			}
		}
		do(t, ctx, chromedp.Click(chromedp.CSS(`#catalog-available [data-id="syncthing"] .add`)))
		armed, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {const row=document.querySelector('#catalog-available [data-id="syncthing"]'),button=row.querySelector('.add'),b=button.getBoundingClientRect(),r=row.getBoundingClientRect();return (button.textContent.includes('again') || button.textContent.includes('再点一次')) && b.left>=r.left && b.right<=r.right && b.height>=44;})()`))
		if err != nil || !armed {
			t.Fatalf("the confirmation label must remain inside the row in %s: %v %v", language, armed, err)
		}
	}
	if reason := text(t, ctx, `#catalog-available [data-id="syncthing"] .catalog-description`); !strings.Contains(reason, "SYNCTHING_API_KEY") {
		t.Fatalf("missing setup instructions: %q", reason)
	}
}
