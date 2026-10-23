const LANGS = ['en', 'zh-CN'];
const LANG_KEY = 'hostdeck.lang';
const STATUS_EVERY = 15_000;
const IDLE_RELEASE = 10 * 60_000; // a page switched away from this long is unloaded, and loads again on its next visit
const ARMED_FOR = 4000; // how long a button that needs a second click waits for it
const PENDING_AGAIN = 2000; // how soon a card still being read is asked for again
const LAYOUT_NOTE_FOR = 8000; // idle time before the saved notice and its one-step undo close
const INTERNAL_DRAG = 'application/x-hostdeck-internal';
const CATALOG = '.plugins'; // the plugins page's address, #/.plugins: no plugin ID starts with a dot

const $ = (id) => document.getElementById(id);

function el(tag, className, text) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined && text !== null) e.textContent = text;
  return e;
}

function feedback(note, text, failed = false) {
  note.setAttribute('role', failed ? 'alert' : 'status');
  note.dataset.kind = failed ? 'error' : 'success';
  if (note.textContent !== text) note.textContent = text;
  note.hidden = false;
}

function pageHint() {
  $('page-hint').textContent = t($('page-kind').value === 'link' ? 'catalog.page.linkHint' : 'catalog.page.embedHint');
}

// Action panels stay next to their owner in the DOM, but the browser's top
// layer keeps them outside scrolling and clipped containers.
let openPanel = null;
let panelSequence = 0;
const narrow = matchMedia('(max-width: 640px)');

function closePanel(restore = false) {
  if (!openPanel) return;
  const { panel, trigger } = openPanel;
  openPanel = null;
  if (panel.isConnected && panel.matches(':popover-open')) panel.hidePopover();
  panel.hidden = true;
  trigger.setAttribute('aria-expanded', 'false');
  if (restore && trigger.isConnected) trigger.focus();
}

function positionPanel() {
  if (!openPanel) return;
  const { panel, trigger } = openPanel;
  if (!trigger.isConnected || trigger.closest('[inert]') || !trigger.checkVisibility()) return closePanel();
  const r = trigger.getBoundingClientRect();
  if (r.bottom < 0 || r.top > innerHeight) return closePanel();
  const size = panel.getBoundingClientRect();
  panel.style.left = `${Math.max(8, Math.min(r.right - size.width, innerWidth - size.width - 8))}px`;
  panel.style.top = `${Math.max(8, Math.min(r.bottom + 4, innerHeight - size.height - 8))}px`;
}

function moreActions(name, controls) {
  const wrap = el('div', 'more');
  const trigger = el('button', 'more-trigger', '⋯');
  trigger.type = 'button';
  trigger.setAttribute('aria-label', t('more.for', { name }));
  trigger.setAttribute('aria-expanded', 'false');
  const panel = el('div', 'action-panel');
  panel.id = `actions-${++panelSequence}`;
  panel.setAttribute('popover', 'manual');
  panel.hidden = true;
  trigger.setAttribute('aria-controls', panel.id);
  panel.append(...controls);
  trigger.addEventListener('click', (e) => {
    e.stopPropagation();
    if (openPanel?.trigger === trigger) return closePanel(true);
    closePanel();
    panel.hidden = false;
    panel.showPopover();
    openPanel = { panel, trigger };
    trigger.setAttribute('aria-expanded', 'true');
    positionPanel();
  });
  panel.addEventListener('click', (e) => e.stopPropagation());
  wrap.append(trigger, panel);
  return wrap;
}

function setNavigation(open, restore = false) {
  open = narrow.matches && open;
  const wasOpen = document.querySelector('.app').classList.contains('nav-open');
  if (open !== wasOpen) closePanel();
  document.querySelector('.app').classList.toggle('nav-open', open);
  $('menu').setAttribute('aria-expanded', String(open));
  document.querySelector('.side-menu').inert = narrow.matches && !open;
  document.querySelector('main').inert = open;
  $('nav-backdrop').hidden = !open;
  if (open && !wasOpen) $('nav-overview').focus();
  else if (restore && narrow.matches) $('menu').focus();
}

function onInterfaceKey(e) {
  if (e.key === 'Escape') {
    if (openPanel) { e.preventDefault(); closePanel(true); }
    else if (document.querySelector('.app').classList.contains('nav-open')) {
      e.preventDefault(); setNavigation(false, true);
    }
    return;
  }
  if (e.key !== 'Tab') return;
  if (openPanel && document.activeElement === openPanel.trigger && !e.shiftKey) {
    const first = openPanel.panel.querySelector('button:not(:disabled), a[href]');
    if (first) { e.preventDefault(); first.focus(); }
    return;
  }
  if (!document.querySelector('.app').classList.contains('nav-open')) return;
  const targets = [...document.querySelector('.sidebar').querySelectorAll('a[href], button:not(:disabled)')]
    .filter((node) => node.getClientRects().length && !node.closest('[hidden]'));
  const first = targets[0], last = targets.at(-1);
  if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
  if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
}

