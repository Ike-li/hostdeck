import { $, el } from './dom.js';
import { getJSON } from './api.js';
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
  const noCards = !state.overview || cards.length === 0;
  $('empty').hidden = !state.overview || !noCards;
  if (state.overview && noCards) {
    $('empty').textContent = state.stored.hidden.length ? t('overview.hidden') : t('overview.empty', { dir: state.overview.pluginDir });
  }
  // The cards are rebuilt only when they are read or rearranged or the
  // language changes: a periodic render would swallow a click on a card's
  // button that lands while the card is replaced.
  if (!state.overview || state.overview.lang === state.lang) {
    placeMoves($('cards'), '.card');
    return;
  }
  state.overview.lang = state.lang;
  $('cards').replaceChildren(...cards.map((c) => cardElement(c)), $('empty-add'));
  placeMoves($('cards'), '.card');
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
  if (!standalone) head.append(moreActions(t('more.for', { name: pick(c.name) }), [moveButton('up'), moveButton('down'), hideButton(), width]));
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
      li.append(el('span', 'row-dot'), el('span', 'row-name', pick(r.name)), el('span', 'row-value', r.value));
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
  const time = new Intl.DateTimeFormat(state.lang, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(c.updated));
  card.append(el('p', 'card-updated', t('card.updated', { time })));
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
  } catch (e) {
    state.failure = e.message;
    callRender();
  } finally {
    setStaticText(refresh, 'overview.refresh');
    refresh.disabled = false;
  }
}
