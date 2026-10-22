const LANGS = ['en', 'zh-CN'];
const LANG_KEY = 'hostdeck.lang';
const STATUS_EVERY = 15_000;
const IDLE_RELEASE = 10 * 60_000; // a page switched away from this long is unloaded, and loads again on its next visit
const ARMED_FOR = 4000; // how long a button that needs a second click waits for it

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
let clearArmed = null; // the timer while the clear button waits for its second click
let clearing = false;
let clearFailure = null; // why the last clearing failed
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
  disarmClear();
  clearFailure = null;
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

// The sidebar is built once and then updated in place: rebuilding it on every
// status refresh would drop a click that lands while it is replaced.
function renderNav() {
  const nav = $('nav');
  if (nav.children.length !== plugins.length + 1) {
    const overviewItem = el('a', 'nav-item');
    overviewItem.href = '#/';
    overviewItem.append(el('span', 'nav-name'));
    nav.replaceChildren(overviewItem, ...plugins.map(navItem));
  }
  const [overviewItem, ...items] = nav.children;
  overviewItem.querySelector('.nav-name').textContent = t('nav.overview');
  overviewItem.classList.toggle('active', !current);
  items.forEach((a, i) => {
    const p = plugins[i];
    a.querySelector('.nav-name').textContent = pick(p.name);
    a.querySelector('.icon').textContent = iconOf(p);
    a.classList.toggle('active', current?.id === p.id);
    const d = a.querySelector('.dot');
    if (d) paintDot(d, p);
  });
}

function navItem(p) {
  const a = el('a', 'nav-item');
  a.dataset.id = p.id;
  a.append(el('span', 'icon'), el('span', 'nav-name'));
  if (p.type === 'link') {
    a.href = p.url;
    a.target = '_blank';
    a.rel = 'noopener';
    a.append(el('span', 'nav-ext', '↗'));
  } else {
    a.href = `#/${encodeURIComponent(p.id)}`;
    a.append(el('span', 'dot'));
  }
  return a;
}

const iconOf = (p) => p.icon || pick(p.name).slice(0, 1);

function paintDot(d, p) {
  const s = p.error ? 'down' : status[p.id];
  if (s) d.dataset.status = s;
  else delete d.dataset.status;
  d.title = s ? t(`status.${s}`) : '';
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
  paintDot($('service-dot'), p);
  $('service-error').hidden = !p.error;
  $('service-reload').hidden = $('service-open').hidden = $('service-clear').hidden = !!p.error;
  const clear = $('service-clear');
  clear.textContent = t(clearing ? 'service.clearing' : clearArmed ? 'service.clear.confirm' : 'service.clear');
  clear.classList.toggle('armed', !!clearArmed);
  clear.disabled = clearing;
  const note = $('clear-note');
  note.hidden = !clearArmed && !clearFailure;
  note.textContent = clearArmed ? t('service.clear.explain') : clearFailure ? t('error.clear_failed', { detail: clearFailure }) : '';
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

// Clearing needs two clicks: the first arms the button and says what clearing
// does. A double-click is one gesture, so its second click does not confirm.
function onClearClick(e) {
  if (e.detail > 1 || clearing || !current) return;
  if (!clearArmed) {
    clearFailure = null;
    clearArmed = setTimeout(() => {
      disarmClear();
      render();
    }, ARMED_FOR);
    render();
    return;
  }
  disarmClear();
  clearService(current);
}

function disarmClear() {
  clearTimeout(clearArmed);
  clearArmed = null;
}

const navigated = (frame, src) =>
  new Promise((resolve) => {
    frame.addEventListener('load', resolve, { once: true });
    frame.src = src;
  });

// The browser clears the origin whose answer carries Clear-Site-Data, and
// only the answer to a navigation counts, so a hidden frame opens the
// clearing address on the service's own port. The service's page is unloaded
// first, so that nothing it writes on its way out survives. Its frame then
// goes back to the page the user was on.
async function clearService(p) {
  clearing = true;
  render();
  const frame = frames.get(p.id);
  try {
    const resp = await fetch(`/api/clear/${encodeURIComponent(p.id)}`, { method: 'POST' });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    const { url } = await resp.json();
    if (frame) await navigated(frame, 'about:blank');
    const helper = el('iframe');
    helper.hidden = true;
    document.body.append(helper);
    await navigated(helper, url);
    helper.remove();
    if (frame) frame.contentWindow.history.back();
  } catch (e) {
    clearFailure = e.message;
  }
  clearing = false;
  render();
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
  $('service-clear').addEventListener('click', onClearClick);
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