let lang = 'en';
let dict = {};
let plugins = []; // the sidebar entries
let status = {}; // plugin id -> up | down
let current = null; // the open sidebar entry, or null on the overview
let catalogOpen = false; // whether the plugins page is shown
let catalog = null; // the last answer of /api/catalog
let catalogNoteFor = null; // the plugin whose card the last result belongs on
let catalogAddRequested = false; // the overview add card should land at the add form
let pendingOverview = null; // a reading waiting until card controls lose focus
let overview = null; // the last answer of /api/cards
let cardsTimer = null; // the next /api/cards while a card is pending
let failure = null; // why the backend could not be read
const frames = new Map(); // plugin id -> its iframe or detail view, kept so each page keeps its state
let clearArmed = null; // the timer while the clear button waits for its second click
let clearing = false;
let clearFailure = null; // why the last clearing failed
const hiddenSince = new Map(); // plugin id -> when its page was switched away from
let dragging = false; // whether a sidebar entry or a card is being dragged
let suppressClick = false; // a click that ends a drag is not an open
let layoutUndo = null; // the layout lists to post to undo the last move, widen, drag, hide or reset
let layoutNoteTimer = null;
let savingLayout = false;
let layoutEpoch = 0; // a save that answers late must not replace a newer one
let stored = { sidebar: [], cards: [], wide: [], hidden: [] }; // the layout file, hidden IDs included
let layoutKnown = false; // a hide before this is known would post only part of the hidden list

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
  closePanel();
  if (remember) setNavigation(false, true);
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
  closePanel();
  pendingOverview = null;
  setNavigation(false);
  let id = '';
  try {
    id = decodeURIComponent(location.hash.replace(/^#\/?/, ''));
  } catch {
    // a malformed escape: show the overview
  }
  catalogOpen = id === CATALOG;
  if (!catalogOpen) catalogAddRequested = false;
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
  for (const e of document.querySelectorAll('[data-i18n-placeholder]')) e.placeholder = t(e.dataset.i18nPlaceholder);
  for (const b of document.querySelectorAll('[data-lang]')) b.setAttribute('aria-pressed', String(b.dataset.lang === lang));

  $('banner').hidden = !failure;
  if (failure) {
    const message = t('error.backend', { detail: failure });
    if ($('banner').textContent !== message) $('banner').textContent = message;
  }

  // Redrawing the sidebar or the cards would drop what is being dragged.
  if (dragging) return;
  pageHint();
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
  const entries = JSON.stringify(plugins.map((p) => [p.id, p.type, p.url]));
  if (nav.dataset.entries !== entries) {
    nav.dataset.entries = entries;
    nav.replaceChildren(...plugins.map(navItem));
  }
  const overviewItem = $('nav-overview');
  const catalogItem = $('nav-catalog');
  overviewItem.querySelector('.nav-name').textContent = t('nav.overview');
  overviewItem.classList.toggle('active', !current && !catalogOpen);
  catalogItem.querySelector('.nav-name').textContent = t('nav.catalog');
  catalogItem.classList.toggle('active', catalogOpen);
  [...nav.querySelectorAll('.nav-row')].forEach((row, i) => {
    const p = plugins[i];
    const a = row.querySelector('.nav-item');
    a.querySelector('.nav-name').textContent = pick(p.name);
    a.querySelector('.icon').textContent = iconOf(p);
    a.classList.toggle('active', current?.id === p.id);
    const d = a.querySelector('.dot');
    if (d) paintDot(d, p);
    row.querySelector('.move-up').textContent = t('move.up');
    row.querySelector('.move-down').textContent = t('move.down');
    row.querySelector('.hide').textContent = t('layout.hide');
    row.querySelector('.more-trigger').setAttribute('aria-label', t('more.for', { name: pick(p.name) }));
  });
  placeMoves(nav, '.nav-row');
}

function navItem(p) {
  const row = el('div', 'nav-row');
  row.dataset.id = p.id;
  row.draggable = true;
  const a = el('a', 'nav-item');
  a.dataset.id = p.id;
  a.draggable = false;
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
  const moves = moreActions(pick(p.name), [moveButton('up'), moveButton('down'), hideButton()]);
  row.append(a, moves);
  return row;
}

// moveButton steps its sidebar row or card by one. The button stays outside
// the link, so Tab reaches it and activating it does not open the entry.
function moveButton(dir) {
  const btn = el('button', `move move-${dir}`, t(dir === 'up' ? 'move.up' : 'move.down'));
  btn.type = 'button';
  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    const item = btn.closest('.nav-row, .card');
    if (!item) return;
    moveItem(item.closest('#nav') ? 'sidebar' : 'cards', item.dataset.id, dir === 'up' ? -1 : 1, btn);
  });
  return btn;
}

// hideButton drops a plugin from the sidebar and the cards. It stays installed.
function hideButton() {
  const btn = el('button', 'move hide', t('layout.hide'));
  btn.type = 'button';
  btn.title = t('layout.hideHint');
  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    const item = btn.closest('.nav-row, .card');
    if (!item) return;
    hideId(item.dataset.id);
  });
  return btn;
}

// hideId saves the whole hidden list. The plugin's place in the order stays.
function hideId(id) {
  closePanel();
  if (!layoutKnown || stored.hidden.includes(id)) return;
  const previous = { hidden: stored.hidden.slice() };
  stored.hidden = [...stored.hidden, id];
  plugins = plugins.filter((p) => p.id !== id);
  $('nav').dataset.entries = '';
  if (overview) {
    overview.cards = overview.cards.filter((c) => c.id !== id);
    overview.lang = null;
  }
  if (catalog) catalog.lang = null;
  const leaving = current?.id === id;
  if (leaving) current = null;
  render();
  saveLayout({ hidden: stored.hidden.slice() }, previous).then((ok) => {
    if (ok && leaving) location.hash = '#/';
  });
}

// showId puts a hidden plugin back in the place the layout kept for it.
async function showId(id) {
  if (!layoutKnown || !stored.hidden.includes(id)) return;
  const previous = { hidden: stored.hidden.slice() };
  stored.hidden = stored.hidden.filter((x) => x !== id);
  if (!(await saveLayout({ hidden: stored.hidden.slice() }, previous))) return;
  await reloadShown();
}

function placeMoves(list, selector) {
  const items = [...list.querySelectorAll(selector)];
  items.forEach((item, i) => {
    const up = item.querySelector('.move-up');
    const down = item.querySelector('.move-down');
    if (up) up.disabled = i === 0;
    if (down) down.disabled = i === items.length - 1;
  });
}

function focusMove(item, stay) {
  const btn = item.querySelector(stay);
  if (btn && !btn.disabled && btn.checkVisibility()) btn.focus();
  else item.querySelector('.more-trigger')?.focus();
}

