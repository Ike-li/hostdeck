import { $, announce, el, feedback } from './dom.js';
import { getJSON, postJSON } from './api.js';
import { t, pick, explain, setStaticText } from './i18n.js';
import { closePanel, moreActions, openPanelTrigger } from './panel.js';
import { byOrder, hideButton, moveButton, placeMoves, saveLayout } from './layout.js';
import { errorFile } from './frag.js';
import { callRender } from './hooks.js';
import { PENDING_AGAIN, state } from './state.js';

let cardsTimer = null;

export function stopCardsRetry() {
  clearTimeout(cardsTimer);
}

export function renderOverview() {
  document.title = 'hostdeck';
  const cards = state.overview ? state.overview.cards : [];
  // A first reading that never arrived is not an empty overview, and must not
  // read like one: the two ask for different things of the reader. While the
  // first reading is still on its way there is nothing to say either.
  const failed = !state.overview && !!state.failure;
  const empty = !!state.overview && cards.length === 0;
  $('empty').hidden = !failed && !empty;
  if (failed) $('empty').textContent = t('overview.unavailable', { detail: state.failure });
  else if (empty) {
    $('empty').textContent = state.stored.hidden.length ? t('overview.hidden') : t('overview.empty', { dir: state.overview.pluginDir });
  }
  announce(failed || empty ? $('empty').textContent : t('overview.loaded', { n: cards.length }));
  // The cards are rebuilt only when they are read or rearranged or the
  // language changes: a periodic render would swallow a click on a card's
  // button that lands while the card is replaced.
  if (state.overview && state.overview.lang !== state.lang) {
    state.overview.lang = state.lang;
    $('cards').replaceChildren(...cards.map((c) => cardElement(c)), $('empty-add'));
  }
  placeMoves($('cards'), '.card');
  showNotifications();
}

export function cardElement(c, standalone = false) {
  const card = el('article', 'card');
  card.dataset.id = c.id;
  card.draggable = !standalone;
  card.classList.toggle('wide', !!c.wide);
  const head = el('header', 'card-head');
  const width = el('button', 'card-width', t(c.wide ? 'card.narrow' : 'card.widen'));
  width.type = 'button';
  width.addEventListener('click', () => {
    const previous = state.overview.cards.filter((x) => x.wide).map((x) => x.id);
    closePanel();
    c.wide = !c.wide;
    state.overview.lang = null;
    saveLayout({ wide: state.overview.cards.filter((x) => x.wide).map((x) => x.id) }, { wide: previous });
    callRender();
  });
  head.append(el('span', 'icon', c.icon || pick(c.name).slice(0, 1)), standalone ? el('h2', 'card-name', pick(c.name)) : cardTitle(c));
  const controls = [moveButton('up'), moveButton('down'), hideButton(), width];
  if (c.watchable) controls.push(watchButton(c));
  if (!standalone) head.append(moreActions(t('more.for', { name: pick(c.name) }), controls));
  card.append(head);
  card.addEventListener('click', (e) => {
    if (state.suppressClick || e.target.closest('button, a, summary, .action-panel')) return;
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
  if (c.destination) {
    card.append(el('p', 'card-destination', t(`card.destination.${c.destination}`)));
    if (c.address) card.append(el('p', 'card-address', c.address));
    return card;
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
      li.append(el('span', 'row-dot'), el('span', 'row-name', pick(r.name)), el('span', 'row-value', pick(r.value)));
      rows.append(li);
    }
    card.append(rows);
  }
  stamp(card, c);
  return card;
}

// cardTitle names the card, and links it when the plugin is in the sidebar.
// Card-only plugins open the console's generated monitor view.
function cardTitle(c) {
  const name = el('h2', 'card-name');
  const page = state.plugins.find((p) => p.id === c.id);
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

function stamp(card, c) {
  if (!c.updated) return;
  card.append(el('p', 'card-updated', t('card.updated', { time: clock(c.updated) })));
}

function clock(when) {
  return new Intl.DateTimeFormat(state.lang, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(when));
}

// What the background checks of notifications.json know is read on its own
// schedule, and written into the cards already shown: they are not rebuilt.
export async function loadNotifications() {
  try {
    state.notifications = await getJSON('/api/notifications');
  } catch {
    return;
  }
  showNotifications();
}

function showNotifications() {
  const n = state.notifications;
  if (!n) return;
  say($('watch-error'), n.error ? t('overview.watch.invalid', { detail: n.error }) : '');
  say($('watch-missing'), n.missing?.length ? t('overview.watch.missing', { ids: n.missing.join(', ') }) : '');
  for (const card of $('cards').querySelectorAll('.card[data-id]')) {
    const w = n.cards[card.dataset.id];
    const toggle = card.querySelector('.watch-toggle');
    if (toggle) toggle.textContent = t(w ? 'card.unwatch' : 'card.watch');
    watchLine(card, 'card-watch', w && watching(w));
    watchLine(card, 'card-watch-failed', w?.failed && t('card.watch.failed', { time: clock(w.failed.at), detail: w.failed.detail }));
  }
}

function watchButton(c) {
  const btn = el('button', 'watch-toggle', t(state.notifications?.cards[c.id] ? 'card.unwatch' : 'card.watch'));
  btn.type = 'button';
  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    toggleWatch(c, btn.closest('.card'));
  });
  return btn;
}

