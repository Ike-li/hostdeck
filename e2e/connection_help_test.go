package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

func TestConnectionHelpExplainsWebPagesAndPluginsInBothLanguages(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	for _, language := range []string{"zh-CN", "en"} {
		ctx := openBrowser(t, language)
		do(t, ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-add`)))
		exists, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!!document.querySelector('#connection-help')`))
		if err != nil || !exists {
			t.Fatalf("the add area needs discoverable connection help: %v %v", exists, err)
		}
		do(t, ctx, chromedp.Click(chromedp.CSS(`#connection-help > summary`)))
		page := text(t, ctx, `#help-pages`)
		plugin := text(t, ctx, `#help-plugins`)
		wantPage, wantPlugin := "内嵌", "重启"
		if language == "en" {
			wantPage, wantPlugin = "Embed", "Restart"
		}
		if !strings.Contains(page, wantPage) || !strings.Contains(plugin, wantPlugin) || !strings.Contains(plugin, filepath.Join(dir, "plugins")) || !strings.Contains(plugin, "schema: 1") {
			t.Fatalf("incomplete %s instructions: pages %q; plugins %q", language, page, plugin)
		}
		for _, width := range []int64{1280, 768, 520} {
			do(t, ctx, chromedp.EmulateViewport(width, 800))
			fits, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.documentElement.scrollWidth <= innerWidth && [...document.querySelectorAll('#connection-help button')].every(b => b.getBoundingClientRect().height >= 44)`))
			if err != nil || !fits {
				t.Fatalf("help should fit at %dpx in %s: %v %v", width, language, fits, err)
			}
		}
	}
}

func TestOneClickCopiesTheCompletePromptIntoTheBrowserClipboard(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.Click(chromedp.CSS(`#connection-help > summary`)))
	if _, err := chromedp.CallBrowser(ctx, browser.SetPermission, browser.SetPermissionParams{
		Permission: &browser.PermissionDescriptor{Name: "clipboard-read"},
		Setting:    browser.PermissionSettingGranted,
		Origin:     url,
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"page", "plugin"} {
		do(t, ctx, chromedp.Click(chromedp.CSS(`#copy-`+kind+`-prompt`)))
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#copy-`+kind+`-note').dataset.kind === 'success'`)); err != nil {
			t.Fatal(err)
		}
		copied, err := chromedp.Run(ctx, chromedp.Evaluate[string](`navigator.clipboard.readText()`, chromedp.EvalAwaitPromise))
		if err != nil {
			t.Fatal(err)
		}
		// The Windows clipboard represents line breaks as CRLF.
		copied = strings.ReplaceAll(copied, "\r\n", "\n")
		preview, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.querySelector('#`+kind+`-agent-prompt').value`))
		if err != nil || len(copied) < 500 || copied != preview {
			t.Fatalf("%s clipboard and preview differ: %d vs %d characters, %v", kind, len(copied), len(preview), err)
		}
	}
}

func TestAgentPromptsCopyTheCurrentConnectionContextAndOfferManualCopyOnFailure(t *testing.T) {
	dir := t.TempDir()
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-add`)))
	exists, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!!document.querySelector('#copy-page-prompt') && !!document.querySelector('#copy-plugin-prompt')`))
	if err != nil || !exists {
		t.Fatalf("both agent prompts need one-click copy buttons: %v %v", exists, err)
	}
	do(t, ctx, chromedp.Click(chromedp.CSS(`#connection-help > summary`)))
	for _, language := range []string{"zh-CN", "en"} {
		do(t, ctx, chromedp.Click(chromedp.CSS(`[data-lang="`+language+`"]`)))
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.documentElement.lang === `+fmt.Sprintf("%q", language))); err != nil {
			t.Fatal(err)
		}
		// The clipboard boundary captures exactly what a click copies.
		_, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText:async value=>{window.copiedPrompt=value;}}}),true)`))
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"page", "plugin"} {
			do(t, ctx, chromedp.Click(chromedp.CSS(`#copy-`+kind+`-prompt`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#copy-`+kind+`-note').dataset.kind === 'success'`)); err != nil {
				t.Fatal(err)
			}
			copied, err := chromedp.Run(ctx, chromedp.Evaluate[string](`window.copiedPrompt`))
			if err != nil || !strings.Contains(copied, url) || !strings.Contains(copied, filepath.Join(dir, "plugins")) || !strings.Contains(copied, "```sh") || !strings.Contains(copied, "```powershell") {
				t.Fatalf("%s %s prompt lacks runnable commands or current context: %q %v", language, kind, copied, err)
			}
			verb := " page add --url "
			if kind == "plugin" {
				verb = " plugin install --url "
			}
			if !strings.Contains(copied, binary) || !strings.Contains(copied, verb) {
				t.Fatalf("%s prompt must invoke this running executable's native CLI: %q", kind, copied)
			}
			_, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`(navigator.clipboard.writeText=async()=>{throw new Error('denied');},true)`))
			if err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`#copy-`+kind+`-prompt`)))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#copy-`+kind+`-note').dataset.kind === 'error' && document.activeElement.id === '`+kind+`-agent-prompt' && document.activeElement.selectionEnd === document.activeElement.value.length`)); err != nil {
				t.Fatalf("clipboard denial should select the prompt and report failure: %v", err)
			}
			// Restore the successful boundary for the next button.
			_, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`(navigator.clipboard.writeText=async value=>{window.copiedPrompt=value;},true)`))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "plugins"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("reading and copying help must not write plugin data: %v %v", entries, err)
	}
}