function moveItem(which, id, dir, btn) {
  closePanel();
  const list = which === 'sidebar' ? $('nav') : $('cards');
  const selector = which === 'sidebar' ? '.nav-row' : '.card';
  const items = [...list.querySelectorAll(selector)];
  const i = items.findIndex((item) => item.dataset.id === id);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= items.length) return;
  const item = items[i];
  const stay = btn.classList.contains('move-up') ? '.move-up' : '.move-down';
  if (dir > 0) items[j].after(item);
  else items[j].before(item);
  const order = [...list.querySelectorAll(selector)].map((node) => node.dataset.id);
  placeMoves(list, selector);
  focusMove(item, stay);
  if (which === 'sidebar') {
    const previous = plugins.map((p) => p.id);
    plugins.sort(byOrder(order));
    $('nav').dataset.entries = JSON.stringify(plugins.map((p) => [p.id, p.type, p.url]));
    saveLayout({ sidebar: order }, { sidebar: previous });
    return;
  }
  const previous = overview.cards.map((c) => c.id);
  overview.cards.sort(byOrder(order));
  saveLayout({ cards: order }, { cards: previous });
}

const iconOf = (p) => p.icon || pick(p.name).slice(0, 1);

function paintDot(d, p) {
  const s = p.error ? 'down' : status[p.id];
  if (s) d.dataset.status = s;
  else delete d.dataset.status;
  d.title = s ? t(`status.${s}`) : t('status.unknown');
  d.setAttribute('role', 'img');
  d.setAttribute('aria-label', d.title);
}

function renderOverview() {
  document.title = 'hostdeck';
  const cards = overview ? overview.cards : [];
  const noCards = !overview || cards.length === 0;
  $('empty').hidden = !overview || !noCards;
  if (overview && noCards) {
    $('empty').textContent = stored.hidden.length ? t('overview.hidden') : t('overview.empty', { dir: overview.pluginDir });
  }
  // The cards are rebuilt only when they are read or rearranged or the
  // language changes: a periodic render would swallow a click on a card's
  // button that lands while the card is replaced.
  if (!overview || overview.lang === lang) {
    placeMoves($('cards'), '.card');
    return;
  }
  overview.lang = lang;
  $('cards').replaceChildren(...cards.map(cardElement), $('empty-add'));
  placeMoves($('cards'), '.card');
}

function cardElement(c) {
  const card = el('article', 'card');
  card.dataset.id = c.id;
  card.draggable = true;
  card.classList.toggle('wide', !!c.wide);
  const head = el('header', 'card-head');
  const width = el('button', 'card-width', t(c.wide ? 'card.narrow' : 'card.widen'));
  width.type = 'button';
  width.addEventListener('click', () => {
    const previous = overview.cards.filter((x) => x.wide).map((x) => x.id);
    closePanel();
    c.wide = !c.wide;
    overview.lang = null;
    saveLayout({ wide: overview.cards.filter((x) => x.wide).map((x) => x.id) }, { wide: previous });
    render();
  });
  const moves = moreActions(pick(c.name), [moveButton('up'), moveButton('down'), hideButton(), width]);
  head.append(el('span', 'icon', c.icon || pick(c.name).slice(0, 1)), cardTitle(c), moves);
  card.append(head);
  card.addEventListener('click', (e) => {
    if (suppressClick || e.target.closest('button, a, summary, .action-panel')) return;
    card.querySelector('.card-name a')?.click();
  });
  if (c.error || c.status) card.append(el('span', 'pill', t(`card.status.${c.error ? 'error' : c.status}`)));
  if (c.pending) card.append(el('p', 'card-pending', t('loading')));
  if (c.error) {
    card.dataset.status = 'error';
    card.append(el('p', 'card-error', explain(c.error)));
    if (c.error.file) card.append(errorFile(c.error.file));
    stamp(card, c);
    return card;
  }
  if (c.status) {
    card.dataset.status = c.status;
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
  stamp(card, c);
  return card;
}

// cardTitle names the card, and links it when the plugin is in the sidebar.
// A card on its own has nowhere to open.
function cardTitle(c) {
  const name = el('h2', 'card-name');
  const page = plugins.find((p) => p.id === c.id);
  if (!page) {
    name.textContent = pick(c.name);
    return name;
  }
  const a = el('a', null, pick(c.name));
  a.draggable = false;
  if (page.type === 'link') {
    a.href = page.url;
    a.target = '_blank';
    a.rel = 'noopener';
  } else {
    a.href = `#/${encodeURIComponent(page.id)}`;
  }
  name.append(a);
  return name;
}

// sortable lets the user drag the items of list that match selector into a
// new order. Once one is dropped, moved gets the IDs in the order they ended in.
function sortable(list, selector, moved) {
  let dragged = null;
  let before = null;
  list.addEventListener('dragstart', (e) => {
    if (e.target.closest('button')) return;
    dragged = e.target.closest(selector);
    if (!dragged) return;
    before = [...list.querySelectorAll(selector)].map((item) => item.dataset.id);
    dragging = true;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', dragged.dataset.id);
    dragged.classList.add('dragging');
  });
  list.addEventListener('dragover', (e) => {
    const over = e.target.closest(selector);
    if (!dragged || !over) return;
    e.preventDefault();
    if (over === dragged) return;
    const items = [...list.querySelectorAll(selector)];
    if (items.indexOf(dragged) < items.indexOf(over)) over.after(dragged);
    else over.before(dragged);
  });
  list.addEventListener('drop', (e) => {
    if (dragged) e.preventDefault();
  });
  list.addEventListener('dragend', () => {
    if (!dragged) return;
    dragged.classList.remove('dragging');
    dragged = null;
    dragging = false;
    // The click that ends a drag would otherwise open the card.
    suppressClick = true;
    setTimeout(() => {
      suppressClick = false;
    }, 0);
    const order = [...list.querySelectorAll(selector)].map((item) => item.dataset.id);
    moved(order, before);
  });
}

const byOrder = (order) => (a, b) => order.indexOf(a.id) - order.indexOf(b.id);

function applyStored(l) {
  stored = {
    sidebar: l.sidebar ?? [],
    cards: l.cards ?? [],
    wide: l.wide ?? [],
    hidden: l.hidden ?? [],
  };
  layoutKnown = true;
}

function dismissLayoutNote() {
  clearTimeout(layoutNoteTimer);
  layoutUndo = null;
  $('layout-note').hidden = true;
}

function scheduleLayoutNote() {
  clearTimeout(layoutNoteTimer);
  if (!layoutUndo || savingLayout) return;
  layoutNoteTimer = setTimeout(() => {
    const note = $('layout-note');
    if (note.matches(':hover') || note.contains(document.activeElement)) return;
    dismissLayoutNote();
  }, LAYOUT_NOTE_FOR);
}

// saveLayout sends the lists of the layout that changed; the others stay.
// The answer is the whole layout, so the next hide can send every hidden ID.
async function saveLayout(change, previous) {
  clearTimeout(layoutNoteTimer);
  savingLayout = true;
  pendingOverview = null;
  const epoch = ++layoutEpoch;
  // The undo is offered before the answer: the page already shows the change,
  // and a slower answer must not put an older change back in its place.
  if (previous && JSON.stringify(change) !== JSON.stringify(previous)) {
    layoutUndo = previous;
    $('layout-note').hidden = false;
  }
  try {
    const resp = await fetch('/api/layout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(change),
    });
    if (!resp.ok) throw new Error(`/api/layout: HTTP ${resp.status}`);
    const saved = await resp.json();
    if (epoch !== layoutEpoch) return false;
    applyStored(saved);
    savingLayout = false;
    scheduleLayoutNote();
    return true;
  } catch (e) {
    if (epoch !== layoutEpoch) return false;
    savingLayout = false;
    if (layoutUndo === previous) dismissLayoutNote();
    // The server keeps its last saved layout when a write fails. Read that
    // state before showing the error, so an optimistic hide or move is undone.
    try {
      const [saved, shown, cards] = await Promise.all([
        getJSON('/api/layout'), getJSON('/api/plugins'), getJSON('/api/cards'),
      ]);
      if (epoch !== layoutEpoch) return false;
      applyStored(saved);
      plugins = shown;
      overview = cards;
      $('nav').dataset.entries = '';
      if (catalog) catalog.lang = null;
    } catch {
      // Keep the original write failure when recovery cannot reach the server.
    }
    if (epoch !== layoutEpoch) return false;
    failure = e.message;
    render();
    return false;
  }
}

