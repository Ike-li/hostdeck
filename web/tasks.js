import { $, el, announce, feedback } from './dom.js';
import { postJSON } from './api.js';
import { t, explain } from './i18n.js';
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
export function scheduledTaskTools(p, view, tools, choice, onChange) {
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
  const add = el('button', 'btn primary task-script-add', t('tasks.addScript'));
  add.type = 'button';
  add.addEventListener('click', () => openScriptEditor(view, null, add, onChange));
  tools.prepend(bar, add);
  return {
    mark(segment) {
      for (const button of bar.children) button.setAttribute('aria-pressed', String(button.dataset.segment === segment));
      add.hidden = segment !== 'scripts';
    },
    paint() { return paintScripts(view, choice, onChange); },
  };
}

async function paintScripts(view, choice, onChange) {
  const token = (view.scriptToken || 0) + 1;
  view.scriptToken = token;
  const list = view.querySelector('.detail-list');
  view.querySelector('.detail-groups').hidden = true;
  if (!view.scriptFiles) {
    list.replaceChildren(el('p', 'empty', t('loading')));
    try {
      const body = await readJSON('/api/plugins/scheduled-tasks/scripts');
      if (view.scriptToken !== token) return;
      view.scriptFiles = body.files || [];
    } catch (err) {
      if (view.scriptToken !== token) return;
      const note = el('p', 'banner', err.code ? explain(err) : err.message);
      note.setAttribute('role', 'alert');
      list.replaceChildren(note);
      return;
    }
  }
  if (view.scriptToken !== token) return;
  const query = choice.search.trim().toLocaleLowerCase();
  const matched = view.scriptFiles.filter((file) => file.name.toLocaleLowerCase().includes(query));
  matched.sort((a, b) => a.name.localeCompare(b.name, state.lang, { sensitivity: 'base' }));
  if (!view.scriptFiles.length) {
    const empty = el('p', 'empty', t('tasks.scriptsEmpty'));
    empty.dataset.empty = 'scripts';
    list.replaceChildren(empty);
  }
  else if (!matched.length) list.replaceChildren(el('p', 'empty', t('detail.noMatches')));
  else list.replaceChildren(...matched.map((file) => scriptRow(view, file, onChange)));
  if (state.current?.id === 'scheduled-tasks') announce(t('detail.matches', { count: matched.length, total: view.scriptFiles.length }));
}

function scriptRow(view, file, onChange) {
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
  actions.append(edit, remove);
  head.append(actions);
  section.append(head);
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
