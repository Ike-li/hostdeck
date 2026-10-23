import { $, el, feedback } from './dom.js';
import { postJSON } from './api.js';
import { t, pick, explain } from './i18n.js';
import { closePanel } from './panel.js';
import { loadPlugins, refreshStatus } from './plugins.js';
import { callRender, callLoadCatalog } from './hooks.js';
import { state } from './state.js';

// labeled is one labelled field of the page editor.
export function labeled(name, label, value) {
  const wrap = el('label', 'edit-field');
  const input = el('input');
  input.name = name;
  input.value = value;
  input.autocomplete = 'off';
  input.spellcheck = false;
  wrap.append(el('span', null, label), input);
  return wrap;
}

// pageEdit opens one focused editor without expanding the management row.
export function pageEdit(p) {
  const button = el('button', 'btn page-edit-open', t('page.edit'));
  button.type = 'button';
  button.setAttribute('aria-haspopup', 'dialog');
  button.setAttribute('aria-label', t('page.editTitle', { name: pick(p.name) }));
  button.addEventListener('click', () => openPageEditor(p));
  return button;
}

// The editor only exposes the existing allowlisted fields, never commands.
export function openPageEditor(p) {
  closePanel();
  const editor = $('page-editor');
  const head = el('header', 'editor-head');
  const title = el('h2', null, t('page.editTitle', { name: pick(p.name) }));
  title.id = 'editor-title';
  title.title = title.textContent;
  const description = el('p', null, pick(p.description) || t('page.editHint'));
  description.id = 'editor-description';
  head.append(title);
  const form = el('form', 'page-edit-form');
  const fields = el('fieldset', 'editor-fields');
  fields.append(labeled('name', t('page.name'), pick(p.name)), labeled('icon', t('page.icon'), p.icon || ''));
  fields.querySelector('[name="name"]').required = true;
  if (p.page) {
    const kind = el('select');
    kind.name = 'kind';
    kind.setAttribute('aria-describedby', 'editor-mode-hint');
    for (const [value, key] of [['proxy', 'page.embed'], ['link', 'page.link']]) {
      const option = el('option', null, t(key));
      option.value = value;
      kind.append(option);
    }
    kind.value = p.link ? 'link' : 'proxy';
    const address = labeled('address', t('page.address'), p.proxy || p.link || '');
    address.querySelector('input').required = true;
    address.querySelector('input').inputMode = 'url';
    const mode = el('label', 'edit-field');
    mode.append(el('span', null, t('page.mode')), kind);
    const hint = el('p', 'page-hint');
    hint.id = 'editor-mode-hint';
    const updateHint = () => { hint.textContent = t(kind.value === 'link' ? 'catalog.page.linkHint' : 'catalog.page.embedHint'); };
    kind.addEventListener('change', updateHint);
    updateHint();
    fields.append(mode, address, hint);
  }
  if (p.cardUrl) fields.append(labeled('cardUrl', t('page.card'), p.cardUrl));
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
  const body = el('div', 'editor-body');
  body.append(description, fields, note);
  form.append(body, footer);
  form.addEventListener('submit', (e) => savePage(e, p.id));
  editor.replaceChildren(head, form);
  editor.oncancel = (e) => { if (form.dataset.saving === 'true') e.preventDefault(); };
  editor.onclose = () => {
    const trigger = document.querySelector(`#catalog-installed .catalog-item[data-id="${CSS.escape(p.id)}"] .page-edit-open`);
    if (state.catalogOpen && trigger?.checkVisibility()) trigger.focus();
  };
  editor.showModal();
  form.elements.name.focus();
}

// Failed saves retain the draft and explain the error within the editor.
// Successful saves close it and announce the result on the updated row.
export async function savePage(e, id) {
  e.preventDefault();
  const form = e.target;
  if (form.dataset.saving === 'true') return;
  const name = form.elements.name.value.trim();
  if (!name) {
    form.elements.name.value = '';
    form.reportValidity();
    return;
  }
  const body = { name, icon: form.elements.icon.value };
  if (form.elements.kind) body[form.elements.kind.value] = form.elements.address.value.trim();
  if (form.elements.cardUrl) body.cardUrl = form.elements.cardUrl.value.trim();
  const btn = form.querySelector('[type="submit"]');
  const cancel = form.querySelector('#editor-cancel');
  const fields = form.querySelector('fieldset');
  const note = form.querySelector('#editor-note');
  form.dataset.saving = 'true';
  form.setAttribute('aria-busy', 'true');
  btn.disabled = cancel.disabled = fields.disabled = true;
  feedback(note, t('page.saving'));
  try {
    await postJSON(`/api/plugins/${encodeURIComponent(id)}/page`, body);
    state.catalogNoteFor = id;
    feedback($('catalog-note'), t('page.saved'));
    await loadPlugins();
    callRender();
    refreshStatus();
    await callLoadCatalog();
    $('page-editor').close();
  } catch (err) {
    feedback(note, explain(err), true);
  } finally {
    form.dataset.saving = 'false';
    form.setAttribute('aria-busy', 'false');
    btn.disabled = cancel.disabled = fields.disabled = false;
    if ($('page-editor').open && !form.contains(document.activeElement)) btn.focus();
  }
}
