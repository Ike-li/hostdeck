package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/catalog"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
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
	config := `{"version":1,"tasks":[{"id":"backup","name":"Backup 中文","enabled":false,"schedule":{"cron":"0 2 * * *","timezone":"Asia/Shanghai"},"exec":{"command":["missing-task-program","argument with spaces"]}}]}`
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
				ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.detail-name').textContent==='Backup 中文' && document.querySelector('.detail-view').textContent.includes('Asia/Shanghai') && document.querySelector('.detail-view').textContent.includes('missing-task-program') && document.querySelectorAll('.detail-actions .action').length===0 && document.querySelector('.task-edit') && document.querySelector('.task-delete') && document.documentElement.scrollWidth<=innerWidth`))
				if err != nil || !ok {
					t.Fatalf("configuration at %dpx: %v %v", width, ok, err)
				}
			}
		})
	}
	b, err := os.ReadFile(filepath.Join(p, "data/state.json"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var state struct{ Runs []any }
	if err := json.Unmarshal(b, &state); err != nil || len(state.Runs) != 0 {
		t.Fatalf("reading ran a task: %s %v", b, err)
	}
}

func TestEnabledScheduledTasksRunWithoutAnOpenPage(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "plugins")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := catalog.Add(examples.Files, root, "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	task := map[string]any{"id": "help", "name": "Scheduled help", "enabled": false, "schedule": map[string]any{"every": "1m"}, "exec": map[string]any{"command": []string{binary, "help"}}, "timeout": "10s"}
	config := map[string]any{"version": 1, "tasks": []any{task}}
	write := func() {
		b, _ := json.Marshal(config)
		if err := os.WriteFile(filepath.Join(p, "tasks.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	url := startConsole(t, dir)
	task["enabled"] = true
	write()
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/plugins/reload", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", url)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reload status %d", response.StatusCode)
	}
	for {
		b, err := os.ReadFile(filepath.Join(p, "data/state.json"))
		if err != nil {
			t.Fatal(err)
		}
		var s struct {
			Tasks map[string]struct {
				Anchor time.Time `json:"anchor_at"`
			}
			Runs []struct {
				Trigger, Status string
				Scheduled       time.Time `json:"scheduled_at"`
			}
		}
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		if len(s.Runs) > 0 {
			r := s.Runs[0]
			if r.Status == "succeeded" {
				if r.Trigger != "scheduled" || !r.Scheduled.Equal(s.Tasks["help"].Anchor.Add(time.Minute)) {
					t.Fatalf("scheduled reading %+v", s)
				}
				break
			}
			if r.Status != "starting" && r.Status != "running" {
				t.Fatalf("scheduled failure %s", b)
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no background run: %s", b)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestAScheduledTaskCanBeRunFromTheBrowserAndItsRawLogIsShown(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "plugins")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := catalog.Add(examples.Files, root, "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"version": 1, "tasks": []any{map[string]any{"id": "help", "name": "Local help 中文", "enabled": true, "schedule": map[string]any{"every": "1m"}, "exec": map[string]any{"command": []string{binary, "help"}}, "timeout": "5s"}}}
	b, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(p, "tasks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	url := startConsole(t, dir)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/scheduled-tasks"), chromedp.WaitVisible(chromedp.CSS(".detail-item")))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-actions button')!==null`, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
				t.Fatal(err)
			}
			before := 0
			if b, err := os.ReadFile(filepath.Join(p, "data/state.json")); err == nil {
				var s struct{ Runs []any }
				json.Unmarshal(b, &s)
				before = len(s.Runs)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(".detail-actions button")), chromedp.Click(chromedp.CSS(".detail-actions button")))
			until := time.Now().Add(10 * time.Second)
			for {
				state, err := os.ReadFile(filepath.Join(p, "data/state.json"))
				if err == nil {
					var s struct {
						Runs []struct{ Status, Trigger string }
					}
					if json.Unmarshal(state, &s) == nil && len(s.Runs) > before {
						last := s.Runs[len(s.Runs)-1]
						if last.Status == "succeeded" && last.Trigger == "manual" {
							break
						}
					}
				}
				if time.Now().After(until) {
					t.Fatalf("no successful run: %s %v", state, err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS("#service-reload")))
			if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('.detail-view').textContent.includes('run-') && document.querySelector('.detail-log pre')?.textContent.includes('commands')`, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestScheduledTaskSetupAndLifecycleWorkInBothLanguagesThemesAndWidths(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			dir := t.TempDir()
			url := startConsole(t, dir)
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/.plugins"), chromedp.WaitVisible(chromedp.CSS(`#catalog-available [data-id="scheduled-tasks"] .btn`)), chromedp.Click(chromedp.CSS(`#catalog-available [data-id="scheduled-tasks"] .btn`)), chromedp.Click(chromedp.CSS(`#catalog-available [data-id="scheduled-tasks"] .btn`)), chromedp.WaitVisible(chromedp.CSS(`#nav [data-id="scheduled-tasks"]`)), chromedp.Navigate(url+"#/scheduled-tasks"), chromedp.WaitVisible(chromedp.CSS(`.task-switch`)))
			poll := func(js string) {
				t.Helper()
				if _, err := chromedp.Run(ctx, chromedp.Poll[bool](js, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
					t.Fatal(err)
				}
			}
			poll(`document.querySelector('.detail-view').textContent.includes('tasks.example.json') && document.querySelectorAll('.detail-item').length===0 && document.querySelector('.task-switch [data-segment="tasks"]').getAttribute('aria-pressed')==='true' && document.querySelector('.task-script-add').hidden && !document.querySelector('.task-add').hidden`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-switch [data-segment="scripts"]`)), chromedp.WaitVisible(chromedp.CSS(`.task-script-add`)))
			poll(`document.querySelector('.task-add').hidden`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-script-add`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] textarea`)))
			do(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => { const form = document.querySelector('#page-editor form'); form.elements.name.value = 'hello.py'; form.elements.content.value = "print('你好')\\n"; form.requestSubmit(); })()`))
			poll(`!!document.querySelector('.detail-item[data-script="hello.py"]') && !document.querySelector('#page-editor').open`)
			scheduleLabel := map[string]string{"en": "Schedule", "zh-CN": "设定时"}[lang]
			runLabel := map[string]string{"en": "Run", "zh-CN": "运行"}[lang]
			poll(`document.querySelector('.detail-item[data-script="hello.py"] .script-schedule')?.textContent===` + fmt.Sprintf("%q", scheduleLabel) + ` && document.querySelector('.detail-item[data-script="hello.py"] .script-run')?.textContent===` + fmt.Sprintf("%q", runLabel))
			for _, width := range []int64{1280, 520} {
				do(t, ctx, chromedp.EmulateViewport(width, 800))
				poll(`document.documentElement.scrollWidth<=innerWidth && document.querySelector('.task-script-add').getBoundingClientRect().height>=44 && document.querySelector('.script-schedule').getBoundingClientRect().height>=44 && document.querySelector('.script-run').getBoundingClientRect().height>=44 && document.querySelector('.script-delete').getBoundingClientRect().height>=44`)
			}
			defaultCommand := "python3 scripts/hello.py"
			if runtime.GOOS == "windows" {
				defaultCommand = "py -3 scripts/hello.py"
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-item[data-script="hello.py"] .script-schedule`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] [name="command"]`)))
			poll(`document.querySelector('#page-editor [name="name"]').value==='hello.py' && document.querySelector('#page-editor [name="command"]').value===` + fmt.Sprintf("%q", defaultCommand) + ` && !document.querySelector('#page-editor [name="id"]')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#editor-cancel`)))
			poll(`!document.querySelector('#page-editor').open`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-script-add`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] textarea`)))
			do(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => { const form = document.querySelector('#page-editor form'); form.elements.name.value = 'notes.txt'; form.elements.content.value = "hello\\n"; form.requestSubmit(); })()`))
			poll(`!!document.querySelector('.detail-item[data-script="notes.txt"] .script-schedule') && !document.querySelector('.detail-item[data-script="notes.txt"] .script-run')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-item[data-script="notes.txt"] .script-schedule`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] [name="command"]`)))
			poll(`document.querySelector('#page-editor [name="command"]').value===''`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#editor-cancel`)))
			poll(`!document.querySelector('#page-editor').open`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-item[data-script="notes.txt"] .script-delete`)), chromedp.Click(chromedp.CSS(`.detail-item[data-script="notes.txt"] .script-delete`)))
			poll(`!document.querySelector('.detail-item[data-script="notes.txt"]') && !!document.querySelector('.detail-item[data-script="hello.py"]')`)
			tasksPath := filepath.Join(dir, "plugins", "scheduled-tasks", "tasks.json")
			tasksBefore, err := os.ReadFile(tasksPath)
			if err != nil {
				t.Fatal(err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-item[data-script="hello.py"] .script-run`)), chromedp.Click(chromedp.CSS(`.detail-item[data-script="hello.py"] .script-run`)))
			poll(`!!document.querySelector('.detail-item[data-script="hello.py"] .script-result')?.textContent.trim()`)
			tasksAfter, err := os.ReadFile(tasksPath)
			if err != nil || string(tasksAfter) != string(tasksBefore) {
				t.Fatalf("script run changed tasks.json: %s", tasksAfter)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-switch [data-segment="tasks"]`)))
			poll(`document.querySelectorAll('.detail-item').length===0 && document.querySelector('.detail-view').textContent.includes('tasks.example.json')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-switch [data-segment="scripts"]`)))
			poll(`!!document.querySelector('.detail-item[data-script="hello.py"]')`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.script-edit`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] textarea`)))
			// When editing, the name field is disabled and focus starts on the textarea.
			// Tab from Save must wrap to the textarea, not skip it for Cancel.
			do(t, ctx, chromedp.Focus(chromedp.CSS(`#page-editor button[type="submit"]`)), chromedp.KeyEvent(kb.Tab))
			if ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.matches('#page-editor textarea[name="content"]')`)); err != nil || !ok {
				t.Fatalf("Tab from Save should wrap to the script textarea: %v %v", ok, err)
			}
			do(t, ctx, chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(chromedp.ModifierShift)))
			if ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.activeElement.matches('#page-editor button[type="submit"]')`)); err != nil || !ok {
				t.Fatalf("Shift+Tab from the script textarea should wrap to Save: %v %v", ok, err)
			}
			do(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => { const form = document.querySelector('#page-editor form'); form.elements.content.value = "print(2)\n"; form.requestSubmit(); })()`))
			poll(`!document.querySelector('#page-editor').open`)
			scriptPath := filepath.Join(dir, "plugins", "scheduled-tasks", "scripts", "hello.py")
			if got, err := os.ReadFile(scriptPath); err != nil || string(got) != "print(2)\n" {
				t.Fatalf("saved script %q %v", got, err)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.script-delete`)), chromedp.Click(chromedp.CSS(`.script-delete`)))
			poll(`!document.querySelector('.detail-item[data-script="hello.py"]') && !!document.querySelector('.detail-list [data-empty="scripts"]')`)
			if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
				t.Fatal("deleted script still on disk")
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-switch [data-segment="tasks"]`)))
			poll(`document.querySelector('.task-switch [data-segment="tasks"]').getAttribute('aria-pressed')==='true' && !document.querySelector('.task-add').hidden`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-add`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] input`)))
			do(t, ctx, chromedp.EmulateViewport(520, 800))
			poll(`document.querySelector('#page-editor').getBoundingClientRect().width<=innerWidth && document.querySelector('.task-enabled').getBoundingClientRect().height>=44 && document.querySelector('#page-editor [name="timezone"]').value===` + fmt.Sprintf("%q", map[string]string{"en": "UTC", "zh-CN": "Asia/Shanghai"}[lang]))
			do(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => { const form = document.querySelector('#page-editor form'); form.elements.name.value = 'From the page'; form.querySelector('[data-schedule="every"]').click(); form.elements.every.value = '24h'; form.elements.command.value = 'echo hello'; form.requestSubmit(); })()`))
			poll(`document.querySelector('.detail-item[data-id="from-the-page"] .detail-name')?.textContent==='From the page' && !document.querySelector('#page-editor').open`)
			for _, width := range []int64{1280, 520} {
				do(t, ctx, chromedp.EmulateViewport(width, 800))
				poll(`document.documentElement.scrollWidth<=innerWidth && document.querySelector('.task-add').getBoundingClientRect().height>=44 && document.querySelector('.task-edit').getBoundingClientRect().height>=44 && document.querySelector('.task-delete').getBoundingClientRect().height>=44`)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-edit`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] .task-id`)))
			do(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => { const form = document.querySelector('#page-editor form'); if (form.querySelector('[name="id"]')) throw new Error('id is editable'); form.elements.name.value = 'Renamed page'; form.requestSubmit(); })()`))
			poll(`document.querySelector('.detail-item[data-id="from-the-page"] .detail-name')?.textContent==='Renamed page' && !document.querySelector('#page-editor').open`)
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-delete`)), chromedp.Click(chromedp.CSS(`.task-delete`)))
			poll(`document.querySelectorAll('.detail-item').length===0 && document.querySelector('.detail-view').textContent.includes('tasks.example.json')`)
			p := filepath.Join(dir, "plugins", "scheduled-tasks")
			work := t.TempDir()
			config := map[string]any{"version": 1, "tasks": []any{map[string]any{"id": "local", "name": "Local task 中文", "enabled": true, "schedule": map[string]any{"every": "24h"}, "exec": map[string]any{"command": []string{binary, "--dir", work, "--addr", "127.0.0.1:0"}}, "timeout": "2m"}}}
			b, _ := json.Marshal(config)
			if err := os.WriteFile(filepath.Join(p, "tasks.json"), b, 0600); err != nil {
				t.Fatal(err)
			}
			cliCommand(t, 0, "plugin", "reload", "--url", url)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			poll(`document.querySelector('.detail-name')?.textContent==='Local task 中文' && document.querySelectorAll('.detail-actions button').length===4 && document.querySelector('.detail-actions button').textContent===` + fmt.Sprintf("%q", map[string]string{"en": "Run now", "zh-CN": "立即运行"}[lang]))
			for _, width := range []int64{1280, 520} {
				for _, theme := range []string{"light", "dark"} {
					do(t, ctx, chromedp.EmulateViewport(width, 800))
					if _, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: theme}}}); err != nil {
						t.Fatal(err)
					}
					poll(`document.documentElement.scrollWidth<=innerWidth && document.querySelector('.detail-view').scrollWidth<=document.querySelector('.detail-view').clientWidth+1 && [...document.querySelectorAll('.detail-actions button')].every(b=>b.getBoundingClientRect().height>=44)`)
				}
			}
			body := []byte(`{"item":"local","action":"run"}`)
			req, _ := http.NewRequest(http.MethodPost, url+"/api/action/scheduled-tasks", bytes.NewReader(body))
			req.Header.Set("Origin", "http://127.0.0.1:1")
			req.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("foreign action HTTP %d", response.StatusCode)
			}
			click := func(n int) {
				t.Helper()
				selector := fmt.Sprintf(".detail-actions button:nth-child(%d)", n)
				do(t, ctx, chromedp.Click(chromedp.CSS(selector)), chromedp.Click(chromedp.CSS(selector)))
			}
			click(2)
			poll(`document.querySelectorAll('.detail-actions button').length===3 && document.querySelector('.detail-actions button').textContent===` + fmt.Sprintf("%q", map[string]string{"en": "Resume", "zh-CN": "恢复"}[lang]))
			click(1)
			poll(`document.querySelectorAll('.detail-actions button').length===4 && !document.querySelector('.detail-actions button').disabled`)
			click(1)
			waitForTaskFile := func(status string) {
				t.Helper()
				until := time.Now().Add(15 * time.Second)
				for {
					b, err := os.ReadFile(filepath.Join(p, "data/state.json"))
					var s struct{ Runs []struct{ Status string } }
					if err == nil && json.Unmarshal(b, &s) == nil && len(s.Runs) > 0 && s.Runs[len(s.Runs)-1].Status == status {
						return
					}
					if time.Now().After(until) {
						t.Fatalf("wanted %s: %s %v", status, b, err)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			waitForTaskFile("running")
			if err := os.WriteFile(filepath.Join(p, "tasks.json"), []byte(`{"version":1,"tasks":[],"typo":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			cliCommand(t, 1, "plugin", "reload", "--url", url)
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			poll(`document.querySelectorAll('.detail-item').length===2 && document.querySelector('.detail-view').textContent.includes('typo')`)
			// Select the retained task by its observed name, independently of sorting.
			stop := `[...document.querySelectorAll('.detail-item')].find(r=>r.querySelector('.detail-name').textContent==='Local task 中文').querySelector('.detail-actions button')`
			for i := 0; i < 2; i++ {
				if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(`+stop+`.click(),true)`)); err != nil {
					t.Fatal(err)
				}
			}
			waitForTaskFile("cancelled")
			do(t, ctx, chromedp.Click(chromedp.CSS(`#service-reload`)))
			poll(`document.querySelector('.detail-log pre')?.textContent.includes('reason=stop')`)
		})
	}
}

