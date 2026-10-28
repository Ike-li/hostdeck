import { el } from './dom.js';
import { getJSON, postJSON } from './api.js';
import { t, pick } from './i18n.js';
import { state } from './state.js';

const panels = new Set();
const backoff = [2000, 5000, 10000];

// Only this region changes during a reading. The enclosing item, its actions,
// confirmation, filter controls and focused nodes remain the same objects.
export function observationPanel(p, item, view, saved = null) {
  if (!view.observationPanels) view.observationPanels = new Map();
  const key = saved?.id || item.id;
  if (view.observationPanels.has(key)) return view.observationPanels.get(key);
  const node = el('details', 'detail-log observation');
  const summary = el('summary', null, t('observation.title'));
  const progress = el('p', 'observation-progress');
  const status = el('p', 'observation-status');
  const sources = el('p', 'observation-sources');
  const at = el('p', 'observation-time');
  const warning = el('p', 'observation-warning');
  const scope = el('p', 'observation-scope');
  const existing = el('details', 'observation-existing');
  existing.append(el('summary', null, t('observation.existing')), el('pre'));
  existing.hidden = true;
  const pre = el('pre', 'observation-log');
  pre.tabIndex = 0;
  pre.setAttribute('aria-label', t('observation.title'));
  const latest = el('button', 'btn observation-latest', t('observation.latest'));
  latest.type = 'button'; latest.hidden = true;
  warning.setAttribute('role', 'status');
  node.append(summary, progress, status, sources, at, warning, scope, existing, pre, latest);
  const model = { p, item, view, node, id: saved?.id, cursor: '', failures: 0, busy: false, following: true, timer: null, active: false, saved };
  node.observation = model;
  view.observationPanels.set(key, node);
  panels.add(model);
  const visible = () => node.open && node.isConnected && !node.closest('[hidden]') && !document.hidden && state.current?.id === p.id;
  model.wake = () => {
    const active = visible();
    if (active === model.active) return;
    model.active = active;
    clearTimeout(model.timer);
    if (!active) model.abort?.abort();
    else read();
  };
  pre.addEventListener('scroll', () => {
    model.following = pre.scrollHeight - pre.clientHeight - pre.scrollTop < 24;
    latest.hidden = model.following;
  });
  latest.addEventListener('click', () => { model.following = true; latest.hidden = true; pre.scrollTop = pre.scrollHeight; });
  model.paint = (snap) => {
    model.saved = snap;
    existing.hidden = !snap.existingLog;
    existing.querySelector('pre').textContent = snap.existingLog || '';
    status.textContent = `${t('observation.state')}${t(`observation.state.${snap.state || 'unknown'}`)}${snap.identity?.pid ? ` · PID ${snap.identity.pid}` : ''}${snap.exitCode != null ? ` · ${t('observation.exit')} ${snap.exitCode}` : ''}${snap.signal ? ` · ${t('observation.signal')} ${snap.signal}` : ''}${snap.runs ? ` · ${t('observation.runs')} ${snap.runs}` : ''}`;
    sources.textContent = (snap.sources || []).map(s => `${s.name ? `${s.name} · ` : ''}${t(`observation.source.${s.state}`)}`).join('\n');
    at.textContent = snap.collectedAt ? t('observation.at', { time: new Date(snap.collectedAt).toLocaleString(state.lang) }) : '';
    scope.textContent = snap.partial ? t('observation.partial') : '';
    const top = pre.scrollTop;
    const content = snap.reset ? snap.log : pre.textContent + (snap.log || '');
    pre.textContent = content.slice(-262144);
    if (model.following) pre.scrollTop = pre.scrollHeight;
    else pre.scrollTop = top;
    if (snap.result) progress.textContent = `${t(`observation.result.${snap.result}`)} ${pick(snap.message)}`;
  };
  async function read() {
    if (model.busy || model.submitting || !visible()) return;
    model.busy = true;
    model.abort = new AbortController();
    try {
      if (!model.id) {
        const session = await postJSON(`/api/observations/${encodeURIComponent(p.id)}`, { item: item.id });
        model.id = session.id;
      }
      if (!visible()) return;
      const resp = await fetch(`/api/observations/session/${encodeURIComponent(model.id)}?cursor=${encodeURIComponent(model.cursor)}`, { cache: 'no-store', signal: model.abort.signal });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const data = await resp.json();
      model.cursor = data.cursor;
      model.paint(data);
      model.failures = 0; warning.textContent = '';
    } catch (err) {
      if (err.name !== 'AbortError') {
        model.failures++;
        warning.textContent = t('observation.failed');
      }
    } finally {
      model.busy = false;
      if (visible() && !model.submitting) model.timer = setTimeout(read, model.failures ? backoff[Math.min(model.failures - 1, 2)] : 1000);
    }
  }
  node.addEventListener('toggle', model.wake);
  if (saved) model.paint({ ...saved, reset: true });
  model.submit = async (action) => {
    model.submitting = true;
    node.open = true;
    clearTimeout(model.timer);
    model.abort?.abort();
    progress.textContent = t('observation.result.submitting');
    // A fresh session establishes file positions for each distinct operation.
    const session = await postJSON(`/api/observations/${encodeURIComponent(p.id)}`, { item: item.id, action: action.id });
    model.id = session.id; model.cursor = '';
    return session.id;
  };
  model.returned = (note, failed) => {
    progress.textContent = `${t(failed ? 'observation.result.failed' : 'observation.result.returned')} ${note}`;
    model.submitting = false;
    model.failures = 0;
    model.active = visible();
    clearTimeout(model.timer);
    if (!model.busy) read();
  };
  return node;
}

