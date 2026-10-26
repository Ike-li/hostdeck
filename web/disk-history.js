import { $, el, feedback } from './dom.js';
import { postJSON } from './api.js';
import { t, pick, explain } from './i18n.js';
import { state } from './state.js';
import { twoClicks } from './confirm.js';

const ranges = ['1h', '24h', '7d', '30d'];
const ns = 'http://www.w3.org/2000/svg';

function size(bytes, signed = false) {
  if (bytes == null) return '—';
  let value = Math.abs(bytes), unit = 0;
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  while (value >= 1000 && unit < units.length - 1) { value /= 1000; unit++; }
  const digits = unit === 0 || (value >= 100 && unit < 3) ? 0 : value >= 10 ? 1 : 2;
  return `${bytes < 0 ? '−' : signed && bytes > 0 ? '+' : ''}${value.toFixed(digits)} ${units[unit]}`;
}

function date(at, short = false) {
  if (!at) return '—';
  return new Intl.DateTimeFormat(state.lang, short ? { hour: '2-digit', minute: '2-digit' } : { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(at));
}

function button(label, key, click, className = 'btn') {
  const b = el('button', className, label);
  b.type = 'button'; b.dataset.key = key;
  b.addEventListener('click', click);
  return b;
}

function section(title, className) {
  const box = el('section', `disk-panel ${className}`);
  box.append(el('h2', null, title));
  return box;
}

export function stopDiskPolling() {
  for (const view of state.frames.values()) { clearTimeout(view.diskTimer); view.diskScheduled = false; }
}

export function resumeDiskPolling(p, view) { if (!view.diskScheduled) schedule(p, view); }

function schedule(p, view) {
  clearTimeout(view.diskTimer);
  if (state.current?.id !== p.id || !view.isConnected) return;
  view.diskScheduled = true;
  view.diskTimer = setTimeout(() => {
    view.diskScheduled = false;
    if (state.current?.id !== p.id || !view.isConnected) return;
    if (document.hidden || view.querySelector('.armed') || view.diskBusy) schedule(p, view);
    else loadDiskHistory(p, view, true);
  }, view.diskData?.running ? 2000 : 60000);
}

export async function loadDiskHistory(p, view, automatic = false) {
  clearTimeout(view.diskTimer);
  view.diskChoice ||= { target: '', path: '.', range: '24h', from: '', to: '', all: false };
  const choice = view.diskChoice;
  const request = (view.diskRequest || 0) + 1;
  view.diskRequest = request;
  view.setAttribute('aria-busy', 'true');
  if (state.current?.id === p.id) $('service-reload').disabled = true;
  try {
    const query = new URLSearchParams({ target: choice.target, path: choice.path, range: choice.range });
    if (choice.from) { query.set('from', choice.from); query.set('to', choice.to); }
    const response = await fetch(`/api/disk-history/${encodeURIComponent(p.id)}?${query}`, { cache: 'no-store' });
    const data = await response.json();
    if (!response.ok) throw data.error || { detail: `HTTP ${response.status}` };
    if (request !== view.diskRequest || !view.isConnected) return;
    view.diskData = data;
    choice.target = data.target;
    renderDiskHistory(p, view);
    const failure = view.querySelector('.disk-load-error');
    failure.hidden = true;
  } catch (error) {
    if (request !== view.diskRequest || !view.isConnected) return;
    const failure = view.querySelector('.disk-load-error');
    failure.replaceChildren(el('p', null, explain(error) || t('error.backend', { detail: error.message })), button(t('disk.resetView'), 'reset-view', () => {
      Object.assign(choice, { target: '', path: '.', from: '', to: '', all: false });
      loadDiskHistory(p, view);
    }));
    failure.hidden = false;
    if (!view.diskData) view.querySelector('.disk-history').replaceChildren();
    if (!automatic) failure.setAttribute('role', 'alert');
  } finally {
    if (request === view.diskRequest) {
      view.setAttribute('aria-busy', 'false');
      if (state.current?.id === p.id) $('service-reload').disabled = false;
      schedule(p, view);
    }
  }
}

export function createDiskHistory(p) {
  const view = el('div', 'frame disk-view');
  const failure = el('div', 'banner disk-load-error'); failure.hidden = true;
  const note = el('p', 'disk-note'); note.hidden = true;
  const content = el('div', 'disk-history'); content.append(el('p', 'empty', t('loading')));
  view.append(failure, note, content);
  view.diskObserver = new ResizeObserver(() => {
    const width = view.clientWidth;
    if (width > 0 && width !== view.diskWidth && view.diskData && !view.querySelector('.armed')) {
      view.diskWidth = width;
      renderDiskHistory(p, view);
    }
  });
  view.diskObserver.observe(view);
  return view;
}

export function renderDiskHistory(p, view) {
  const data = view.diskData;
  if (!data || !view.isConnected) return;
  // A response already in flight can arrive after the first confirmation click.
  // Keep that actual button until confirmation expires or its action finishes.
  clearTimeout(view.diskRenderTimer);
  if (view.diskBusy || view.querySelector('.armed')) {
    view.diskRenderTimer = setTimeout(() => renderDiskHistory(p, view), 250);
    return;
  }
  view.lang = state.lang;
  view.diskWidth = view.clientWidth;
  const choice = view.diskChoice;
  const active = view.contains(document.activeElement) ? document.activeElement.dataset.key : null;
  const content = view.querySelector('.disk-history');
  const parts = [];
  const meta = el('p', 'disk-meta', data.latest ? t('disk.meta', { time: date(data.latest), count: data.count, first: date(data.first) }) : t('disk.noScans'));
  parts.push(meta);

  const filters = el('div', 'disk-filters');
  const label = el('label', 'edit-field', t('disk.target'));
  const select = el('select'); select.dataset.key = 'target';
  for (const target of data.targets) {
    const option = el('option', null, target.name === '~' ? t('disk.home') : target.name);
    option.value = target.id; select.append(option);
  }
  select.value = data.target;
  select.addEventListener('change', () => { Object.assign(choice, { target: select.value, path: '.', from: '', to: '', all: false }); loadDiskHistory(p, view); });
  label.append(select); filters.append(label);
  const rangeGroup = el('div', 'disk-ranges');
  rangeGroup.setAttribute('role', 'group'); rangeGroup.setAttribute('aria-label', t('disk.range'));
  for (const range of ranges) {
    const b = button(t(`disk.range.${range}`), `range-${range}`, () => { Object.assign(choice, { range, from: '', to: '', all: false }); loadDiskHistory(p, view); });
    b.dataset.range = range; b.setAttribute('aria-pressed', String(range === choice.range)); rangeGroup.append(b);
  }
  filters.append(rangeGroup); parts.push(filters);

  const crumbs = el('div', 'disk-crumbs'); crumbs.setAttribute('role', 'group'); crumbs.setAttribute('aria-label', t('disk.location'));
  const target = data.targets.find(target => target.id === data.target);
  const segments = data.path === '.' ? [] : data.path.split('/');
  const names = [target?.name === '~' ? '~' : target?.name, ...segments];
  names.forEach((name, index) => {
    const path = index ? segments.slice(0, index).join('/') : '.';
    if (index) crumbs.append(el('span', 'disk-separator', '/'));
    if (index === names.length - 1) { const current = el('span', 'disk-current-name', name); current.tabIndex = -1; crumbs.append(current); }
    else crumbs.append(button(name, `crumb-${path}`, () => navigate(path), 'btn disk-crumb'));
  });
  parts.push(crumbs);
  const toolbar = el('div', 'disk-panel disk-scanbar');
  toolbar.append(el('p', 'disk-meta', data.running ? t('disk.scanning', { path: data.active }) : t(data.tracking ? 'disk.tracking' : 'disk.paused')));
  const actions = el('div', 'disk-actions');
  for (const action of data.actions) {
    const b = button(pick(action.label), `action-${action.id}`, () => {}, 'btn action');
    b.dataset.action = action.id;
    twoClicks(b, t('action.confirm', { label: pick(action.label) }), async () => {
      view.diskBusy = true; clearTimeout(view.diskTimer); b.disabled = true; b.textContent = t('action.running');
      const note = view.querySelector('.disk-note');
      try {
        const answer = await postJSON(`/api/action/${encodeURIComponent(p.id)}`, { item: action.item, action: action.id });
        feedback(note, action.id === 'scan' || action.id === 'track' ? t('disk.scanStarted') : pick(answer.message) || t('disk.actionDone'));
      } catch (error) { feedback(note, error instanceof TypeError ? t('action.interrupted') : explain(error), true); }
      finally {
        clearTimeout(view.diskNoteTimer);
        view.diskNoteTimer = setTimeout(() => { note.hidden = true; }, 8000);
        view.diskBusy = false;
        await loadDiskHistory(p, view);
      }
    });
    actions.append(b);
  }
  toolbar.append(actions); parts.push(toolbar);
  const warnings = [];
  if (data.unreadable) warnings.push(t('disk.unreadable', { count: data.unreadable }));
  if (data.truncated) warnings.push(t('disk.truncated'));
  if (data.old) warnings.push(t('disk.old'));
  if (data.failure) warnings.push(t('disk.failure', { detail: data.failure }));
  if (warnings.length) parts.push(el('p', 'disk-warning', warnings.join(' ')));
  const metrics = el('div', 'disk-metrics');
  for (const [key, value, className] of [['disk.current', size(data.bytes), 'disk-total'], ['disk.change', size(data.change, true), 'disk-change']]) {
    const box = el('div', 'disk-panel disk-metric');
    box.append(el('p', 'disk-meta', t(key)), el('p', className, value));
    if (className === 'disk-change' && data.from) box.append(el('p', 'disk-meta', `${date(data.from)} → ${date(data.to)}`));
    metrics.append(box);
  }
  parts.push(metrics);
  if (data.count) {
    const line = section(t('disk.curve'), 'disk-line');
    line.append(chart(data, false, view, p)); parts.push(line);
    const bars = section(t(`disk.buckets.${data.range}`), 'disk-bars');
    bars.append(el('p', 'disk-meta', t('disk.barHelp')));
    if (data.periods.length) bars.append(chart(data, true, view, p));
    else bars.append(el('p', 'empty', t('disk.single')));
    if (choice.from) bars.append(button(t('disk.clearPeriod'), 'clear-period', () => { choice.from = choice.to = ''; loadDiskHistory(p, view); }, 'btn disk-clear-period'));
    parts.push(bars);
    if (data.change == null) parts.push(el('p', 'disk-warning', t(data.points.length < 2 ? 'disk.single' : 'disk.unknown')));
    const dirs = section(t('disk.directories'), 'disk-directory');
    dirs.append(el('p', 'disk-meta', choice.from ? t('disk.selectedPeriod') : t('disk.directoryHelp')));
    if (data.children.length) {
      const table = el('table');
      const head = el('thead'), row = el('tr');
      for (const key of ['disk.directory', choice.from ? 'disk.periodSize' : 'disk.current', 'disk.change']) { const th = el('th', null, t(key)); th.scope = 'col'; row.append(th); }
      head.append(row); table.append(head);
      const body = el('tbody');
      for (const child of data.children.slice(0, choice.all ? undefined : 20)) {
        const tr = el('tr'), name = el('td');
        // Even a leaf can be opened to inspect its own curve and recorded boundary.
        const b = button(child.name, `path-${child.path}`, () => navigate(child.path), 'btn disk-directory-name');
        b.dataset.path = child.path; b.title = child.path;
        name.append(b);
        if (child.partial) name.append(el('span', 'disk-meta', t('disk.partial')));
        const value = el('td', null, size(child.bytes));
        const delta = el('td', child.change > 0 ? 'disk-grow' : child.change < 0 ? 'disk-shrink' : null, size(child.change, true));
        tr.append(name, value, delta); body.append(tr);
      }
      table.append(body); dirs.append(table);
      if (!choice.all && data.children.length > 20) dirs.append(button(t('disk.showAll', { count: data.children.length - 20 }), 'all-directories', () => { choice.all = true; renderDiskHistory(p, view); }));
    } else dirs.append(el('p', 'empty', t(data.depth >= 3 ? 'disk.depthLimit' : 'disk.noChildren')));
    parts.push(dirs);
    const details = el('details', 'disk-panel disk-records');
    details.open = view.diskRecordsOpen || false;
    details.addEventListener('toggle', () => { view.diskRecordsOpen = details.open; });
    details.append(el('summary', null, t('disk.records')), el('p', 'disk-meta', t('disk.measurement')));
    const table = el('table'), body = el('tbody');
    const head = el('thead'), header = el('tr');
    for (const label of ['disk.time', 'disk.current']) { const th = el('th', null, t(label)); th.scope = 'col'; header.append(th); }
    head.append(header); table.append(head);
    for (const point of [...data.points].reverse()) { const row = el('tr'); row.append(el('td', null, date(point.time)), el('td', null, size(point.bytes))); body.append(row); }
    table.append(body); const scroll = el('div', 'disk-record-scroll'); scroll.tabIndex = 0; scroll.setAttribute('aria-label', t('disk.records')); scroll.append(table); details.append(scroll); parts.push(details);
  } else parts.push(el('p', 'empty', t('disk.emptyHelp')));
  content.replaceChildren(...parts);
  if (active) content.querySelector('[data-key="' + CSS.escape(active) + '"]')?.focus({ preventScroll: true });
  schedule(p, view);

  function navigate(path) {
    Object.assign(choice, { path, from: '', to: '', all: false });
    loadDiskHistory(p, view).then(() => view.querySelector('.disk-current-name')?.focus({ preventScroll: true }));
  }
}

function svgElement(tag, attrs = {}, text) {
  const node = document.createElementNS(ns, tag);
  for (const [name, value] of Object.entries(attrs)) node.setAttribute(name, value);
  if (text !== undefined) node.textContent = text;
  return node;
}

function chart(data, bars, view, p) {
  const box = el('div', 'disk-chart');
  const width = Math.max(300, Math.min(928, view.clientWidth - 80));
  const svg = svgElement('svg', { viewBox: `0 0 ${width} 230`, tabindex: '0', role: 'img', 'data-key': bars ? 'bars' : 'line', 'aria-label': t(bars ? 'disk.barsKeyboard' : 'disk.lineKeyboard') });
  const reading = el('p', 'disk-chart-reading');
  const values = bars ? data.periods : data.points;
  let cursor = Math.min(view[bars ? 'diskBarIndex' : 'diskLineIndex'] || 0, values.length - 1);
  const times = values.map(v => Date.parse(bars ? v.to : v.time));
  const first = Date.parse(data.points[0].time), last = Date.parse(data.points.at(-1).time);
  const numbers = values.map(v => v.bytes).filter(v => v != null);
  let lo = Math.min(...numbers, bars ? 0 : Infinity), hi = Math.max(...numbers, 0);
  if (!numbers.length) { lo = 0; hi = 1; }
  if (lo === hi) { const padding = Math.max(1, Math.abs(lo) * .05); lo = Math.max(0, lo - padding); hi += padding; }
  const niceStep = value => {
    const power = 10 ** Math.floor(Math.log10(Math.max(1, value)));
    return [1, 2, 2.5, 5, 10].find(step => step * power >= value) * power;
  };
  let step = niceStep((hi - lo) / 4);
  if (bars) {
    const maximum = Math.max(Math.abs(lo), hi, 1);
    step = niceStep(maximum / 2);
    const limit = Math.ceil(maximum / step) * step; lo = -limit; hi = limit;
  } else { lo = Math.floor(lo / step) * step; hi = Math.ceil(hi / step) * step; }
  const x = time => 70 + (time - first) / Math.max(1, last - first) * (width - 86);
  const y = value => 190 - (value - lo) / (hi - lo) * 166;
  for (let i = 0; i <= Math.round((hi - lo) / step); i++) {
    const v = lo + step * i;
    svg.append(svgElement('line', { x1: 70, x2: width - 16, y1: y(v), y2: y(v), class: 'disk-grid' }), svgElement('text', { x: 62, y: y(v) + 4, 'text-anchor': 'end', class: 'disk-axis' }, size(v)));
  }
  const ticks = width < 500 ? 2 : 4;
  for (let i = 0; i <= (first === last ? 0 : ticks); i++) {
    const time = first + (last - first) * i / ticks;
    svg.append(svgElement('text', { x: x(time), y: 218, 'text-anchor': i === ticks ? 'end' : i === 0 ? 'start' : 'middle', class: 'disk-axis' }, date(time, last - first <= 86400000)));
  }
  const marks = [];
  let previous;
  values.forEach((value, index) => {
    if (value.bytes == null) { previous = null; return; }
    const cx = x(times[index]), cy = y(value.bytes);
    let mark;
    if (bars) {
      const left = x(Date.parse(value.from)), width = Math.max(3, cx - left - 4);
      mark = svgElement('rect', { x: left + 2, y: Math.min(y(0), cy), width, height: Math.max(2, Math.abs(y(0) - cy)), class: `disk-bar ${value.bytes >= 0 ? 'disk-grow' : 'disk-shrink'}`, 'data-index': index });
      mark.addEventListener('click', () => choose(index));
      if (view.diskChoice.from === value.from && view.diskChoice.to === value.to) mark.classList.add('disk-selected-bar');
    } else {
      if (previous && value.connected) svg.append(svgElement('line', { x1: previous[0], y1: previous[1], x2: cx, y2: cy, class: 'disk-series' }));
      mark = svgElement('circle', { cx, cy, r: 3, class: 'disk-point' });
      previous = [cx, cy];
    }
    mark.addEventListener('pointerenter', () => show(index));
    const title = svgElement('title', {}, bars ? `${date(value.from)} → ${date(value.to)} · ${size(value.bytes, true)}` : `${date(value.time)} · ${size(value.bytes)}`);
    mark.append(title); svg.append(mark); marks[index] = mark;
  });
  const guide = svgElement('line', { y1: 24, y2: 190, class: 'disk-guide', visibility: 'hidden' }); svg.append(guide);
  function show(index) {
    cursor = index; view[bars ? 'diskBarIndex' : 'diskLineIndex'] = index;
    const v = values[index]; if (!v) return;
    reading.textContent = bars ? `${date(v.from)} → ${date(v.to)} · ${size(v.bytes, true)}` : `${date(v.time)} · ${size(v.bytes)}`;
    svg.setAttribute('aria-label', `${t(bars ? 'disk.barsKeyboard' : 'disk.lineKeyboard')} ${reading.textContent}`);
    guide.setAttribute('x1', x(times[index])); guide.setAttribute('x2', x(times[index])); guide.setAttribute('visibility', 'visible');
  }
  function choose(index) {
    const v = values[index]; if (!bars || !v || v.bytes == null) return;
    const same = view.diskChoice.from === v.from && view.diskChoice.to === v.to;
    view.diskChoice.from = same ? '' : v.from; view.diskChoice.to = same ? '' : v.to;
    loadDiskHistory(p, view);
  }
  svg.addEventListener('focus', () => show(cursor));
  svg.addEventListener('pointermove', event => {
    const bounds = svg.getBoundingClientRect(), px = (event.clientX - bounds.left) / bounds.width * width;
    let nearest = 0;
    times.forEach((at, i) => { if (Math.abs(x(at) - px) < Math.abs(x(times[nearest]) - px)) nearest = i; });
    show(nearest);
  });
  svg.addEventListener('keydown', event => {
    if (['ArrowLeft', 'ArrowRight', 'Home', 'End', 'Enter', ' ', 'Escape'].includes(event.key)) event.preventDefault();
    if (event.key === 'ArrowLeft') show(Math.max(0, cursor - 1));
    if (event.key === 'ArrowRight') show(Math.min(values.length - 1, cursor + 1));
    if (event.key === 'Home') show(0);
    if (event.key === 'End') show(values.length - 1);
    if (event.key === 'Enter' || event.key === ' ') choose(cursor);
    if (event.key === 'Escape' && bars && view.diskChoice.from) { view.diskChoice.from = view.diskChoice.to = ''; loadDiskHistory(p, view); }
  });
  box.append(svg, reading); show(cursor);
  return box;
}