// dialogFoot is why an open dialog fails to keep its foot inside the window.
// A tall form must scroll its body; a short one must not be stretched to fill.
func dialogFoot(mustScroll bool) string {
	scroll := "false"
	if mustScroll {
		scroll = "true"
	}
	return `(() => {
		const mustScroll = ` + scroll + `;
		const d = document.querySelector('#page-editor');
		if (!d || !d.open) return 'dialog is closed';
		const r = d.getBoundingClientRect();
		const n = (v) => Math.round(v);
		if (r.top < -0.5 || r.left < -0.5 || r.bottom > innerHeight + 0.5 || r.right > innerWidth + 0.5) {
			return 'dialog leaves the window: ' + n(r.left) + ',' + n(r.top) + ' ' + n(r.right) + ',' + n(r.bottom) + ' in ' + innerWidth + 'x' + innerHeight;
		}
		if (d.scrollWidth > d.clientWidth + 1) return 'dialog scrolls sideways by ' + (d.scrollWidth - d.clientWidth) + 'px';
		const body = d.querySelector('.editor-body');
		const actions = d.querySelector('.editor-actions');
		if (!body || !actions) return 'dialog is missing its body or foot';
		if (body.contains(actions)) return 'the foot scrolls with the body';
		if (body.scrollWidth > body.clientWidth + 1) return 'dialog body scrolls sideways by ' + (body.scrollWidth - body.clientWidth) + 'px';
		const buttons = [...actions.querySelectorAll('button')];
		if (buttons.length < 1) return 'the foot has no button';
		for (const b of buttons) {
			const br = b.getBoundingClientRect();
			const name = b.id || b.textContent;
			if (br.height < 44) return name + ' is ' + n(br.height) + 'px tall';
			if (br.top < r.top - 0.5 || br.bottom > r.bottom + 0.5 || br.left < r.left - 0.5 || br.right > r.right + 0.5 || br.bottom > innerHeight + 0.5 || br.top < -0.5) {
				return name + ' is outside the dialog or the window';
			}
		}
		const cron = d.querySelector('.task-cron');
		if (cron && !cron.hidden) {
			for (const input of cron.querySelectorAll('input')) {
				const ir = input.getBoundingClientRect();
				if (ir.width < 24 || ir.left < r.left - 0.5 || ir.right > r.right + 0.5) return 'a cron field is clipped';
			}
		}
		if (mustScroll && body.scrollHeight <= body.clientHeight + 1) return 'the body does not scroll, so a short window is not being tested';
		if (!mustScroll && body.clientHeight > body.scrollHeight + 8) return 'a short form is stretched to fill the dialog';
		return '';
	})()`
}