async function undoLayout() {
  const previous = layoutUndo;
  if (!previous) return;
  dismissLayoutNote();
  const hiddenBefore = stored.hidden.slice();
  if (previous.sidebar) {
    plugins.sort(byOrder(previous.sidebar));
    $('nav').dataset.entries = '';
  }
  if (overview && previous.cards) {
    overview.cards.sort(byOrder(previous.cards));
    overview.lang = null;
  }
  if (overview && previous.wide) {
    const wide = new Set(previous.wide);
    for (const c of overview.cards) c.wide = wide.has(c.id);
    overview.lang = null;
  }
  render();
  await saveLayout(previous);
  if (previous.hidden && JSON.stringify(previous.hidden) !== JSON.stringify(hiddenBefore)) await reloadShown();
}

// resetLayout clears the order, the widths and what is hidden.
async function resetLayout() {
  if (!layoutKnown) return;
  const previous = {
    sidebar: stored.sidebar.slice(),
    cards: stored.cards.slice(),
    wide: stored.wide.slice(),
    hidden: stored.hidden.slice(),
  };
  if (!previous.sidebar.length && !previous.cards.length && !previous.wide.length && !previous.hidden.length) return;
  if (!(await saveLayout({ reset: true }, previous))) return;
  await reloadShown();
}

// reloadShown reads the sidebar again, and the page that is open.
async function reloadShown() {
  await loadPlugins();
  $('nav').dataset.entries = '';
  if (catalogOpen) await loadCatalog();
  if (!current && !catalogOpen) await loadCards();
  else if (overview) overview.lang = null;
  render();
}

async function loadLayout() {
  try {
    applyStored(await getJSON('/api/layout'));
  } catch (e) {
    layoutKnown = false;
    failure = e.message;
  }
}

function renderService(p) {
  document.title = `${pick(p.name)} · hostdeck`;
  $('service-icon').textContent = iconOf(p);
  $('service').setAttribute('aria-busy', String(clearing));
  $('service-more').hidden = !!p.error || p.type !== 'proxy';
  $('service-more').querySelector('.more-trigger').setAttribute('aria-label', t('more.for', { name: pick(p.name) }));
  $('service-name').textContent = pick(p.name);
  paintDot($('service-dot'), p);
  $('service-dot').hidden = p.type !== 'proxy';
  $('service-error').hidden = !p.error;
  $('service-reload').hidden = !!p.error;
  $('service-reload').disabled = p.type === 'detail' && frames.get(p.id)?.getAttribute('aria-busy') === 'true';
  $('service-open').hidden = $('service-clear').hidden = !!p.error || p.type === 'detail';
  const clear = $('service-clear');
  clear.textContent = t(clearing ? 'service.clearing' : clearArmed ? 'service.clear.confirm' : 'service.clear');
  clear.classList.toggle('armed', !!clearArmed);
  clear.disabled = clearing;
  const note = $('clear-note');
  note.hidden = !clearArmed && !clearFailure && !clearing;
  note.setAttribute('role', clearFailure ? 'alert' : 'status');
  const clearMessage = clearArmed ? t('service.clear.explain') : clearFailure ? t('error.clear_failed', { detail: clearFailure }) : clearing ? t('service.clearing') : '';
  if (note.textContent !== clearMessage) note.textContent = clearMessage;
  if (p.error) {
    const message = explain(p.error);
    if ($('service-error').textContent !== message) $('service-error').textContent = message;
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
  for (const [id, f] of frames) {
    f.hidden = id !== p.id || !!p.error;
    if (id === p.id && f.tagName === 'IFRAME') f.title = pick(p.name);
  }
}

// A detail page is drawn here from the items its plugin's command prints. It
// is rebuilt only when it is loaded or the language changes, so that a
// periodic render does not reset an action button waiting for its second click.
async function loadDetail(p, view) {
  view.setAttribute('aria-busy', 'true');
  if (current?.id === p.id) $('service-reload').disabled = true;
  try {
    view.data = await getJSON(`/api/detail/${encodeURIComponent(p.id)}`);
  } catch (e) {
    view.data = { error: { code: 'backend', detail: e.message } };
  } finally {
    view.setAttribute('aria-busy', 'false');
    if (current?.id === p.id) $('service-reload').disabled = false;
  }
  renderDetail(p, view);
}

function renderDetail(p, view) {
  view.lang = lang;
  const { items, error } = view.data;
  const list = view.querySelector('.detail-list');
  if (error) {
    const note = el('p', 'banner', explain(error));
    note.setAttribute('role', 'alert');
    list.replaceChildren(note);
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
  head.append(el('h2', 'detail-name', pick(item.name)));
  if (item.status) head.append(el('span', 'pill detail-status', t(`card.status.${item.status}`)));
  section.append(head);
  if (item.value !== undefined && item.value !== null) section.append(el('p', 'detail-value', item.value));
  if (item.fields?.length) {
    const fields = el('dl', 'detail-fields');
    for (const f of item.fields) fields.append(el('dt', null, pick(f.name)), el('dd', null, f.value));
    section.append(fields);
  }
  if (item.log) {
    const log = el('details', 'detail-log');
    const content = el('pre', null, item.log);
    content.tabIndex = 0;
    content.setAttribute('aria-label', t('detail.log'));
    log.append(el('summary', null, t('detail.log')), content);
    section.append(log);
  }
  if (item.actions?.length) {
    const actions = el('div', 'detail-actions');
    actions.append(...item.actions.map((action) => actionButton(p, item, action, view)));
    section.append(actions);
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
    let failed = false;
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
      failed = true;
    }
    showNote(view, note, failed);
    setTimeout(() => loadDetail(p, view), 1500);
  });
  return btn;
}

