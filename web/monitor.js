import { $, el, feedback, announce } from './dom.js';
import { getJSON, postJSON } from './api.js';
import { t, pick, explain } from './i18n.js';
import { twoClicks } from './confirm.js';
import { cardElement } from './overview.js';
import { scheduledTaskTools } from './tasks.js';
import { PENDING_AGAIN, state } from './state.js';

let monitorTimer = null;

export function stopMonitorRetry() {
  clearTimeout(monitorTimer);
}

export async function loadMonitor(p, view, refresh = false) {
  if (!view || view.getAttribute('aria-busy') === 'true') return;
  clearTimeout(monitorTimer);
  view.setAttribute('aria-busy', 'true');
  if (state.current?.id === p.id) $('service-reload').disabled = true;
  try {
    if (refresh) {
      const resp = await fetch('/api/cards/refresh', { method: 'POST' });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    }
    view.data = await getJSON(`/api/cards/${encodeURIComponent(p.id)}`);
  } catch (e) {
    view.data = { id: p.id, name: p.name, icon: p.icon, error: { code: 'backend', detail: e.message } };
  } finally {
    view.setAttribute('aria-busy', 'false');
  }
  view.lang = state.lang;
  view.replaceChildren(cardElement(view.data, true));
  if (state.current?.id !== p.id || !view.isConnected) return;
  $('service-reload').disabled = false;
  if (view.data.pending || view.data.error) monitorTimer = setTimeout(() => loadMonitor(p, view), PENDING_AGAIN);
}

// A detail page is drawn here from the items its plugin's command prints. It
// is rebuilt only when it is loaded or the language changes, so that a
// periodic render does not reset an action button waiting for its second click.
export async function loadDetail(p, view) {
  if (!view.openLogs) view.openLogs = new Set();
  if (!view.detailDisarm) {
    view.detailDisarm = true;
    view.addEventListener('disarm', () => {
      if (view.detailStale && !view.querySelector('.action.armed')) renderDetail(view.detailPlugin, view);
    });
  }
  view.detailPlugin = p;
  view.setAttribute('aria-busy', 'true');
  if (state.current?.id === p.id) $('service-reload').disabled = true;
  try {
    view.data = await getJSON(`/api/detail/${encodeURIComponent(p.id)}`);
  } catch (e) {
    view.data = { error: { code: 'backend', detail: e.message } };
  } finally {
    view.setAttribute('aria-busy', 'false');
    if (state.current?.id === p.id) $('service-reload').disabled = false;
  }
  renderDetail(p, view);
}

export function renderDetail(p, view) {
  view.detailPlugin = p;
  view.lang = state.lang;
  // A reading can arrive between the two clicks that confirm an action.
  // Replacing the button would forget the first click.
  if (view.querySelector('.action.armed')) {
    view.detailStale = true;
    return;
  }
  view.detailStale = false;
  view.scriptFiles = null;
  view.classList.toggle('detail-compact', view.data.presentation === 'compact');
  const { items, error, groups = [] } = view.data;
  view.querySelector('.detail-tools')?.remove();
  const list = view.querySelector('.detail-list');
  if (error) {
    const note = el('p', 'banner', explain(error));
    note.setAttribute('role', 'alert');
    list.replaceChildren(note);
  } else if (!items?.length) {
    if (groups.length) renderDetailTools(p, view);
    else list.replaceChildren(el('p', 'empty', t('detail.empty')));
  } else {
    if (groups.length) renderDetailTools(p, view);
    else list.replaceChildren(...items.map((item) => detailItem(p, item, view)));
  }
}

