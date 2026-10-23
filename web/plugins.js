import { $, el } from './dom.js';
import { getJSON } from './api.js';
import { t, pick } from './i18n.js';
import { moreActions } from './panel.js';
import { byOrder, moveButton, hideButton, placeMoves, saveLayout } from './layout.js';
import { loadMonitor } from './monitor.js';
import { callRender } from './hooks.js';
import { state } from './state.js';

// The sidebar is read at start and after the plugins page changed the plugins.
// A page whose entry went away or changed is unloaded.
export async function loadPlugins() {
  let next;
  try {
    next = await getJSON('/api/plugins');
  } catch (e) {
    state.failure = e.message;
    return;
  }
  for (const [id, f] of state.frames) {
    const before = JSON.stringify(state.plugins.find((p) => p.id === id));
    if (JSON.stringify(next.find((p) => p.id === id)) === before) continue;
    f.remove();
    state.frames.delete(id);
    state.hiddenSince.delete(id);
  }
  state.plugins = next;
}

// The sidebar is built once and then updated in place: rebuilding it on every
// status refresh would drop a click that lands while it is replaced.
export function renderNav() {
  const nav = $('nav');
  const entries = JSON.stringify(state.plugins.map((p) => [p.id, p.type, p.url]));
  if (nav.dataset.entries !== entries) {
    nav.dataset.entries = entries;
    nav.replaceChildren(...state.plugins.map(navItem));
  }
  const overviewItem = $('nav-overview');
  const catalogItem = $('nav-catalog');
  overviewItem.querySelector('.nav-name').textContent = t('nav.overview');
  overviewItem.classList.toggle('active', !state.current && !state.catalogOpen);
  catalogItem.querySelector('.nav-name').textContent = t('nav.catalog');
  catalogItem.classList.toggle('active', state.catalogOpen);
  [...nav.querySelectorAll('.nav-row')].forEach((row, i) => {
    const p = state.plugins[i];
    const a = row.querySelector('.nav-item');
    a.querySelector('.nav-name').textContent = pick(p.name);
    a.querySelector('.icon').textContent = iconOf(p);
    a.classList.toggle('active', state.current?.id === p.id);
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
  const moves = moreActions(t('more.for', { name: pick(p.name) }), [moveButton('up'), moveButton('down'), hideButton()]);
  row.append(a, moves);
  return row;
}

export function onSidebarMoved(order, previous) {
  state.plugins.sort(byOrder(order));
  $('nav').dataset.entries = JSON.stringify(state.plugins.map((p) => [p.id, p.type, p.url]));
  placeMoves($('nav'), '.nav-row');
  saveLayout({ sidebar: order }, { sidebar: previous });
}

export const iconOf = (p) => p.icon || pick(p.name).slice(0, 1);

export function paintDot(d, p) {
  const s = p.error ? 'down' : state.status[p.id];
  if (s) d.dataset.status = s;
  else delete d.dataset.status;
  d.title = s ? t(`status.${s}`) : t('status.unknown');
  d.setAttribute('role', 'img');
  d.setAttribute('aria-label', d.title);
}

export async function refreshStatus() {
  if (document.hidden) return;
  try {
    state.status = await getJSON('/api/status');
  } catch {
    return; // the banner of the next failed request says why
  }
  callRender();
  if (state.current?.type === 'card' && !state.current.error) loadMonitor(state.current, state.frames.get(state.current.id));
}