function showNote(view, text, failed = false) {
  const note = view.querySelector('.detail-note');
  feedback(note, text, failed);
  clearTimeout(note.timer);
  note.timer = setTimeout(() => {
    note.hidden = true;
  }, 8000);
}

function focusCatalogAdd() {
  $('catalog-add').scrollIntoView({ block: 'start' });
  $('page-address').focus();
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
  if (catalogOpen && catalogAddRequested) {
    catalogAddRequested = false;
    focusCatalogAdd();
  }
}

function renderCatalog(loaded) {
  if (!catalog || (!loaded && catalog.lang === lang)) return;
  if (openPanel && $('catalog').contains(openPanel.trigger)) closePanel();
  parkCatalogNote();
  catalog.lang = lang;
  document.title = `${t('catalog.title')} · hostdeck`;
  $('catalog-intro').textContent = t('catalog.intro');
  $('catalog-directory').textContent = catalog.pluginDir;
  const byName = (a, b) => pick(a.name).localeCompare(pick(b.name), lang);
  const groups = [
    ['use', 'catalog.group.use', (p) => !p.disabled && !p.broken && !p.error],
    ['disabled', 'catalog.group.disabled', (p) => p.disabled && !p.broken && !p.error],
    ['error', 'catalog.group.error', (p) => !!(p.broken || p.error)],
  ].flatMap(([id, key, pred]) => {
    const items = catalog.installed.filter(pred).sort(byName).map(installedItem);
    if (!items.length) return [];
    const section = el('section', 'catalog-group');
    section.dataset.group = id;
    const grid = el('div', 'catalog-list');
    grid.append(...items);
    section.append(el('h3', 'catalog-heading', t(key)), grid);
    return [section];
  });
  $('catalog-installed').replaceChildren(...(groups.length ? groups : [el('p', 'empty', t('catalog.none'))]));
  const trashed = catalog.trash ?? [];
  $('catalog-trash').hidden = trashed.length === 0;
  $('catalog-trash-list').replaceChildren(...trashed.map(trashItem));
  $('catalog-available-heading').hidden = !catalog.available.length;
  $('catalog-available').replaceChildren(...[...catalog.available].sort(byName).map(availableItem));
  placeCatalogNote();
  updateCatalogSearch();
}

// The search box is for a long page. Fewer plugins than this stay a plain list.
const SEARCH_AFTER = 8;

function updateCatalogSearch() {
  const search = $('catalog-search');
  const many = (catalog?.installed.length ?? 0) + (catalog?.available.length ?? 0) > SEARCH_AFTER;
  search.hidden = !many;
  $('catalog-search-wrap').hidden = !many;
  if (!many) search.value = '';
  filterCatalog();
}

function filterCatalog() {
  const q = $('catalog-search').value.trim().toLowerCase();
  for (const card of document.querySelectorAll('#catalog .catalog-item')) {
    const name = card.querySelector('.card-name')?.textContent.toLowerCase() ?? '';
    card.hidden = q !== '' && !name.includes(q);
  }
  for (const group of document.querySelectorAll('.catalog-group')) {
    group.hidden = ![...group.querySelectorAll('.catalog-item')].some((card) => !card.hidden);
  }
  const available = [...document.querySelectorAll('#catalog-available .catalog-item')];
  if (available.length) $('catalog-available-heading').hidden = available.every((card) => card.hidden);
  $('catalog-no-results').hidden = !q || [...document.querySelectorAll('#catalog .catalog-item')].some((card) => !card.hidden);
  const trash = $('catalog-trash');
  if (trash) {
    const items = [...trash.querySelectorAll('.catalog-item')];
    trash.hidden = items.length === 0 || items.every((card) => card.hidden);
  }
}

// parkCatalogNote lifts the result out of a card before that card is rebuilt.
function parkCatalogNote() {
  const note = $('catalog-note');
  const form = $('page-form');
  if (note && form && note.closest('.catalog-item')) form.before(note);
}