function renderDetailTools(p, view) {
  const choice = state.detailFilters.get(p.id) || { search: '', group: 'all' };
  state.detailFilters.set(p.id, choice);
  const tools = el('div', 'detail-tools');
  const form = el('div', 'detail-search');
  const label = el('label', 'edit-field', t('detail.search'));
  const input = el('input');
  input.type = 'search';
  input.value = choice.search;
  input.placeholder = t('detail.searchHint');
  input.autocomplete = 'off';
  input.spellcheck = false;
  label.append(input);
  const clear = el('button', 'btn detail-clear', t('detail.clear'));
  clear.type = 'button';
  form.append(label, clear);
  const filters = el('div', 'detail-groups');
  filters.setAttribute('role', 'group');
  filters.setAttribute('aria-label', t('detail.filter'));
  tools.append(form, filters);
  view.querySelector('.detail-list').before(tools);
  let update = () => {};
  const tasksUI = scheduledTaskTools(p, view, tools, choice, () => update());
  update = () => {
    if (tasksUI && choice.segment === 'scripts') {
      tasksUI.mark('scripts');
      return tasksUI.paint();
    }
    if (tasksUI) tasksUI.mark('tasks');
    filters.hidden = false;
    const items = view.data.items || [];
    const groups = [{ id: 'all', label: t('detail.all') }, ...view.data.groups];
    if (!groups.some(g => g.id === choice.group)) choice.group = 'all';
    const query = choice.search.trim().toLocaleLowerCase();
    const matched = items.filter(it => `${pick(it.name)}\n${it.id}`.toLocaleLowerCase().includes(query));
    const selected = matched.filter(it => choice.group === 'all' || it.group === choice.group);
    const severity = (it) => ({ error: 2, warn: 1 }[it.status] || 0);
    selected.sort((a, b) => (severity(b) - severity(a)) || (groups.findIndex(g => g.id === a.group) - groups.findIndex(g => g.id === b.group)) || pick(a.name).localeCompare(pick(b.name), state.lang, { sensitivity: 'base' }));
    for (const g of groups) {
      let button = [...filters.children].find(b => b.dataset.group === g.id);
      if (!button) {
        button = el('button', 'btn');
        button.type = 'button';
        button.dataset.group = g.id;
        button.addEventListener('click', () => { choice.group = g.id; update(); });
        filters.append(button);
      }
      const count = g.id === 'all' ? matched.length : matched.filter(it => it.group === g.id).length;
      button.textContent = `${pick(g.label)} (${count})`;
      button.setAttribute('aria-pressed', String(choice.group === g.id));
    }
    clear.disabled = !choice.search;
    const list = view.querySelector('.detail-list');
    if (selected.length) list.replaceChildren(...selected.map(it => detailItem(p, it, view)));
    else list.replaceChildren(el('p', 'empty', !items.length ? (view.data.note ? pick(view.data.note) : t('detail.empty')) : t(query ? 'detail.noMatches' : 'detail.groupEmpty')));
    if (state.current?.id === p.id) announce(t('detail.matches', { count: selected.length, total: items.length }));
  };
  input.addEventListener('input', () => { choice.search = input.value; update(); });
  clear.addEventListener('click', () => { choice.search = input.value = ''; update(); input.focus(); });
  update();
}

function detailItem(p, item, view) {
  const section = el('section', 'detail-item');
  if (item.status) section.dataset.status = item.status;
  const head = el('header', 'detail-head');
  head.append(el('h2', 'detail-name', pick(item.name)));
  const compact = view.data.presentation === 'compact';
  if (compact && item.value != null || item.status) head.append(el('span', 'pill detail-status', compact && item.value != null ? pick(item.value) : t(`card.status.${item.status}`)));
  section.append(head);
  if (!compact && item.value !== undefined && item.value !== null) {
    const figure = el('p', 'detail-figure');
    figure.append(el('span', 'detail-value', pick(item.value)));
    const caption = pick(item.label).trim();
    if (caption) figure.append(el('span', 'detail-label', caption));
    section.append(figure);
  }
  if (item.fields?.length) {
    const fields = el('dl', 'detail-fields');
    for (const f of item.fields) fields.append(el('dt', null, pick(f.name)), el('dd', null, pick(f.value)));
    section.append(fields);
  }
  if (item.log || item.logAvailable) section.append(detailLog(p, item, view));
  if (item.actions?.length) {
    const actions = el('div', 'detail-actions');
    actions.append(...item.actions.map((action) => actionButton(p, item, action, view)));
    (compact ? head : section).append(actions);
  }
  return section;
}

