import { $, el } from './dom.js';
import { getJSON } from './api.js';
import { closePanel } from './panel.js';
import { callRender, callReloadShown } from './hooks.js';
import { t } from './i18n.js';
import { LAYOUT_NOTE_FOR, state } from './state.js';

// Everything about the saved layout: reading it, writing the lists that
// changed, undoing them, and the row controls that drive them.
export const byOrder = (order) => (a, b) => order.indexOf(a.id) - order.indexOf(b.id);

let layoutNoteTimer = null;

export function applyStored(l) {
  state.stored = {
    sidebar: l.sidebar ?? [],
    cards: l.cards ?? [],
    wide: l.wide ?? [],
    hidden: l.hidden ?? [],
  };
  state.layoutKnown = true;
}

export function dismissLayoutNote() {
  clearTimeout(layoutNoteTimer);
  state.layoutUndo = null;
  $('layout-note').hidden = true;
}

// pauseLayoutNote holds the notice open while the pointer or the keyboard is
// on it; scheduleLayoutNote starts the idle countdown again.
export function pauseLayoutNote() {
  clearTimeout(layoutNoteTimer);
}

export function scheduleLayoutNote() {
  clearTimeout(layoutNoteTimer);
  if (!state.layoutUndo || state.savingLayout) return;
  layoutNoteTimer = setTimeout(() => {
    const note = $('layout-note');
    if (note.matches(':hover') || note.contains(document.activeElement)) return;
    dismissLayoutNote();
  }, LAYOUT_NOTE_FOR);
}

// saveLayout sends the lists of the layout that changed; the others stay.
// The answer is the whole layout, so the next hide can send every hidden ID.
export async function saveLayout(change, previous) {
  clearTimeout(layoutNoteTimer);
  state.savingLayout = true;
  state.pendingOverview = null;
  const epoch = ++state.layoutEpoch;
  // The undo is offered before the answer: the page already shows the change,
  // and a slower answer must not put an older change back in its place.
  if (previous && JSON.stringify(change) !== JSON.stringify(previous)) {
    state.layoutUndo = previous;
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
    if (epoch !== state.layoutEpoch) return false;
    applyStored(saved);
    state.savingLayout = false;
    scheduleLayoutNote();
    return true;
  } catch (e) {
    if (epoch !== state.layoutEpoch) return false;
    state.savingLayout = false;
    if (state.layoutUndo === previous) dismissLayoutNote();
    // The server keeps its last saved layout when a write fails. Read that
    // state before showing the error, so an optimistic hide or move is undone.
    try {
      const [saved, shown, cards] = await Promise.all([
        getJSON('/api/layout'), getJSON('/api/plugins'), getJSON('/api/cards'),
      ]);
      if (epoch !== state.layoutEpoch) return false;
      applyStored(saved);
      state.plugins = shown;
      state.overview = cards;
      $('nav').dataset.entries = '';
      if (state.catalog) state.catalog.lang = null;
    } catch {
      // Keep the original write failure when recovery cannot reach the server.
    }
    if (epoch !== state.layoutEpoch) return false;
    state.failure = e.message;
    callRender();
    return false;
  }
}

export async function undoLayout() {
  const previous = state.layoutUndo;
  if (!previous) return;
  dismissLayoutNote();
  const hiddenBefore = state.stored.hidden.slice();
  if (previous.sidebar) {
    state.plugins.sort(byOrder(previous.sidebar));
    $('nav').dataset.entries = '';
  }
  if (state.overview && previous.cards) {
    state.overview.cards.sort(byOrder(previous.cards));
    state.overview.lang = null;
  }
  if (state.overview && previous.wide) {
    const wide = new Set(previous.wide);
    for (const c of state.overview.cards) c.wide = wide.has(c.id);
    state.overview.lang = null;
  }
  callRender();
  await saveLayout(previous);
  if (previous.hidden && JSON.stringify(previous.hidden) !== JSON.stringify(hiddenBefore)) await callReloadShown();
}

// resetLayout clears the order, the widths and what is hidden.
export async function resetLayout() {
  if (!state.layoutKnown) return;
  const previous = {
    sidebar: state.stored.sidebar.slice(),
    cards: state.stored.cards.slice(),
    wide: state.stored.wide.slice(),
    hidden: state.stored.hidden.slice(),
  };
  if (!previous.sidebar.length && !previous.cards.length && !previous.wide.length && !previous.hidden.length) return;
  if (!(await saveLayout({ reset: true }, previous))) return;
  await callReloadShown();
}

// hideId saves the whole hidden list. The plugin's place in the order stays.
export function hideId(id) {
  closePanel();
  if (!state.layoutKnown || state.stored.hidden.includes(id)) return;
  const previous = { hidden: state.stored.hidden.slice() };
  state.stored.hidden = [...state.stored.hidden, id];
  state.plugins = state.plugins.filter((p) => p.id !== id);
  $('nav').dataset.entries = '';
  if (state.overview) {
    state.overview.cards = state.overview.cards.filter((c) => c.id !== id);
    state.overview.lang = null;
  }
  if (state.catalog) state.catalog.lang = null;
  const leaving = state.current?.id === id;
  if (leaving) state.current = null;
  callRender();
  saveLayout({ hidden: state.stored.hidden.slice() }, previous).then((ok) => {
    if (ok && leaving) location.hash = '#/';
  });
}

// showId puts a hidden plugin back in the place the layout kept for it.
export async function showId(id) {
  if (!state.layoutKnown || !state.stored.hidden.includes(id)) return;
  const previous = { hidden: state.stored.hidden.slice() };
  state.stored.hidden = state.stored.hidden.filter((x) => x !== id);
  if (!(await saveLayout({ hidden: state.stored.hidden.slice() }, previous))) return;
  await callReloadShown();
}

// moveButton steps its sidebar row or card by one. The button stays outside
// the link, so Tab reaches it and activating it does not open the entry.
export function moveButton(dir) {
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
export function hideButton() {
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

export function placeMoves(list, selector) {
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

export function moveItem(which, id, dir, btn) {
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
    const previous = state.plugins.map((p) => p.id);
    state.plugins.sort(byOrder(order));
    $('nav').dataset.entries = JSON.stringify(state.plugins.map((p) => [p.id, p.type, p.url]));
    saveLayout({ sidebar: order }, { sidebar: previous });
    return;
  }
  const previous = state.overview.cards.map((c) => c.id);
  state.overview.cards.sort(byOrder(order));
  saveLayout({ cards: order }, { cards: previous });
}

export async function loadLayout() {
  try {
    applyStored(await getJSON('/api/layout'));
  } catch (e) {
    state.layoutKnown = false;
    state.failure = e.message;
  }
}