// toggleWatch writes only the watch list of notifications.json; the result
// shows as the card's watch line, and a refusal is said on the card.
async function toggleWatch(c, card) {
  const on = !state.notifications?.cards[c.id];
  closePanel();
  try {
    state.notifications = await postJSON('/api/notifications/watch', { id: c.id, watch: on });
    card.querySelector('.card-note')?.remove();
    showNotifications();
    announce(t(on ? 'card.watching' : 'card.unwatched', { name: pick(c.name) }));
  } catch (error) {
    let note = card.querySelector('.card-note');
    if (!note) {
      note = el('p', 'card-note');
      card.querySelector('.card-head').after(note);
    }
    feedback(note, explain(error), true);
  }
}

function watching(w) {
  let words = w.checked ? t('card.watch.checked', { time: clock(w.checked) }) : t('card.watch.waiting');
  if (w.toldAt) words += t('card.watch.told', { status: t(`card.status.${w.told}`), time: clock(w.toldAt) });
  return words;
}

// Words are written only when they change, so an alert is not said again
// every time the status is read.
function say(node, words) {
  if (node.textContent !== words) node.textContent = words;
  node.hidden = !words;
}

function watchLine(card, className, words) {
  let line = card.querySelector(`.${className}`);
  if (!words) {
    line?.remove();
    return;
  }
  if (!line) {
    line = el('p', className);
    card.append(line);
  }
  if (line.textContent !== words) line.textContent = words;
}

export function onCardsMoved(order, previous) {
  state.overview.cards.sort(byOrder(order));
  placeMoves($('cards'), '.card');
  saveLayout({ cards: order }, { cards: previous });
}

// A card still being read is asked for again shortly, for as long as the
// overview is shown.
export function flushOverview() {
  const trigger = openPanelTrigger();
  if (!state.pendingOverview || $('cards').contains(document.activeElement) || (trigger && $('cards').contains(trigger))) return;
  state.overview = state.pendingOverview;
  state.pendingOverview = null;
  callRender();
}

export async function loadCards() {
  clearTimeout(cardsTimer);
  $('cards').setAttribute('aria-busy', 'true');
  $('overview-loading').hidden = !!state.overview;
  try {
    const next = await getJSON('/api/cards');
    const trigger = openPanelTrigger();
    if ($('cards').contains(document.activeElement) || (trigger && $('cards').contains(trigger))) state.pendingOverview = next;
    else state.overview = next;
    state.failure = null;
  } catch (e) {
    state.failure = e.message;
  } finally {
    $('cards').setAttribute('aria-busy', 'false');
    $('overview-loading').hidden = true;
  }
  callRender();
  const waiting = (state.pendingOverview || state.overview)?.cards.some((c) => c.pending || c.error);
  if (!state.current && !state.catalogOpen && waiting) cardsTimer = setTimeout(loadCards, PENDING_AGAIN);
}

export async function refreshOverview() {
  const refresh = $('refresh');
  refresh.disabled = true;
  setStaticText(refresh, 'overview.refreshing');
  try {
    const resp = await fetch('/api/cards/refresh', { method: 'POST' });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    await loadCards();
    announce(t('overview.refreshed'));
  } catch (e) {
    state.failure = e.message;
    callRender();
  } finally {
    setStaticText(refresh, 'overview.refresh');
    refresh.disabled = false;
  }
}