function detailLog(p, item, view) {
  const log = el('details', 'detail-log');
  const content = el('pre', null, item.log || '');
  content.tabIndex = 0;
  content.setAttribute('aria-label', t('detail.log'));
  log.append(el('summary', null, t('detail.log')), content);
  log.addEventListener('toggle', () => {
    if (!view.openLogs) view.openLogs = new Set();
    if (log.open) {
      view.openLogs.add(item.id);
      ensureLog(p, item, log, view);
    } else view.openLogs.delete(item.id);
  });
  if (view.openLogs?.has(item.id)) {
    log.open = true;
    ensureLog(p, item, log, view);
  }
  return log;
}

// ensureLog fills a log that the list left out. A failed read stays in this
// box, so it cannot replace the list the page is already showing.
async function ensureLog(p, item, log, view) {
  if (log.dataset.loaded === 'yes' || log.dataset.loaded === 'pending') return;
  const pre = log.querySelector('pre');
  if (item.log) {
    pre.textContent = item.log;
    log.dataset.loaded = 'yes';
    requestAnimationFrame(() => { pre.scrollTop = pre.scrollHeight; });
    return;
  }
  log.dataset.loaded = 'pending';
  pre.textContent = t('loading');
  try {
    const data = await getJSON(`/api/detail/${encodeURIComponent(p.id)}?item=${encodeURIComponent(item.id)}`);
    if (!log.isConnected) return;
    if (data.error) throw data.error;
    const fresh = (data.items || []).find((it) => it.id === item.id);
    if (!fresh) throw { code: 'command_output' };
    const stored = (view.data.items || []).find((it) => it.id === item.id);
    if (stored) {
      stored.log = fresh.log || '';
      if (fresh.fields) stored.fields = fresh.fields;
      replaceFields(log.closest('.detail-item'), stored);
    }
    pre.textContent = fresh.log || t('detail.logEmpty');
    log.dataset.loaded = 'yes';
    requestAnimationFrame(() => { pre.scrollTop = pre.scrollHeight; });
  } catch (err) {
    if (!log.isConnected) return;
    delete log.dataset.loaded;
    pre.textContent = err instanceof Error ? t('detail.logFailed') : explain(err);
  }
}

function replaceFields(section, item) {
  if (!section) return;
  const fields = el('dl', 'detail-fields');
  for (const f of item.fields || []) fields.append(el('dt', null, pick(f.name)), el('dd', null, pick(f.value)));
  const old = section.querySelector('.detail-fields');
  if (!item.fields?.length) {
    old?.remove();
    return;
  }
  if (old) old.replaceWith(fields);
  else section.querySelector('.detail-log')?.before(fields);
}

export function actionButton(p, item, action, view) {
  const label = pick(action.label);
  const btn = el('button', 'btn action', label);
  btn.type = 'button';
  twoClicks(btn, t('action.confirm', { label }), async () => {
    btn.disabled = true;
    btn.textContent = t('action.running');
    let note;
    let failed = false;
    try {
      const body = await postJSON(`/api/action/${encodeURIComponent(p.id)}`, { item: item.id, action: action.id });
      note = body.message ? pick(body.message) : t('action.done', { label, name: pick(item.name) });
    } catch (err) {
      note = err instanceof TypeError ? t('action.interrupted') : explain(err);
      failed = true;
    }
    showNote(view, note, failed);
    if (!failed) {
      if (!view.openLogs) view.openLogs = new Set();
      view.openLogs.add(item.id);
    }
    await loadDetail(p, view);
  });
  return btn;
}

export function showNote(view, text, failed = false) {
  const note = view.querySelector('.detail-note');
  feedback(note, text, failed);
  clearTimeout(note.timer);
  note.timer = setTimeout(() => {
    note.hidden = true;
  }, 8000);
}
