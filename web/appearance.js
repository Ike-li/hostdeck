import { $, el, feedback, announce } from './dom.js';
import { getJSON, postJSON } from './api.js';
import { t, explain } from './i18n.js';
import { closePanel } from './panel.js';
import { narrow, setNavigation } from './nav.js';
import { state, STATUS_EVERY } from './state.js';

const defaults = () => ({ version: 1, preset: 'default', mode: 'system' });
const apply = (settings) => document.dispatchEvent(new CustomEvent('hostdeck:theme', { detail: settings }));

export function renderAppearanceStatus() {
  const warning = state.appearance?.warning;
  $('appearance-warning').hidden = !warning;
  $('appearance-open').setAttribute('aria-label', t(warning ? 'appearance.warningLabel' : 'appearance.title'));
  $('appearance-open').title = warning ? explain(warning) : t('appearance.title');
}

export function wireAppearance() {
  try { state.appearance = JSON.parse(document.documentElement.dataset.appearance); } catch { /* standalone fixture */ }
  state.appearance ||= { appearance: defaults() };
  $('appearance-open').addEventListener('click', openAppearance);
  setInterval(refreshAppearance, STATUS_EVERY);
  addEventListener('focus', refreshAppearance);
  document.addEventListener('visibilitychange', refreshAppearance);
  document.addEventListener('hostdeck:theme-applied', showActualColour);
}

// A read started before a save is never allowed to undo that save. Only one
// read is in flight; background tabs do no work and a draft owns its preview.
export async function refreshAppearance() {
  if (document.hidden || state.appearanceReading || state.appearanceSaving) return;
  state.appearanceReading = true;
  const epoch = state.appearanceEpoch;
  try {
    const view = await getJSON('/api/appearance');
    if (epoch !== state.appearanceEpoch) return;
    state.appearance = view;
    renderAppearanceStatus();
    if (!state.appearanceDraft) apply(view.appearance);
  } catch {
    // A temporary read failure retains the last saved appearance and draft.
  } finally {
    state.appearanceReading = false;
  }
}

function selectField(id, label, choices) {
  const wrap = el('label', 'edit-field');
  const select = el('select');
  select.id = id;
  for (const [value, key] of choices) {
    const option = el('option', null, t(key));
    option.value = value;
    select.append(option);
  }
  wrap.append(el('span', null, t(label)), select);
  return wrap;
}

function showActualColour() {
  const output = $('appearance-actual');
  if (!output || !state.appearanceDraft) return;
  const colour = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();
  output.textContent = t(state.appearanceDraft.accent ? 'appearance.adapted' : 'appearance.presetAccent', { colour });
}