// placeCatalogNote puts the last result on the card it is about. A result
// that is not about one plugin, such as an address the form rejected, stays
// where the page first drew it.
function placeCatalogNote() {
  if (!catalogNoteFor) return;
  const card = document.querySelector(`#catalog .catalog-item[data-id="${CSS.escape(catalogNoteFor)}"]`);
  if (card) card.insertBefore($('catalog-note'), card.firstChild);
}

function catalogCard(p) {
  const item = el('article', 'card catalog-item');
  item.dataset.id = p.id;
  const head = el('header', 'card-head');
  head.append(el('span', 'icon', iconOf(p)), el('h2', 'card-name', pick(p.name)));
  item.append(head);
  if (p.description) item.append(el('p', 'catalog-description', pick(p.description)));
  return item;
}

// providesList names the parts a plugin's manifest declares.
function providesList(p) {
  const keys = ['page', 'card', 'detail', 'actions'].filter((key) => p[key]);
  if (!keys.length) return null;
  const list = el('ul', 'catalog-provides');
  for (const key of keys) list.append(el('li', null, t(`catalog.provides.${key}`)));
  return list;
}

// openControl opens a plugin that this run put in the sidebar.
function openControl(p) {
  const live = plugins.find((x) => x.id === p.id);
  if (!live) return null;
  const a = el('a', 'btn catalog-open', t('catalog.open'));
  if (live.type === 'link') {
    a.href = live.url;
    a.target = '_blank';
    a.rel = 'noopener';
  } else {
    a.href = `#/${encodeURIComponent(live.id)}`;
  }
  return a;
}

// postCatalog posts to url, says how it went with noteOf(the answer), and
// reads the page and the sidebar again.
async function postCatalog(btn, url, noteOf, id) {
  btn.disabled = true;
  btn.textContent = t('catalog.working');
  let note;
  let failed = false;
  try {
    const resp = await fetch(url, { method: 'POST' });
    const body = await resp.json().catch(() => ({}));
    if (!resp.ok) throw body.error || { code: '', detail: `HTTP ${resp.status}` };
    note = noteOf(body);
  } catch (err) {
    note = explain(err);
    failed = true;
  }
  catalogNoteFor = id;
  const n = $('catalog-note');
  feedback(n, note, failed);
  await loadPlugins();
  render();
  refreshStatus();
  loadCatalog();
}

// catalogButton posts to url on a second click. A double-click does not.
function catalogButton(label, armedText, url, noteOf, id) {
  const btn = el('button', 'btn', label);
  btn.type = 'button';
  twoClicks(btn, armedText, () => postCatalog(btn, url, noteOf, id));
  return btn;
}

// catalogOnce posts to url on one click. A double-click is one gesture.
function catalogOnce(label, url, noteOf, id) {
  const btn = el('button', 'btn', label);
  btn.type = 'button';
  btn.addEventListener('click', (e) => {
    if (e.detail > 1 || btn.disabled) return;
    postCatalog(btn, url, noteOf, id);
  });
  return btn;
}

