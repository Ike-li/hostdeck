const LANGS = ['en', 'zh-CN'];
const LANG_KEY = 'hostdeck.lang';
const STATUS_EVERY = 15_000;
const IDLE_RELEASE = 10 * 60_000; // a page switched away from this long is unloaded, and loads again on its next visit

const $ = (id) => document.getElementById(id);

function el(tag, className, text) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined && text !== null) e.textContent = text;
  return e;
}

let lang = 'en';
let dict = {};
let plugins = []; // the sidebar entries
let status = {}; // plugin id -> up | down
let current = null; // the open sidebar entry, or null on the overview
let overview = null; // the last answer of /api/cards
let failure = null; // why the backend could not be read
const frames = new Map(); // plugin id -> iframe, kept so each page keeps its state
const hiddenSince = new Map(); // plugin id -> when its page was switched away from

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

// An error from the backend is worded here from its code; an unknown code
// shows the backend's own detail.
function explain(error) {
  const key = `error.${error.code}`;
  return key in dict ? t(key, { detail: error.detail }) : error.detail;
}

async function getJSON(url) {
  const resp = await fetch(url, { cache: 'no-store' });
  if (!resp.ok) throw new Error(`${url}: HTTP ${resp.status}`);
  return resp.json();
}

async function setLang(next, remember) {
  dict = await getJSON(`/i18n/${next}.json`);
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

// The address names the open entry: #/<id>. Anything else, a malformed
// address included, is the overview.
function route() {
  let id = '';
  try {
    id = decodeURIComponent(location.hash.replace(/^#\/?/, ''));
  } catch {
    // a malformed escape: show the overview
  }
  current = plugins.find((p) => p.id === id && p.type === 'proxy') || null;
  for (const frameId of frames.keys()) {
    if (current && frameId === current.id) hiddenSince.delete(frameId);
    else if (!hiddenSince.has(frameId)) hiddenSince.set(frameId, Date.now());
  }
  if (!current) loadCards();
  render();
}

function render() {
  for (const e of document.querySelectorAll('[data-i18n]')) e.textContent = t(e.dataset.i18n);
  for (const e of document.querySelectorAll('[data-i18n-label]')) e.setAttribute('aria-label', t(e.dataset.i18nLabel));
  for (const b of document.querySelectorAll('[data-lang]')) b.setAttribute('aria-pressed', String(b.dataset.lang === lang));

  $('banner').hidden = !failure;
  if (failure) $('banner').textContent = t('error.backend', { detail: failure });

  renderNav();
  $('overview').hidden = !!current;
  $('service').hidden = !current;
  if (current) renderService(current);
  else renderOverview();
}

function renderNav() {
  const nav = $('nav');
  const overviewItem = el('a', 'nav-item', t('nav.overview'));
  overviewItem.href = '#/';
  overviewItem.classList.toggle('active', !current);
  nav.replaceChildren(overviewItem, ...plugins.map(navItem));
}

function navItem(p) {
  const a = el('a', 'nav-item');
  a.dataset.id = p.id;
  a.append(el('span', 'icon', iconOf(p)), el('span', 'nav-name', pick(p.name)));
  if (p.type === 'link') {
    a.href = p.url;
    a.target = '_blank';
    a.rel = 'noopener';
    a.append(el('span', 'nav-ext', '↗'));
  } else {
    a.href = `#/${encodeURIComponent(p.id)}`;
    a.classList.toggle('active', current?.id === p.id);
    a.append(dot(p));
  }
  return a;
}

const iconOf = (p) => p.icon || pick(p.name).slice(0, 1);

function dot(p) {
  const d = el('span', 'dot');
  const s = p.error ? 'down' : status[p.id];
  if (s) {
    d.dataset.status = s;
    d.title = t(`status.${s}`);
  }
  return d;
}

function renderOverview() {
  document.title = 'hostdeck';
  const cards = overview ? overview.cards : [];
  $('empty').hidden = !overview || cards.length > 0;
  if (overview) $('empty').textContent = t('overview.empty', { dir: overview.pluginDir });
  $('cards').replaceChildren(...cards.map(cardElement));
}

function cardElement(c) {
  const card = el('article', 'card');
  card.dataset.id = c.id;
  const head = el('header', 'card-head');
  head.append(el('span', 'icon', c.icon || pick(c.name).slice(0, 1)), el('h2', 'card-name', pick(c.name)));
  card.append(head);
  if (c.error) {
    card.dataset.status = 'error';
    card.append(el('p', 'card-error', explain(c.error)));
    return card;
  }
  const big = el('div', 'card-big');
  big.append(el('span', 'card-value', c.value ?? ''), el('span', 'card-label', pick(c.label)));
  card.append(big);
  return card;
}

function renderService(p) {
  document.title = `${pick(p.name)} · hostdeck`;
  $('service-icon').textContent = iconOf(p);
  $('service-name').textContent = pick(p.name);
  $('service-dot').replaceWith(Object.assign(dot(p), { id: 'service-dot' }));
  $('service-error').hidden = !p.error;
  $('service-reload').hidden = $('service-open').hidden = !!p.error;
  if (p.error) {
    $('service-error').textContent = explain(p.error);
  } else {
    $('service-open').href = p.url;
    if (!frames.has(p.id)) {
      const frame = el('iframe', 'frame');
      frame.title = pick(p.name);
      frame.allow = 'clipboard-read; clipboard-write; fullscreen';
      frame.src = p.url;
      $('frames').append(frame);
      frames.set(p.id, frame);
    }
  }
  for (const [id, f] of frames) f.hidden = id !== p.id || !!p.error;
}

// Removing an iframe frees its memory and closes its connections; the next
// visit builds a fresh one.
function releaseIdleFrames() {
  for (const [id, since] of hiddenSince) {
    if (Date.now() - since < IDLE_RELEASE) continue;
    frames.get(id).remove();
    frames.delete(id);
    hiddenSince.delete(id);
  }
}

async function loadCards() {
  try {
    overview = await getJSON('/api/cards');
    failure = null;
  } catch (e) {
    failure = e.message;
  }
  render();
}

async function refreshStatus() {
  if (document.hidden) return;
  try {
    status = await getJSON('/api/status');
  } catch {
    return; // the banner of the next failed request says why
  }
  render();
}

async function init() {
  for (const b of document.querySelectorAll('[data-lang]')) {
    b.addEventListener('click', () => setLang(b.dataset.lang, true));
  }
  $('service-reload').addEventListener('click', () => {
    if (current && frames.has(current.id)) frames.get(current.id).src = current.url;
  });
  await setLang(initialLang(), false);
  try {
    plugins = await getJSON('/api/plugins');
  } catch (e) {
    failure = e.message;
  }
  addEventListener('hashchange', route);
  route();
  refreshStatus();
  setInterval(refreshStatus, STATUS_EVERY);
  setInterval(releaseIdleFrames, 60_000);
  document.addEventListener('visibilitychange', refreshStatus);
}

await init();