export function openAppearance() {
  const editor = $('page-editor');
  if (editor.open) return;
  closePanel();
  setNavigation(false);
  const draft = { ...state.appearance.appearance };
  state.appearanceDraft = draft;
  const head = el('header', 'editor-head');
  const title = el('h2', null, t('appearance.title'));
  title.id = 'editor-title';
  head.append(title);
  const form = el('form', 'page-edit-form');
  const body = el('div', 'editor-body');
  const description = el('p', null, t('appearance.description'));
  description.id = 'editor-description';
  const fields = el('fieldset', 'editor-fields');
  fields.append(
    selectField('appearance-mode', 'appearance.mode', [['system', 'appearance.system'], ['light', 'appearance.light'], ['dark', 'appearance.dark']]),
    selectField('appearance-preset', 'appearance.preset', ['default', 'forest', 'dusk', 'sand'].map((name) => [name, `appearance.${name}`])),
  );
  const colours = el('div', 'appearance-colours');
  const picker = el('input');
  picker.type = 'color';
  picker.id = 'appearance-picker';
  picker.setAttribute('aria-label', t('appearance.pickColour'));
  const label = el('label', 'edit-field');
  const accent = el('input');
  accent.id = 'appearance-accent';
  accent.type = 'text';
  accent.required = true;
  accent.pattern = '#[0-9a-fA-F]{6}';
  accent.maxLength = 7;
  accent.autocomplete = 'off';
  accent.spellcheck = false;
  accent.setAttribute('aria-describedby', 'appearance-actual');
  label.append(el('span', null, t('appearance.accent')), accent);
  colours.append(label, picker);
  const usePreset = el('button', 'btn', t('appearance.usePreset'));
  usePreset.type = 'button';
  usePreset.id = 'appearance-use-preset';
  const actual = el('p', 'page-hint');
  actual.id = 'appearance-actual';
  const preview = el('div', 'appearance-preview');
  preview.append(el('strong', null, t('appearance.preview')), el('span', 'appearance-sample', t('appearance.sample')), el('p', 'page-hint', t('appearance.previewHint')));
  fields.append(colours, usePreset, actual, preview);
  const note = el('p', 'catalog-note');
  note.id = 'appearance-note';
  note.hidden = true;
  if (state.appearance.warning) feedback(note, explain(state.appearance.warning), true);
  const footer = el('footer', 'editor-actions');
  const reset = el('button', 'btn', t('appearance.reset'));
  reset.id = 'appearance-reset';
  reset.type = 'button';
  const cancel = el('button', 'btn', t('page.cancel'));
  cancel.id = 'appearance-cancel';
  cancel.type = 'button';
  const save = el('button', 'btn primary', t('page.save'));
  save.id = 'appearance-save';
  save.type = 'submit';
  footer.append(reset, cancel, save);
  body.append(description, fields, note);
  form.append(body, footer);
  editor.replaceChildren(head, form);

  const sync = () => {
    $('appearance-mode').value = draft.mode;
    $('appearance-preset').value = draft.preset;
    apply(draft);
    accent.value = draft.accent || getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();
    accent.setCustomValidity('');
    picker.value = accent.value;
    usePreset.disabled = !draft.accent;
    showActualColour();
  };
  for (const [id, key] of [['appearance-mode', 'mode'], ['appearance-preset', 'preset']]) {
    $(id).addEventListener('change', (e) => { draft[key] = e.target.value; sync(); });
  }
  accent.addEventListener('input', () => {
    const valid = /^#[\da-f]{6}$/i.test(accent.value);
    accent.setCustomValidity(valid ? '' : t('appearance.invalidColour'));
    if (valid) {
      draft.accent = accent.value.toLowerCase();
      picker.value = draft.accent;
      usePreset.disabled = false;
      apply(draft);
      showActualColour();
    }
  });
  picker.addEventListener('input', () => { draft.accent = picker.value; sync(); });
  usePreset.addEventListener('click', () => { delete draft.accent; sync(); });
  reset.addEventListener('click', () => {
    for (const key of Object.keys(draft)) delete draft[key];
    Object.assign(draft, defaults());
    sync();
  });
  cancel.addEventListener('click', () => editor.close());
  editor.oncancel = (e) => { if (state.appearanceSaving) e.preventDefault(); };
  editor.onclose = () => {
    state.appearanceDraft = null;
    apply(state.appearance.appearance);
    (narrow.matches ? $('menu') : $('appearance-open')).focus();
  };
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (state.appearanceSaving || !form.reportValidity()) return;
    state.appearanceSaving = true;
    state.appearanceEpoch++;
    fields.disabled = reset.disabled = cancel.disabled = save.disabled = true;
    feedback(note, t('page.saving'));
    try {
      state.appearance = await postJSON('/api/appearance', draft);
      renderAppearanceStatus();
      announce(t('appearance.saved'));
      if (editor.contains(form)) editor.close();
    } catch (err) {
      feedback(note, explain(err), true);
    } finally {
      state.appearanceSaving = false;
      fields.disabled = reset.disabled = cancel.disabled = save.disabled = false;
    }
  });
  sync();
  editor.showModal();
  $('appearance-mode').focus();
}
