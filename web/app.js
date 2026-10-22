const LANGS = ['en', 'zh-CN'];
const LANG_KEY = 'hostdeck.lang';
const STATUS_EVERY = 15_000;
const IDLE_RELEASE = 10 * 60_000; // a page switched away from this long is unloaded, and loads again on its next visit
const ARMED_FOR = 4000; // how long a button that needs a second click waits for it
const PENDING_AGAIN = 2000; // how soon a card still being read is asked for again
const CATALOG = '.plugins'; // the plugins page's address, #/.plugins: no plugin ID starts with a dot

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
let catalogOpen = false; // whether the plugins page is shown
let catalog = null; // the last answer of /api/catalog
let overview = null; // the last answer of /api/cards
let cardsTimer = null; // the next /api/cards while a card is pending
let failure = null; // why the backend could not be read
const frames = new Map(); // plugin id -> its iframe or detail view, kept so each page keeps its state
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
// shows the backend's own detail, or the code when there is none.
function explain(error) {
  if (error.code === 'reported') return pick(error.text);
  const key = `error.${error.code}`;
  return key in dict ? t(key, { detail: error.detail }) : error.detail ?? error.code;
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

// The address names the open entry: #/<id>, or #/.plugins for the plugins
// page. Anything else, a malformed address included, is the overview.
function route() {
  let id = '';
  try {
    id = decodeURIComponent(location.hash.replace(/^#\/?/, ''));
  } catch {
    // a malformed escape: show the overview
  }
  catalogOpen = id === CATALOG;
  current = plugins.find((p) => p.id === id && p.type !== 'link') || null;
  disarmClear();
  clearFailure = null;
  for (const frameId of frames.keys()) {
    if (current && frameId === current.id) hiddenSince.delete(frameId);
    else if (!hiddenSince.has(frameId)) hiddenSince.set(frameId, Date.now());
  }
  if (catalogOpen) loadCatalog();
  if (!current && !catalogOpen) loadCards();
  else clearTimeout(cardsTimer);
  render();
}

function render() {
  for (const e of document.querySelectorAll('[data-i18n]')) e.textContent = t(e.dataset.i18n);
  for (const e of document.querySelectorAll('[data-i18n-label]')) e.setAttribute('aria-label', t(e.dataset.i18nLabel));
  for (const b of document.querySelectorAll('[data-lang]')) b.setAttribute('aria-pressed', String(b.dataset.lang === lang));

  $('banner').hidden = !failure;
  if (failure) $('banner').textContent = t('error.backend', { detail: failure });

  renderNav();
  $('overview').hidden = !!current || catalogOpen;
  $('catalog').hidden = !catalogOpen;
  $('service').hidden = !current;
  if (current) renderService(current);
  else if (catalogOpen) renderCatalog(false);
  else renderOverview();
}

// The sidebar is built once and then updated in place: rebuilding it on every
// status refresh would drop a click that lands while it is replaced.
function renderNav() {
  const nav = $('nav');
  if (nav.children.length !== plugins.length + 2) {
    const pageItem = (href) => {
      const a = el('a', 'nav-item');
      a.href = href;
      a.append(el('span', 'nav-name'));
      return a;
    };
    nav.replaceChildren(pageItem('#/'), ...plugins.map(navItem), pageItem(`#/${CATALOG}`));
  }
  const [overviewItem, ...items] = nav.children;
  const catalogItem = items.pop();
  overviewItem.querySelector('.nav-name').textContent = t('nav.overview');
  overviewItem.classList.toggle('active', !current && !catalogOpen);
  catalogItem.querySelector('.nav-name').textContent = t('nav.catalog');
  catalogItem.classList.toggle('active', catalogOpen);
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
    if (p.type === 'proxy') a.append(el('span', 'dot'));
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
  if (c.pending) card.append(el('p', 'card-pending', t('loading')));
  if (c.error) {
    card.dataset.status = 'error';
    card.append(el('p', 'card-error', explain(c.error)));
    return card;
  }
  if (c.status) {
    card.dataset.status = c.status;
    head.append(el('span', 'pill', t(`card.status.${c.status}`)));
  }
  // A pending card that has never been read has nothing more to show.
  if (c.pending && !c.status && !c.value) return card;
  const big = el('div', 'card-big');
  big.append(el('span', 'card-value', c.value ?? ''), el('span', 'card-label', pick(c.label)));
  card.append(big);
  if (c.rows?.length) {
    const rows = el('ul', 'rows');
    for (const r of c.rows) {
      const li = el('li', 'row');
      if (r.status) li.dataset.status = r.status;
      li.append(el('span', 'row-dot'), el('span', 'row-name', pick(r.name)), el('span', 'row-value', r.value));
      rows.append(li);
    }
    card.append(rows);
  }
  return card;
}

function renderService(p) {
  document.title = `${pick(p.name)} · hostdeck`;
  $('service-icon').textContent = iconOf(p);
  $('service-name').textContent = pick(p.name);
  paintDot($('service-dot'), p);
  $('service-dot').hidden = p.type !== 'proxy';
  $('service-error').hidden = !p.error;
  $('service-reload').hidden = !!p.error;
  $('service-open').hidden = $('service-clear').hidden = !!p.error || p.type === 'detail';
  const clear = $('service-clear');
  clear.textContent = t(clearing ? 'service.clearing' : clearArmed ? 'service.clear.confirm' : 'service.clear');
  clear.classList.toggle('armed', !!clearArmed);
  clear.disabled = clearing;
  const note = $('clear-note');
  note.hidden = !clearArmed && !clearFailure;
  note.textContent = clearArmed ? t('service.clear.explain') : clearFailure ? t('error.clear_failed', { detail: clearFailure }) : '';
  if (p.error) {
    $('service-error').textContent = explain(p.error);
  } else if (p.type === 'detail') {
    let view = frames.get(p.id);
    if (!view) {
      view = el('div', 'frame detail-view');
      view.append(el('p', 'detail-note'), el('div', 'detail-list'));
      view.firstChild.hidden = true;
      view.lastChild.append(el('p', 'empty', t('loading')));
      $('frames').append(view);
      frames.set(p.id, view);
      loadDetail(p, view);
    } else if (view.data && view.lang !== lang) {
      renderDetail(p, view);
    }
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

// A detail page is drawn here from the items its plugin's command prints. It
// is rebuilt only when it is loaded or the language changes, so that a
// periodic render does not reset an action button waiting for its second click.
async function loadDetail(p, view) {
  try {
    view.data = await getJSON(`/api/detail/${encodeURIComponent(p.id)}`);
  } catch (e) {
    view.data = { error: { code: 'backend', detail: e.message } };
  }
  renderDetail(p, view);
}

function renderDetail(p, view) {
  view.lang = lang;
  const { items, error } = view.data;
  const list = view.querySelector('.detail-list');
  if (error) {
    list.replaceChildren(el('p', 'banner', explain(error)));
  } else if (!items?.length) {
    list.replaceChildren(el('p', 'empty', t('detail.empty')));
  } else {
    list.replaceChildren(...items.map((item) => detailItem(p, item, view)));
  }
}

function detailItem(p, item, view) {
  const section = el('section', 'detail-item');
  if (item.status) section.dataset.status = item.status;
  const head = el('header', 'detail-head');
  head.append(el('span', 'row-dot'), el('h3', 'detail-name', pick(item.name)), el('span', 'detail-value', item.value ?? ''));
  for (const action of item.actions || []) head.append(actionButton(p, item, action, view));
  section.append(head);
  if (item.fields?.length) {
    const fields = el('dl', 'detail-fields');
    for (const f of item.fields) fields.append(el('dt', null, pick(f.name)), el('dd', null, f.value));
    section.append(fields);
  }
  if (item.log) {
    const log = el('details', 'detail-log');
    log.append(el('summary', null, t('detail.log')), el('pre', null, item.log));
    section.append(log);
  }
  return section;
}

// twoClicks makes btn run confirmed only on a second click: the first arms it
// for a few seconds and shows armedText. A double-click is one gesture, so its
// second click does not confirm.
function twoClicks(btn, armedText, confirmed) {
  const label = btn.textContent;
  let armed = null;
  btn.addEventListener('click', (e) => {
    if (e.detail > 1 || btn.disabled) return;
    if (!armed) {
      btn.textContent = armedText;
      btn.classList.add('armed');
      armed = setTimeout(() => {
        armed = null;
        btn.textContent = label;
        btn.classList.remove('armed');
      }, ARMED_FOR);
      return;
    }
    clearTimeout(armed);
    armed = null;
    btn.classList.remove('armed');
    confirmed();
  });
}

function actionButton(p, item, action, view) {
  const label = pick(action.label);
  const btn = el('button', 'btn action', label);
  btn.type = 'button';
  twoClicks(btn, t('action.confirm', { label }), async () => {
    btn.disabled = true;
    btn.textContent = t('action.running');
    let note;
    try {
      const resp = await fetch(`/api/action/${encodeURIComponent(p.id)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ item: item.id, action: action.id }),
      });
      const body = await resp.json().catch(() => ({}));
      if (!resp.ok) throw body.error || { code: '', detail: `HTTP ${resp.status}` };
      note = body.message ? pick(body.message) : t('action.done', { label, name: pick(item.name) });
    } catch (err) {
      note = err instanceof TypeError ? t('action.interrupted') : explain(err);
    }
    showNote(view, note);
    setTimeout(() => loadDetail(p, view), 1500);
  });
  return btn;
}

function showNote(view, text) {
  const note = view.querySelector('.detail-note');
  note.textContent = text;
  note.hidden = false;
  clearTimeout(note.timer);
  note.timer = setTimeout(() => {
    note.hidden = true;
  }, 8000);
}

// The plugins page lists the plugins built into hostdeck. Like a detail page,
// it is rebuilt only when it is loaded or the language changes, so that a
// periodic render does not reset an add button waiting for its second click.
async function loadCatalog() {
  try {
    catalog = await getJSON('/api/catalog');
    failure = null;
  } catch (e) {
    failure = e.message;
  }
  renderCatalog(true);
}

function renderCatalog(loaded) {
  if (!catalog || (!loaded && catalog.lang === lang)) return;
  catalog.lang = lang;
  document.title = `${t('catalog.title')} · hostdeck`;
  $('catalog-intro').textContent = t('catalog.intro', { dir: catalog.pluginDir });
  const installed = catalog.installed.map(installedItem);
  $('catalog-installed').replaceChildren(...(installed.length ? installed : [el('p', 'empty', t('catalog.none'))]));
  $('catalog-available-heading').hidden = !catalog.available.length;
  $('catalog-available').replaceChildren(...catalog.available.map(availableItem));
}

function catalogCard(p) {
  const item = el('article', 'card catalog-item');
  item.dataset.id = p.id;
  const head = el('header', 'card-head');
  head.append(el('span', 'icon', pick(p.name).slice(0, 1)), el('h2', 'card-name', pick(p.name)));
  item.append(head);
  if (p.description) item.append(el('p', 'catalog-description', pick(p.description)));
  return item;
}

// catalogButton posts to url on a second click, says how it went with
// noteOf(the answer), and reads the page again.
function catalogButton(label, armedText, url, noteOf) {
  const btn = el('button', 'btn', label);
  btn.type = 'button';
  twoClicks(btn, armedText, async () => {
    btn.disabled = true;
    btn.textContent = t('catalog.working');
    let note;
    try {
      const resp = await fetch(url, { method: 'POST' });
      const body = await resp.json().catch(() => ({}));
      if (!resp.ok) throw body.error || { code: '', detail: `HTTP ${resp.status}` };
      note = noteOf(body);
    } catch (err) {
      note = explain(err);
    }
    const n = $('catalog-note');
    n.textContent = note;
    n.hidden = false;
    loadCatalog();
  });
  return btn;
}

function installedItem(p) {
  const item = catalogCard(p);
  const folder = `.${p.id}`;
  const state = p.broken
    ? t('catalog.state.broken')
    : p.disabled
      ? t(p.loaded ? 'catalog.state.disabling' : 'catalog.state.disabled', { folder })
      : t(p.loaded ? 'catalog.state.loaded' : 'catalog.state.added');
  const stateLine = el('p', 'catalog-state', state);
  stateLine.dataset.state = p.broken ? 'broken' : p.disabled ? 'disabled' : 'enabled';
  item.append(stateLine);
  const name = pick(p.name);
  const verb = p.disabled ? 'enable' : 'disable';
  item.append(
    catalogButton(
      t(`catalog.${verb}`),
      t(`catalog.${verb}.confirm`),
      `/api/plugins/${encodeURIComponent(p.id)}/${verb}`,
      () => t(`catalog.${verb}d.note`, { name }),
    ),
  );
  return item;
}

function availableItem(p) {
  const item = catalogCard(p);
  const btn = catalogButton(t('catalog.add'), t('catalog.add.confirm'), `/api/catalog/${encodeURIComponent(p.id)}`, (body) =>
    t('catalog.added', { path: body.path }),
  );
  btn.classList.add('add');
  item.append(btn);
  return item;
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

// cleared opens url in frame and says whether the page there confirmed the
// clearing. A refusal loads too, so loading alone does not say so.
const cleared = (frame, url) =>
  new Promise((resolve) => {
    let timer;
    const done = (ok) => {
      removeEventListener('message', onMessage);
      clearTimeout(timer);
      resolve(ok);
    };
    const onMessage = (e) => {
      if (e.source === frame.contentWindow && e.data === 'hostdeck:cleared') done(true);
    };
    addEventListener('message', onMessage);
    frame.addEventListener('load', () => (timer = setTimeout(() => done(false), 2000)), { once: true });
    frame.src = url;
  });

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
    const ok = await cleared(helper, url);
    helper.remove();
    if (frame) frame.contentWindow.history.back();
    if (!ok) throw new Error(t('service.clear.unconfirmed'));
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

// A card still being read is asked for again shortly, for as long as the
// overview is shown.
async function loadCards() {
  clearTimeout(cardsTimer);
  try {
    overview = await getJSON('/api/cards');
    failure = null;
  } catch (e) {
    failure = e.message;
  }
  render();
  if (!current && overview?.cards.some((c) => c.pending)) cardsTimer = setTimeout(loadCards, PENDING_AGAIN);
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
    if (!current || !frames.has(current.id)) return;
    if (current.type === 'detail') loadDetail(current, frames.get(current.id));
    else frames.get(current.id).src = current.url;
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