export function wakeObservations() {
  for (const model of panels) {
    if (!model.view.isConnected) { clearTimeout(model.timer); model.abort?.abort(); panels.delete(model); continue; }
    model.wake();
  }
}
document.addEventListener('visibilitychange', wakeObservations);
new MutationObserver(wakeObservations).observe(document.documentElement, { subtree: true, attributes: true, attributeFilter: ['hidden'], childList: true });

export async function recentOperations(p, view) {
  if (!view.data?.observation) return;
  let area = view.querySelector('.recent-operations');
  if (!area) { area = el('section', 'recent-operations'); view.append(area); }
  try {
    const recent = await getJSON(`/api/operations/${encodeURIComponent(p.id)}`);
    // Keep open regions mounted while updating the surrounding history.
    const title = area.querySelector('h2') || el('h2', null, t('observation.recent'));
    area.append(title);
    for (const record of recent) {
      if (area.querySelector(`[data-operation="${CSS.escape(record.id)}"]`)) continue;
      const row = el('section', 'detail-item'); row.dataset.operation = record.id;
      row.append(el('h3', 'detail-name', record.item), el('p', null, new Date(record.operationTime).toLocaleString(state.lang)));
      row.append(observationPanel(p, { id: record.item }, view, record));
      area.append(row);
    }
    area.hidden = !recent.length;
  } catch { area.hidden = false; area.append(el('p', null, t('observation.failed'))); }
}

// Reconcile current membership once after an operation. Surviving row and
// control objects are reused; retired observations move to history.
export async function refreshObservedItems(p,view) {
  try {
    const fresh=await getJSON(`/api/detail/${encodeURIComponent(p.id)}`);
    if(fresh.error) return;
    const ids=new Set((fresh.items||[]).map(it=>it.id));
    let area=view.querySelector('.recent-operations');
    if(!area){area=el('section','recent-operations');area.append(el('h2',null,t('observation.recent')));view.append(area);}
    for(const [id,row] of view.detailRows||[]) {
      if(ids.has(id)) continue;
      const model=row.querySelector('.observation')?.observation;
      if(!model?.id) continue;
      area.querySelector(`[data-operation="${CSS.escape(model.id)}"]`)?.remove();
      row.dataset.operation=model.id;
      for(const button of row.querySelectorAll('.action')) button.disabled=true;
      const focused=document.activeElement;
      area.hidden=false;area.append(row);
      if(focused?.isConnected) focused.focus({preventScroll:true});
    }
    view.data=fresh;
    if(view.updateObservedList) view.updateObservedList();
    else {
      for(const row of view.querySelectorAll('.detail-list > .detail-item')) if(!ids.has(row.dataset.item)) row.remove();
    }
  }catch { /* The observation independently reports a failed latest reading. */ }
}
