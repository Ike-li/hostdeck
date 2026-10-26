import { $, el, feedback, announce } from './dom.js';
import { getJSON, postJSON } from './api.js';
import { t, pick, explain } from './i18n.js';
import { twoClicks } from './confirm.js';
import { cardElement } from './overview.js';
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
  view.lang = state.lang;
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
  const update = () => {
    const items = view.data.items || [];
    const groups = [{ id: 'all', label: t('detail.all') }, ...view.data.groups];
    if (!groups.some(g => g.id === choice.group)) choice.group = 'all';
    const query = choice.search.trim().toLocaleLowerCase();
    const matched = items.filter(it => `${pick(it.name)}\n${it.id}`.toLocaleLowerCase().includes(query));
    const selected = matched.filter(it => choice.group === 'all' || it.group === choice.group);
    selected.sort((a, b) => (Number(['warn', 'error'].includes(b.status)) - Number(['warn', 'error'].includes(a.status))) || (groups.findIndex(g => g.id === a.group) - groups.findIndex(g => g.id === b.group)) || pick(a.name).localeCompare(pick(b.name), state.lang, { sensitivity: 'base' }));
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
    else list.replaceChildren(el('p', 'empty', t(!items.length ? 'detail.empty' : query ? 'detail.noMatches' : 'detail.groupEmpty')));
    announce(t('detail.matches', { count: selected.length, total: items.length }));
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
  if (item.status) head.append(el('span', 'pill detail-status', compact && item.value != null ? item.value : t(`card.status.${item.status}`)));
  section.append(head);
  if (!compact && item.value !== undefined && item.value !== null) section.append(el('p', 'detail-value', item.value));
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
    (compact ? head : section).append(actions);
  }
  return section;
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
    setTimeout(() => loadDetail(p, view), 1500);
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
