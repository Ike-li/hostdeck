import { $ } from './dom.js';
import { getJSON } from './api.js';
import { closePanel } from './panel.js';
import { setNavigation } from './nav.js';
import { callRender } from './hooks.js';
import { LANGS, LANG_KEY, state } from './state.js';

// A choice made on this page wins; otherwise the first of the browser's
// languages that the console has.
export function initialLang() {
  try {
    const saved = localStorage.getItem(LANG_KEY);
    if (LANGS.includes(saved)) return saved;
  } catch {
    // storage can be blocked; fall back to the browser's languages
  }
  for (const l of navigator.languages || [navigator.language]) {
    const lower = l.toLowerCase();
    if (lower.startsWith('zh')) return 'zh-CN';
    if (lower.startsWith('en')) return 'en';
  }
  return 'en';
}

export function t(key, params = {}) {
  return (state.dict[key] ?? key).replace(/\{(\w+)\}/g, (_, name) => params[name] ?? '');
}

// Text from a plugin is a string, or one string per language.
export function pick(text) {
  if (text == null) return '';
  if (typeof text === 'string') return text;
  return text[state.lang] ?? text.en ?? Object.values(text)[0] ?? '';
}

// An error from the backend is worded here from its code; an unknown code
// shows the backend's own detail, or the code when there is none.
export function explain(error) {
  if (error.code === 'reported') return pick(error.text);
  const key = `error.${error.code}`;
  return key in state.dict ? t(key, { detail: error.detail }) : error.detail ?? error.code;
}

export async function setLang(next, remember) {
  if ($('page-editor').open) $('page-editor').close();
  closePanel();
  if (remember) setNavigation(false, true);
  state.dict = await getJSON(`/i18n/${next}.json`);
  state.lang = next;
  document.documentElement.lang = next;
  if (remember) {
    try {
      localStorage.setItem(LANG_KEY, next);
    } catch {
      // the choice then lasts until the page is reloaded
    }
  }
  callRender();
}

// The static shell is worded from data-i18n attributes; nodes the scripts
// create word themselves with t() as they are built.
export function applyI18n() {
  for (const e of document.querySelectorAll('[data-i18n]')) e.textContent = t(e.dataset.i18n);
  for (const e of document.querySelectorAll('[data-i18n-label]')) e.setAttribute('aria-label', t(e.dataset.i18nLabel));
  for (const e of document.querySelectorAll('[data-i18n-placeholder]')) e.placeholder = t(e.dataset.i18nPlaceholder);
  for (const b of document.querySelectorAll('[data-lang]')) b.setAttribute('aria-pressed', String(b.dataset.lang === state.lang));
}

// setStaticText moves a static node to another key and words it in one pass,
// so a label change cannot leave the text and the key disagreeing.
export function setStaticText(node, key) {
  node.dataset.i18n = key;
  applyI18n();
}