// addPage adds the web page at address and opens it. mode "link" stores a
// link and need not be local; anything else embeds a page on this machine.
// If it cannot, the plugins page says why.
async function addPage(address, name = '', mode = '') {
  catalogNoteFor = null;
  const note = $('catalog-note');
  if (note) $('page-form').before(note);
  const payload = { address, name };
  if (mode === 'link') payload.mode = 'link';
  try {
    const resp = await fetch('/api/pages', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    const body = await resp.json().catch(() => ({}));
    if (!resp.ok) throw body.error || { code: '', detail: `HTTP ${resp.status}` };
    await loadPlugins();
    location.hash = `#/${encodeURIComponent(body.id)}`;
    refreshStatus();
    return true;
  } catch (err) {
    const n = $('catalog-note');
    feedback(n, explain(err), true);
    location.hash = `#/${CATALOG}`;
    return false;
  }
}

async function onPageSubmit(e) {
  e.preventDefault();
  const form = e.target;
  const btn = form.querySelector('button');
  btn.disabled = true;
  if (await addPage($('page-address').value.trim(), $('page-name').value, $('page-kind').value)) form.reset();
  btn.disabled = false;
  pageHint();
}

// An external service address dropped onto the console is embedded. Internal
// drags and the console's own origin must not create another plugin.
function onDragOver(e) {
  if (!dragging && e.dataTransfer.types.includes('text/uri-list')) e.preventDefault();
}

function onDrop(e) {
  if (!e.dataTransfer.types.includes('text/uri-list')) return;
  e.preventDefault();
  if (dragging || e.dataTransfer.types.includes(INTERNAL_DRAG)) return;
  const address = e.dataTransfer
    .getData('text/uri-list')
    .split(/\r?\n/)
    .find((line) => line && !line.startsWith('#'));
  if (!address) return;
  try {
    if (new URL(address.trim()).origin === location.origin) return;
  } catch {
    // The API words invalid addresses, as it does for the add-page form.
  }
  addPage(address.trim());
}

// errorFile is the manifest path, folded away from the sentence that says why.
function errorFile(path) {
  const d = el('details', 'error-file');
  d.append(el('summary', null, t('error.file')), el('p', null, path));
  return d;
}

function installedItem(p) {
  const item = el('article', 'card catalog-item catalog-entry');
  item.dataset.id = p.id;
  const name = pick(p.name);
  const identity = el('div', 'catalog-identity');
  const head = el('header', 'card-head');
  const title = el('h4', 'card-name', name);
  title.title = name;
  const kind = p.card || p.detail || p.actions ? 'plugin' : p.link ? 'link' : 'page';
  head.append(el('span', 'icon', iconOf(p)), title, el('span', 'catalog-kind', t(`catalog.kind.${kind}`)));
  const description = el('p', 'catalog-description', pick(p.description) || p.proxy || p.link || t('catalog.custom'));
  description.title = pick(p.description) || description.textContent;
  identity.append(head, description);

  const features = el('div', 'catalog-summary');
  features.append(el('span', 'catalog-field-label', t('catalog.features')), providesList(p) || el('p', null, t('catalog.features.none')));
  const status = el('div', 'catalog-status');
  const broken = p.error || p.broken;
  const state = broken ? 'broken' : p.disabled ? (p.loaded ? 'disabling' : 'disabled') : p.loaded ? 'loaded' : 'added';
  const stateLine = el('p', 'catalog-state', t(`catalog.state.${state}`));
  stateLine.dataset.state = broken ? 'broken' : p.disabled ? 'disabled' : p.loaded ? 'enabled' : 'pending';
  const hidden = stored.hidden.includes(p.id);
  const visibility = p.disabled ? (hidden ? 'offHidden' : 'offVisible') : hidden ? 'hidden' : p.loaded || broken ? 'visible' : 'pending';
  status.append(el('span', 'catalog-field-label', t('catalog.connectionState')), stateLine, el('p', 'catalog-visibility', t(`catalog.visibility.${visibility}`)));

  const actions = el('div', 'catalog-actions');
  const open = openControl(p);
  if (open) actions.append(open);
  if (hidden && !p.disabled) {
    const show = el('button', 'btn show', t('layout.show'));
    show.type = 'button';
    show.addEventListener('click', () => showId(p.id));
    actions.append(show);
  }
  const verb = p.disabled ? 'enable' : 'disable';
  const toggle = catalogButton(t(`catalog.${verb}`), t(`catalog.${verb}.confirm`),
    `/api/plugins/${encodeURIComponent(p.id)}/${verb}`, () => t(`catalog.${verb}d.note`, { name }), p.id);
  toggle.classList.add('catalog-toggle');
  const trash = catalogOnce(t('catalog.trash'), `/api/plugins/${encodeURIComponent(p.id)}/trash`, () => t('catalog.trashed.note', { name }), p.id);
  trash.classList.add('trash');
  const options = [];
  if (!hidden && !p.disabled) options.push(hideButton());
  if (p.disabled) actions.append(toggle);
  else options.push(toggle);
  options.push(trash);
  actions.append(moreActions(name, options));
  item.append(identity, features, status, actions);
  const detail = p.error ? explain(p.error) : p.broken ? t('catalog.state.broken') : p.disabled && p.loaded ? t('catalog.disablingHint') : !p.disabled && !p.loaded ? t('catalog.pendingHint') : '';
  if (detail) item.append(el('p', 'catalog-state-detail', detail));
  if (p.error?.file) item.append(errorFile(p.error.file));
  item.append(pageEdit(p));
  return item;
}

// trashItem is one folder in the trash: restore puts it back enabled, and
// purge deletes it on a second click.
function trashItem(p) {
  const item = catalogCard(p);
  const name = pick(p.name);
  const actions = el('div', 'catalog-actions');
  const restore = catalogOnce(
    t('catalog.restore'),
    `/api/trash/${encodeURIComponent(p.folder)}/restore`,
    () => t('catalog.restored.note', { name }),
    p.id,
  );
  restore.classList.add('restore');
  const purge = catalogButton(
    t('catalog.purge'),
    t('catalog.purge.confirm'),
    `/api/trash/${encodeURIComponent(p.folder)}/purge`,
    () => t('catalog.purged.note', { name }),
    p.id,
  );
  purge.classList.add('purge');
  item.querySelector('.card-head').append(moreActions(name, [purge]));
  actions.append(restore);
  item.append(actions);
  return item;
}

// labeled is one labelled field of the page editor.
function labeled(name, label, value) {
  const wrap = el('label', 'edit-field');
  const input = el('input');
  input.name = name;
  input.value = value;
  input.autocomplete = 'off';
  input.spellcheck = false;
  wrap.append(el('span', null, label), input);
  return wrap;
}

// pageEdit is the form that saves a page's name, icon and address once.
// It has no field a command could be written into.
function pageEdit(p) {
  const details = el('details', 'page-edit');
  const form = el('form', 'page-edit-form');
  form.append(labeled('name', t('page.name'), pick(p.name)), labeled('icon', t('page.icon'), p.icon || ''));
  form.elements.name.required = true;
  if (p.page) {
    const kind = el('select');
    kind.name = 'kind';
    for (const [value, key] of [['proxy', 'page.embed'], ['link', 'page.link']]) {
      const option = el('option', null, t(key));
      option.value = value;
      kind.append(option);
    }
    kind.value = p.link ? 'link' : 'proxy';
    const address = labeled('address', t('page.address'), p.proxy || p.link || '');
    address.querySelector('input').required = true;
    const mode = el('label', 'edit-field');
    mode.append(el('span', null, t('page.mode')), kind);
    form.append(mode, address);
  }
  if (p.cardUrl) form.append(labeled('cardUrl', t('page.card'), p.cardUrl));
  const save = el('button', 'btn', t('page.save'));
  save.type = 'submit';
  form.append(save);
  form.addEventListener('submit', (e) => savePage(e, p.id));
  details.append(el('summary', null, t('page.edit')), form);
  return details;
}

// savePage writes the form once. A failure leaves what was typed; success
// is a note on that plugin's card.
async function savePage(e, id) {
  e.preventDefault();
  const form = e.target;
  const body = { name: form.elements.name.value, icon: form.elements.icon.value };
  if (form.elements.kind) body[form.elements.kind.value] = form.elements.address.value.trim();
  if (form.elements.cardUrl) body.cardUrl = form.elements.cardUrl.value.trim();
  const btn = form.querySelector('button');
  btn.disabled = true;
  catalogNoteFor = id;
  const note = $('catalog-note');
  try {
    const resp = await fetch(`/api/plugins/${encodeURIComponent(id)}/page`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const got = await resp.json().catch(() => ({}));
    if (!resp.ok) throw got.error || { code: '', detail: `HTTP ${resp.status}` };
    feedback(note, t('page.saved'));
    await loadPlugins();
    render();
    refreshStatus();
    await loadCatalog();
  } catch (err) {
    feedback(note, explain(err), true);
    const card = form.closest('.catalog-item');
    if (card) card.insertBefore(note, card.firstChild);
    btn.disabled = false;
  }
}

function availableItem(p) {
  const item = catalogCard(p);
  const offers = providesList(p);
  if (offers) item.append(offers);
  const btn = catalogButton(
    t('catalog.add'),
    t('catalog.add.confirm'),
    `/api/catalog/${encodeURIComponent(p.id)}`,
    (body) => t('catalog.added', { path: body.path }),
    p.id,
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
  closePanel(true);
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

// The sidebar is read at start and after the plugins page changed the plugins.
// A page whose entry went away or changed is unloaded.
async function loadPlugins() {
  let next;
  try {
    next = await getJSON('/api/plugins');
  } catch (e) {
    failure = e.message;
    return;
  }
  for (const [id, f] of frames) {
    const before = JSON.stringify(plugins.find((p) => p.id === id));
    if (JSON.stringify(next.find((p) => p.id === id)) === before) continue;
    f.remove();
    frames.delete(id);
    hiddenSince.delete(id);
  }
  plugins = next;
}

// A card still being read is asked for again shortly, for as long as the
// overview is shown.
function flushOverview() {
  if (!pendingOverview || $('cards').contains(document.activeElement) || (openPanel && $('cards').contains(openPanel.trigger))) return;
  overview = pendingOverview;
  pendingOverview = null;
  render();
}

async function loadCards() {
  clearTimeout(cardsTimer);
  $('cards').setAttribute('aria-busy', 'true');
  $('overview-loading').hidden = !!overview;
  try {
    const next = await getJSON('/api/cards');
    if ($('cards').contains(document.activeElement) || (openPanel && $('cards').contains(openPanel.trigger))) pendingOverview = next;
    else overview = next;
    failure = null;
  } catch (e) {
    failure = e.message;
  } finally {
    $('cards').setAttribute('aria-busy', 'false');
    $('overview-loading').hidden = true;
  }
  render();
  const waiting = (pendingOverview || overview)?.cards.some((c) => c.pending || c.error);
  if (!current && !catalogOpen && waiting) cardsTimer = setTimeout(loadCards, PENDING_AGAIN);
}

function stamp(card, c) {
  if (!c.updated) return;
  const time = new Intl.DateTimeFormat(lang, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(c.updated));
  card.append(el('p', 'card-updated', t('card.updated', { time })));
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
  $('service-more').append(moreActions('hostdeck', [$('service-clear')]));
  $('service-clear').addEventListener('click', onClearClick);
  $('page-form').addEventListener('submit', onPageSubmit);
  $('page-kind').addEventListener('change', pageHint);
  $('catalog-add-open').addEventListener('click', focusCatalogAdd);
  $('empty-add').addEventListener('click', (e) => {
    if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) catalogAddRequested = true;
  });
  // A URI-list from one of our own links is not a request to add a page.
  document.addEventListener('dragstart', (e) => e.dataTransfer?.setData(INTERNAL_DRAG, '1'));
  document.addEventListener('dragover', onDragOver);
  document.addEventListener('drop', onDrop);
  $('catalog-search').addEventListener('input', filterCatalog);
  $('layout-undo').addEventListener('click', undoLayout);
  for (const event of ['pointerenter', 'focusin']) {
    $('layout-note').addEventListener(event, () => clearTimeout(layoutNoteTimer));
  }
  for (const event of ['pointerleave', 'focusout']) {
    $('layout-note').addEventListener(event, scheduleLayoutNote);
  }
  $('layout-reset').addEventListener('click', resetLayout);
  $('refresh').addEventListener('click', async () => {
    const refresh = $('refresh');
    refresh.disabled = true;
    refresh.textContent = t('overview.refreshing');
    refresh.dataset.i18n = 'overview.refreshing';
    try {
      const resp = await fetch('/api/cards/refresh', { method: 'POST' });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      await loadCards();
    } catch (e) {
      failure = e.message;
      render();
    } finally {
      refresh.disabled = false;
      refresh.dataset.i18n = 'overview.refresh';
      refresh.textContent = t('overview.refresh');
    }
  });
  document.addEventListener('focusout', () => setTimeout(flushOverview, 0));
  $('menu').addEventListener('click', () => setNavigation(!document.querySelector('.app').classList.contains('nav-open')));
  $('nav-backdrop').addEventListener('click', () => setNavigation(false, true));
  document.querySelector('.side-menu').addEventListener('click', (e) => {
    if (e.target.closest('a')) setNavigation(false, true);
  });
  document.addEventListener('keydown', onInterfaceKey);
  document.addEventListener('click', (e) => {
    if (openPanel && !e.composedPath().includes(openPanel.panel) && !e.composedPath().includes(openPanel.trigger)) closePanel();
  });
  narrow.addEventListener('change', () => {
    setNavigation(document.querySelector('.app').classList.contains('nav-open'));
    positionPanel();
  });
  addEventListener('resize', positionPanel);
  document.addEventListener('scroll', (e) => {
    if (openPanel && !openPanel.panel.contains(e.target)) positionPanel();
  }, true);
  setNavigation(false);
  sortable($('nav'), '.nav-row', (order, previous) => {
    plugins.sort(byOrder(order));
    $('nav').dataset.entries = JSON.stringify(plugins.map((p) => [p.id, p.type, p.url]));
    placeMoves($('nav'), '.nav-row');
    saveLayout({ sidebar: order }, { sidebar: previous });
  });
  sortable($('cards'), '.card', (order, previous) => {
    overview.cards.sort(byOrder(order));
    placeMoves($('cards'), '.card');
    saveLayout({ cards: order }, { cards: previous });
  });
  $('service-reload').addEventListener('click', () => {
    if (!current || !frames.has(current.id)) return;
    if (current.type === 'detail') loadDetail(current, frames.get(current.id));
    else frames.get(current.id).src = current.url;
  });
  await setLang(initialLang(), false);
  await loadPlugins();
  await loadLayout();
  addEventListener('hashchange', route);
  route();
  refreshStatus();
  setInterval(refreshStatus, STATUS_EVERY);
  setInterval(releaseIdleFrames, 60_000);
  document.addEventListener('visibilitychange', refreshStatus);
}

await init();
