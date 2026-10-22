const LANGS = ['en', 'zh-CN'];
const LANG_KEY = 'hostdeck.lang';

const $ = (id) => document.getElementById(id);

function el(tag, className, text) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined && text !== null) e.textContent = text;
  return e;
}

let lang = 'en';
let dict = {};
let overview = null; // the last answer of /api/cards
let failure = null; // why /api/cards could not be read

// A choice made on this page wins; otherwise the first of the browser's
// languages that the console has.
function initialLang() {
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

function t(key, params = {}) {
  return (dict[key] ?? key).replace(/\{(\w+)\}/g, (_, name) => params[name] ?? '');
}

// Text from a plugin is a string, or one string per language.
function pick(text) {
  if (text == null) return '';
  if (typeof text === 'string') return text;
  return text[lang] ?? text.en ?? Object.values(text)[0] ?? '';
}

async function setLang(next, remember) {
  const resp = await fetch(`/i18n/${next}.json`);
  dict = await resp.json();
  lang = next;
  document.documentElement.lang = next;
  if (remember) {
    try {
      localStorage.setItem(LANG_KEY, next);
    } catch {
      // the choice then lasts until the page is reloaded
    }
  }
  render();
}

function render() {
  for (const e of document.querySelectorAll('[data-i18n]')) e.textContent = t(e.dataset.i18n);
  for (const e of document.querySelectorAll('[data-i18n-label]')) e.setAttribute('aria-label', t(e.dataset.i18nLabel));
  for (const b of document.querySelectorAll('[data-lang]')) b.setAttribute('aria-pressed', String(b.dataset.lang === lang));

  $('banner').hidden = !failure;
  if (failure) $('banner').textContent = t('error.backend', { detail: failure });

  const cards = overview ? overview.cards : [];
  $('empty').hidden = !overview || cards.length > 0;
  if (overview) $('empty').textContent = t('overview.empty', { dir: overview.pluginDir });
  $('cards').replaceChildren(...cards.map(cardElement));
}

function cardElement(c) {
  const card = el('article', 'card');
  card.dataset.id = c.id;
  const head = el('header', 'card-head');
  head.append(el('span', 'card-icon', c.icon || pick(c.name).slice(0, 1)), el('h2', 'card-name', pick(c.name)));
  card.append(head);
  if (c.error) {
    card.dataset.status = 'error';
    const key = `error.${c.error.code}`;
    card.append(el('p', 'card-error', key in dict ? t(key, { detail: c.error.detail }) : c.error.detail));
    return card;
  }
  const big = el('div', 'card-big');
  big.append(el('span', 'card-value', c.value ?? ''), el('span', 'card-label', pick(c.label)));
  card.append(big);
  return card;
}

async function loadCards() {
  try {
    const resp = await fetch('/api/cards', { cache: 'no-store' });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    overview = await resp.json();
    failure = null;
  } catch (e) {
    failure = e.message;
  }
  render();
}

for (const b of document.querySelectorAll('[data-lang]')) {
  b.addEventListener('click', () => setLang(b.dataset.lang, true));
}
await setLang(initialLang(), false);
await loadCards();
