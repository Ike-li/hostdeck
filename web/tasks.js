import { $, el, announce, feedback } from './dom.js';
import { postJSON } from './api.js';
import { t, explain, pick } from './i18n.js';
import { twoClicks } from './confirm.js';
import { state } from './state.js';

// readJSON keeps the API error code. getJSON turns a failure into a plain Error.
async function readJSON(url) {
  const resp = await fetch(url, { cache: 'no-store' });
  const parsed = await resp.json().catch(() => ({}));
  if (!resp.ok) throw parsed.error || { code: '', detail: `HTTP ${resp.status}` };
  return parsed;
}

// scheduledTaskTools adds the Tasks | Scripts switch on the scheduled-tasks
// detail. Other detail pages are unchanged. Script rows are not task items.
export function scheduledTaskTools(p, view, tools, choice, onChange, reload) {
  if (p.id !== 'scheduled-tasks') return null;
  choice.segment = choice.segment || 'tasks';
  const bar = el('div', 'lang task-switch');
  bar.setAttribute('role', 'group');
  bar.setAttribute('aria-label', t('tasks.switch'));
  for (const [id, key] of [['tasks', 'tasks.tasks'], ['scripts', 'tasks.scripts']]) {
    const button = el('button', null, t(key));
    button.type = 'button';
    button.dataset.segment = id;
    button.addEventListener('click', () => {
      choice.segment = id;
      onChange();
    });
    bar.append(button);
  }
  const addTask = el('button', 'btn primary task-add', t('tasks.addTask'));
  addTask.type = 'button';
  addTask.addEventListener('click', () => openTaskEditor(view, null, addTask, reload));
  const add = el('button', 'btn primary task-script-add', t('tasks.addScript'));
  add.type = 'button';
  add.addEventListener('click', () => openScriptEditor(view, null, add, onChange));
  tools.prepend(bar, addTask, add);
  return {
    mark(segment) {
      for (const button of bar.children) button.setAttribute('aria-pressed', String(button.dataset.segment === segment));
      addTask.hidden = segment !== 'tasks';
      add.hidden = segment !== 'scripts';
    },
    paint() { return paintScripts(view, choice, onChange, reload); },
  };
}

// taskRowControls adds Edit and Delete beside a task's existing actions.
export function taskRowControls(item, view, reload) {
  const name = pick(item.name);
  const edit = el('button', 'btn task-edit', t('tasks.editScript'));
  edit.type = 'button';
  edit.addEventListener('click', async () => {
    edit.disabled = true;
    try {
      const body = await readJSON(`/api/plugins/scheduled-tasks/tasks/${encodeURIComponent(item.id)}`);
      openTaskEditor(view, body, edit, reload);
    } catch (err) {
      announce(err.code ? explain(err) : err.message);
    } finally {
      edit.disabled = false;
    }
  });
  const remove = el('button', 'btn task-delete', t('tasks.deleteScript'));
  remove.type = 'button';
  twoClicks(remove, t('tasks.confirmDelete', { name }), async () => {
    remove.disabled = true;
    try {
      await postJSON(`/api/plugins/scheduled-tasks/tasks/${encodeURIComponent(item.id)}/delete`);
      announce(t('tasks.taskDeleted', { name }));
      await reload();
    } catch (err) {
      announce(err.code ? explain(err) : err.message);
      remove.disabled = false;
    }
  });
  return [edit, remove];
}

async function paintScripts(view, choice, onChange, reload, quiet) {
  if (choice.segment !== 'scripts') return;
  if (view.querySelector('.detail-list .armed')) {
    watchScripts(view, () => paintScripts(view, choice, onChange, reload, true));
    return;
  }
  const token = (view.scriptToken || 0) + 1;
  view.scriptToken = token;
  const list = view.querySelector('.detail-list');
  view.querySelector('.detail-groups').hidden = true;
  if (!view.scriptFiles) {
    list.replaceChildren(el('p', 'empty', t('loading')));
    try {
      const body = await readJSON('/api/plugins/scheduled-tasks/scripts');
      if (view.scriptToken !== token || choice.segment !== 'scripts') return;
      view.scriptFiles = body.files || [];
    } catch (err) {
      if (view.scriptToken !== token || choice.segment !== 'scripts') return;
      const note = el('p', 'banner', err.code ? explain(err) : err.message);
      note.setAttribute('role', 'alert');
      list.replaceChildren(note);
      return;
    }
  }
  if (view.scriptToken !== token || choice.segment !== 'scripts') return;
  if (view.querySelector('.detail-list .armed')) {
    watchScripts(view, () => paintScripts(view, choice, onChange, reload, true));
    return;
  }
  const query = choice.search.trim().toLocaleLowerCase();
  const matched = view.scriptFiles.filter((file) => file.name.toLocaleLowerCase().includes(query));
  matched.sort((a, b) => a.name.localeCompare(b.name, state.lang, { sensitivity: 'base' }));
  if (!view.scriptFiles.length) {
    const empty = el('p', 'empty', t('tasks.scriptsEmpty'));
    empty.dataset.empty = 'scripts';
    list.replaceChildren(empty);
  }
  else if (!matched.length) list.replaceChildren(el('p', 'empty', t('detail.noMatches')));
  else list.replaceChildren(...matched.map((file) => scriptRow(view, file, onChange, reload)));
  if (!quiet && state.current?.id === 'scheduled-tasks') announce(t('detail.matches', { count: matched.length, total: view.scriptFiles.length }));
  watchScripts(view, () => paintScripts(view, choice, onChange, reload, true));
}