func TestAShortWindowKeepsDialogActionsInView(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.Mkdir(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Add(examples.Files, pluginDir, "scheduled-tasks"); err != nil {
		t.Fatal(err)
	}
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, page(t, servicePage)))
	url := startConsole(t, dir)
	widths := []int64{1280, 520}
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx := openBrowser(t, lang)
			do(t, ctx, chromedp.Navigate(url+"#/scheduled-tasks"), chromedp.WaitVisible(chromedp.CSS(`.task-add`)))
			check := func(width int64, opener, closer string, mustScroll bool) {
				t.Helper()
				do(t, ctx,
					chromedp.EmulateViewport(width, 640),
					chromedp.Click(chromedp.CSS(opener)),
					chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] .editor-actions button`)),
				)
				reason, err := chromedp.Run(ctx, chromedp.Evaluate[string](dialogFoot(mustScroll)))
				if err != nil || reason != "" {
					t.Fatalf("%s at %dpx: %s %v", opener, width, reason, err)
				}
				do(t, ctx, chromedp.Click(chromedp.CSS(closer)))
				if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('#page-editor').open`)); err != nil {
					t.Fatal(err)
				}
			}
			for _, width := range widths {
				check(width, `.task-add`, `#editor-cancel`, true)
			}
			do(t, ctx, chromedp.Click(chromedp.CSS(`.task-switch [data-segment="scripts"]`)), chromedp.WaitVisible(chromedp.CSS(`.task-script-add`)))
			for _, width := range widths {
				check(width, `.task-script-add`, `#editor-cancel`, false)
			}
			do(t, ctx, chromedp.Navigate(url+"#/svc"), chromedp.WaitVisible(chromedp.CSS(`#service-more .more-trigger`)))
			for _, width := range widths {
				do(t, ctx,
					chromedp.EmulateViewport(width, 640),
					chromedp.Click(chromedp.CSS(`#service-more .more-trigger`)),
					chromedp.Click(chromedp.CSS(`#service-notify`)),
					chromedp.WaitVisible(chromedp.CSS(`#notify-close`)),
				)
				reason, err := chromedp.Run(ctx, chromedp.Evaluate[string](dialogFoot(false)))
				if err != nil || reason != "" {
					t.Fatalf("notifications at %dpx: %s %v", width, reason, err)
				}
				do(t, ctx, chromedp.Click(chromedp.CSS(`#notify-close`)))
				if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`!document.querySelector('#page-editor').open`)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestTaskViewFollowsNativeCommandInAnyFolderAndLeavesGenericDetailsAlone(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.Mkdir(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	src, err := catalog.Add(examples.Files, pluginDir, "scheduled-tasks")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, filepath.Join(pluginDir, "nightly-jobs")); err != nil {
		t.Fatal(err)
	}
	writePlugin(t, dir, "scheduled-tasks", fmt.Sprintf(fakeDetailPlugin, exe))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	poll := func(js string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Poll[bool](js, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	do(t, ctx, chromedp.Navigate(url+"#/scheduled-tasks"), chromedp.WaitVisible(chromedp.CSS(`.detail-view:not([hidden]) .detail-item`)))
	poll(`document.querySelector('.detail-view:not([hidden]) .detail-name')?.textContent==='Service' && !document.querySelector('.detail-view:not([hidden]) .task-switch') && !document.querySelector('.detail-view:not([hidden]) .task-edit') && !document.querySelector('.detail-view:not([hidden]) .task-delete')`)

	do(t, ctx, chromedp.Navigate(url+"#/nightly-jobs"), chromedp.WaitVisible(chromedp.CSS(`.detail-view:not([hidden]) .task-switch`)))
	do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .task-switch [data-segment="scripts"]`)), chromedp.WaitVisible(chromedp.CSS(`.detail-view:not([hidden]) .task-script-add`)))
	do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .task-script-add`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] textarea`)))
	do(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => { const form = document.querySelector('#page-editor form'); form.elements.name.value = 'job.py'; form.elements.content.value = "print('ok')\\n"; form.requestSubmit(); })()`))
	poll(`!!document.querySelector('.detail-view:not([hidden]) .detail-item[data-script="job.py"]') && !document.querySelector('#page-editor').open`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .detail-item[data-script="job.py"] .script-delete`)), chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .detail-item[data-script="job.py"] .script-delete`)))
	poll(`!document.querySelector('.detail-view:not([hidden]) .detail-item[data-script="job.py"]') && !!document.querySelector('.detail-view:not([hidden]) .detail-list [data-empty="scripts"]')`)

	do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .task-switch [data-segment="tasks"]`)), chromedp.WaitVisible(chromedp.CSS(`.detail-view:not([hidden]) .task-add`)))
	do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .task-add`)), chromedp.WaitVisible(chromedp.CSS(`#page-editor[open] input`)))
	do(t, ctx, chromedp.Evaluate[chromedp.Void](`(() => { const form = document.querySelector('#page-editor form'); form.elements.name.value = 'Custom folder task'; form.querySelector('[data-schedule="every"]').click(); form.elements.every.value = '24h'; form.elements.command.value = 'echo hi'; form.requestSubmit(); })()`))
	poll(`document.querySelector('.detail-view:not([hidden]) .detail-item[data-id="custom-folder-task"] .detail-name')?.textContent==='Custom folder task' && !!document.querySelector('.detail-view:not([hidden]) .detail-item[data-id="custom-folder-task"] .task-edit') && !!document.querySelector('.detail-view:not([hidden]) .detail-item[data-id="custom-folder-task"] .task-delete')`)
	do(t, ctx, chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .detail-item[data-id="custom-folder-task"] .task-delete`)), chromedp.Click(chromedp.CSS(`.detail-view:not([hidden]) .detail-item[data-id="custom-folder-task"] .task-delete`)))
	poll(`document.querySelectorAll('.detail-view:not([hidden]) .detail-item').length===0 && document.querySelector('.detail-view:not([hidden])').textContent.includes('tasks.example.json')`)
}