func TestAgentCommandExamplesWriteOnlyTheSelectedDataDirectory(t *testing.T) {
	// Quotes, spaces and shell syntax in a real data path must stay literal.
	dir := filepath.Join(t.TempDir(), "owner's data $HOME")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-add`)))
	exists, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`!!document.querySelector('#plugin-agent-prompt')`))
	if err != nil || !exists {
		t.Fatalf("agent command examples must be inspectable: %v %v", exists, err)
	}
	runExample := func(kind string) error {
		t.Helper()
		prompt, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.querySelector('#`+kind+`-agent-prompt').value`))
		if err != nil {
			t.Fatal(err)
		}
		fence, shell, args := "```sh\n", "sh", []string{"-c"}
		if runtime.GOOS == "windows" {
			fence, shell, args = "```powershell\n", "powershell", []string{"-NoProfile", "-NonInteractive", "-Command"}
		}
		_, rest, ok := strings.Cut(prompt, fence)
		if !ok {
			t.Fatalf("prompt lacks %s example", shell)
		}
		command, _, ok := strings.Cut(rest, "\n```")
		if !ok {
			t.Fatal("unterminated command example")
		}
		out, err := exec.Command(shell, append(args, command)...).CombinedOutput()
		if err != nil {
			t.Logf("%s example: %s", kind, out)
		}
		return err
	}
	if err := runExample("plugin"); err != nil {
		t.Fatal(err)
	}
	plugins, broken, err := plugin.Load(filepath.Join(dir, "plugins"))
	if err != nil || len(broken) != 0 || len(plugins) != 1 || plugins[0].ID != "my-service" {
		t.Fatalf("command should create a valid custom plugin: %v %v %v", plugins, broken, err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "plugins", "my-service", "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runExample("plugin"); err == nil {
		t.Fatal("the example must refuse to replace an existing plugin")
	}
	after, err := os.ReadFile(filepath.Join(dir, "plugins", "my-service", "plugin.yaml"))
	if err != nil || string(before) != string(after) {
		t.Fatal("a repeated command replaced the plugin")
	}
	if err := os.Rename(filepath.Join(dir, "plugins", "my-service"), filepath.Join(dir, "plugins", ".my-service")); err != nil {
		t.Fatal(err)
	}
	if err := runExample("plugin"); err == nil {
		t.Fatal("the example must also refuse an ID belonging to a disabled plugin")
	}
	if err := runExample("page"); err != nil {
		t.Fatal(err)
	}
	plugins, broken, err = plugin.Load(filepath.Join(dir, "plugins"))
	if err != nil || len(broken) != 0 || len(plugins) != 1 || plugins[0].Page == nil || plugins[0].Page.Proxy != "http://127.0.0.1:3000/" {
		t.Fatalf("page API example should create a valid page and leave the custom plugin disabled: %v %v %v", plugins, broken, err)
	}
	loaded, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`fetch('/api/catalog').then(r=>r.json()).then(c=>c.installed.some(p=>p.proxy==='http://127.0.0.1:3000/' && p.loaded && !p.broken && !p.error))`, chromedp.EvalAwaitPromise))
	if err != nil || !loaded {
		t.Fatalf("the page command must reload immediately: %v %v", loaded, err)
	}
}