// watchScripts refreshes a running script without replacing a button that is
// waiting for its second click. A quiet refresh does not announce the count.
function watchScripts(view, refresh) {
  clearTimeout(view.scriptTimer);
  const live = (view.scriptFiles || []).some((file) => file.status === 'starting' || file.status === 'running');
  if (!live) return;
  const delay = view.querySelector('.detail-list .armed') ? 400 : 1000;
  view.scriptTimer = setTimeout(() => {
    if (view.querySelector('.detail-list .armed')) {
      watchScripts(view, refresh);
      return;
    }
    view.scriptFiles = null;
    refresh();
  }, delay);
}

function scriptRow(view, file, onChange, reload) {
  const section = el('section', 'detail-item');
  section.dataset.script = file.name;
  const head = el('header', 'detail-head');
  head.append(el('h2', 'detail-name', file.name));
  const actions = el('div', 'detail-actions');
  const edit = el('button', 'btn script-edit', t('tasks.editScript'));
  edit.type = 'button';
  edit.addEventListener('click', async () => {
    edit.disabled = true;
    try {
      const body = await readJSON(`/api/plugins/scheduled-tasks/scripts/${encodeURIComponent(file.name)}`);
      openScriptEditor(view, body, edit, onChange);
    } catch (err) {
      announce(err.code ? explain(err) : err.message);
    } finally {
      edit.disabled = false;
    }
  });
  const schedule = el('button', 'btn script-schedule', t('tasks.scheduleScript'));
  schedule.type = 'button';
  schedule.addEventListener('click', () => openTaskEditor(view, { name: file.name, command: file.command || '' }, schedule, reload));
  const remove = el('button', 'btn script-delete', t('tasks.deleteScript'));
  remove.type = 'button';
  twoClicks(remove, t('tasks.confirmDelete', { name: file.name }), async () => {
    remove.disabled = true;
    try {
      await postJSON(`/api/plugins/scheduled-tasks/scripts/${encodeURIComponent(file.name)}/delete`);
      view.scriptFiles = null;
      announce(t('tasks.scriptDeleted', { name: file.name }));
      onChange();
    } catch (err) {
      announce(err.code ? explain(err) : err.message);
      remove.disabled = false;
    }
  });
  actions.append(edit, schedule);
  if (file.runnable) {
    const run = el('button', 'btn script-run', t('tasks.runScript'));
    run.type = 'button';
    twoClicks(run, t('tasks.confirmRun', { name: file.name }), async () => {
      run.disabled = true;
      try {
        await postJSON(`/api/plugins/scheduled-tasks/scripts/${encodeURIComponent(file.name)}/run`);
        view.scriptFiles = null;
        announce(t('tasks.scriptStarted', { name: file.name }));
        onChange();
      } catch (err) {
        announce(err.code ? explain(err) : err.message);
        run.disabled = false;
      }
    });
    actions.append(run);
  }
  actions.append(remove);
  head.append(actions);
  section.append(head);
  if (file.result || file.status) section.append(el('p', 'script-result', pick(file.result)));
  if (file.log) {
    const log = el('details', 'detail-log');
    const content = el('pre', null, file.log);
    content.tabIndex = 0;
    content.setAttribute('aria-label', t('detail.log'));
    log.append(el('summary', null, t('detail.log')), content);
    section.append(log);
  }
  return section;
}

function labeled(name, label, value, multiline) {
  const wrap = el('label', 'edit-field');
  const control = el(multiline ? 'textarea' : 'input');
  control.name = name;
  control.value = value ?? '';
  control.autocomplete = 'off';
  control.spellcheck = false;
  wrap.append(el('span', null, label), control);
  return wrap;
}

// openScriptEditor uses the page's one dialog. A script saved here is not a task.
function openScriptEditor(view, file, opener, onChange) {
  const editor = $('page-editor');
  const creating = !file;
  const head = el('header', 'editor-head');
  const title = el('h2', null, creating ? t('tasks.addScript') : t('tasks.editTitle', { name: file.name }));
  title.id = 'editor-title';
  head.append(title);
  const form = el('form', 'page-edit-form');
  const fields = el('fieldset', 'editor-fields');
  const name = labeled('name', t('tasks.scriptName'), creating ? '' : file.name, false);
  const nameInput = name.querySelector('input');
  nameInput.required = true;
  nameInput.maxLength = 64;
  nameInput.disabled = !creating;
  const body = labeled('content', t('tasks.scriptBody'), creating ? '' : file.content, true);
  fields.append(name, body);
  const description = el('p', null, t('tasks.scriptHint'));
  description.id = 'editor-description';
  const note = el('p', 'catalog-note');
  note.id = 'editor-note';
  note.hidden = true;
  const footer = el('footer', 'editor-actions');
  const cancel = el('button', 'btn', t('page.cancel'));
  cancel.id = 'editor-cancel';
  cancel.type = 'button';
  cancel.addEventListener('click', () => editor.close());
  const save = el('button', 'btn primary', t('page.save'));
  save.type = 'submit';
  footer.append(cancel, save);
  const scroll = el('div', 'editor-body');
  scroll.append(description, fields, note);
  form.append(scroll, footer);
  let saved = false;
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (form.dataset.saving === 'true') return;
    const filename = nameInput.value.trim();
    if (creating && !filename) {
      nameInput.value = '';
      form.reportValidity();
      return;
    }
    form.dataset.saving = 'true';
    form.setAttribute('aria-busy', 'true');
    save.disabled = cancel.disabled = fields.disabled = true;
    feedback(note, t('page.saving'));
    try {
      if (creating) await postJSON('/api/plugins/scheduled-tasks/scripts', { name: filename, content: body.querySelector('textarea').value });
      else await postJSON(`/api/plugins/scheduled-tasks/scripts/${encodeURIComponent(file.name)}`, { content: body.querySelector('textarea').value });
      saved = true;
      view.scriptFiles = null;
      const kept = creating ? filename : file.name;
      announce(t('tasks.scriptSaved', { name: kept }));
      editor.close();
      await onChange();
      const back = view.querySelector(`.detail-item[data-script="${CSS.escape(kept)}"] .script-edit`);
      (back || opener)?.focus();
    } catch (err) {
      feedback(note, explain(err), true);
    } finally {
      form.dataset.saving = 'false';
      form.setAttribute('aria-busy', 'false');
      save.disabled = cancel.disabled = fields.disabled = false;
      if (editor.open && !form.contains(document.activeElement)) save.focus();
    }
  });
  editor.replaceChildren(head, form);
  editor.oncancel = (e) => { if (form.dataset.saving === 'true') e.preventDefault(); };
  editor.onclose = () => { if (!saved && opener?.isConnected) opener.focus(); };
  editor.showModal();
  (creating ? nameInput : body.querySelector('textarea')).focus();
}

const cronFields = [['minute', 'tasks.minute'], ['hour', 'tasks.hour'], ['day', 'tasks.day'], ['month', 'tasks.month'], ['weekday', 'tasks.weekday']];

// openTaskEditor writes tasks.json through the same dialog as a script.
// Saving refreshes the schedule; it does not run the command.
function openTaskEditor(view, task, opener, reload) {
  const editor = $('page-editor');
  const creating = !task?.id;
  const head = el('header', 'editor-head');
  const title = el('h2', null, creating ? t('tasks.addTask') : t('tasks.editTitle', { name: task.name }));
  title.id = 'editor-title';
  head.append(title);
  const form = el('form', 'page-edit-form');
  const fields = el('fieldset', 'editor-fields');
  const name = labeled('name', t('page.name'), task?.name || '', false);
  const nameInput = name.querySelector('input');
  nameInput.required = true;
  nameInput.maxLength = 200;
  const enabled = el('label', 'task-enabled');
  const box = el('input');
  box.type = 'checkbox';
  box.name = 'enabled';
  box.checked = creating ? true : !!task.enabled;
  enabled.append(box, el('span', null, t('tasks.enabled')));
  const kind = el('div', 'lang task-kind');
  kind.setAttribute('role', 'group');
  kind.setAttribute('aria-label', t('tasks.schedule'));
  let mode = !creating && task.schedule?.cron ? 'cron' : (creating ? 'cron' : 'every');
  const cron = el('div', 'task-cron');
  const parts = (task?.schedule?.cron || '').trim().split(/\s+/);
  cronFields.forEach(([key, label], i) => cron.append(labeled(key, t(label), creating ? '' : (parts[i] || ''), false)));
  const zoneValue = task?.schedule?.timezone || (state.lang === 'zh-CN' ? 'Asia/Shanghai' : 'UTC');
  const zone = labeled('timezone', t('tasks.timezone'), zoneValue, false);
  const every = labeled('every', t('tasks.every'), task?.schedule?.every || '', false);
  every.querySelector('input').placeholder = '24h';
  const timeout = labeled('timeout', t('tasks.timeout'), task?.timeout || '', false);
  timeout.querySelector('input').placeholder = '30m';
  const command = labeled('command', t('tasks.command'), task?.command || '', false);
  const commandInput = command.querySelector('input');
  if (task?.command_locked) commandInput.disabled = true;
  const show = (next) => {
    mode = next;
    cron.hidden = zone.hidden = mode !== 'cron';
    every.hidden = mode !== 'every';
    for (const button of kind.children) button.setAttribute('aria-pressed', String(button.dataset.schedule === mode));
  };
  for (const [id, key] of [['cron', 'tasks.cron'], ['every', 'tasks.interval']]) {
    const button = el('button', null, t(key));
    button.type = 'button';
    button.dataset.schedule = id;
    button.addEventListener('click', () => show(id));
    kind.append(button);
  }
  fields.append(name, enabled, kind, cron, zone, every, timeout, command);
  if (!creating) fields.append(el('p', 'page-hint task-id', t('tasks.id', { id: task.id })));
  const description = el('p', null, t('tasks.taskHint'));
  description.id = 'editor-description';
  const commandHint = el('p', 'page-hint', task?.command_locked ? t('tasks.commandLocked') : t('tasks.commandHint'));
  const note = el('p', 'catalog-note');
  note.id = 'editor-note';
  note.hidden = true;
  const footer = el('footer', 'editor-actions');
  const cancel = el('button', 'btn', t('page.cancel'));
  cancel.id = 'editor-cancel';
  cancel.type = 'button';
  cancel.addEventListener('click', () => editor.close());
  const save = el('button', 'btn primary', t('page.save'));
  save.type = 'submit';
  footer.append(cancel, save);
  const scroll = el('div', 'editor-body');
  scroll.append(description, fields, commandHint, note);
  form.append(scroll, footer);
  show(mode);
  let saved = false;
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (form.dataset.saving === 'true') return;
    if (!nameInput.value.trim()) {
      nameInput.value = '';
      form.reportValidity();
      return;
    }
    const payload = {
      name: nameInput.value.trim(),
      enabled: box.checked,
      timeout: timeout.querySelector('input').value.trim(),
      schedule: mode === 'every'
        ? { every: every.querySelector('input').value.trim() }
        : { cron: cronFields.map(([key]) => form.elements[key].value.trim()).join(' '), timezone: zone.querySelector('input').value.trim() },
    };
    if (!task?.command_locked) payload.command = commandInput.value;
    form.dataset.saving = 'true';
    form.setAttribute('aria-busy', 'true');
    save.disabled = cancel.disabled = fields.disabled = true;
    feedback(note, t('page.saving'));
    try {
      const body = creating
        ? await postJSON('/api/plugins/scheduled-tasks/tasks', payload)
        : await postJSON(`/api/plugins/scheduled-tasks/tasks/${encodeURIComponent(task.id)}`, payload);
      saved = true;
      const kept = creating ? body.id : task.id;
      const keptName = payload.name;
      announce(t('tasks.taskSaved', { name: keptName }));
      editor.close();
      await reload();
      const back = view.querySelector(`.detail-item[data-id="${CSS.escape(kept)}"] .task-edit`);
      (back || opener)?.focus();
    } catch (err) {
      feedback(note, explain(err), true);
    } finally {
      form.dataset.saving = 'false';
      form.setAttribute('aria-busy', 'false');
      save.disabled = cancel.disabled = fields.disabled = false;
      if (editor.open && !form.contains(document.activeElement)) save.focus();
    }
  });
  editor.replaceChildren(head, form);
  editor.oncancel = (e) => { if (form.dataset.saving === 'true') e.preventDefault(); };
  editor.onclose = () => { if (!saved && opener?.isConnected) opener.focus(); };
  editor.showModal();
  nameInput.focus();
}
