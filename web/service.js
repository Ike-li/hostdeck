import { $, el } from './dom.js';
import { t, pick, explain } from './i18n.js';
import { iconOf, paintDot } from './plugins.js';
import { cardElement } from './overview.js';
import { loadMonitor, loadDetail, renderDetail } from './monitor.js';
import { createDiskHistory, loadDiskHistory, renderDiskHistory, resumeDiskPolling } from './disk-history.js';
import { closePanel } from './panel.js';
import { callRender } from './hooks.js';
import { ARMED_FOR, IDLE_RELEASE, state } from './state.js';

let clearArmed = null; // the timer while the clear button waits for its second click

export function disarmClear() {
  clearTimeout(clearArmed);
  clearArmed = null;
}

export function renderService(p) {
  document.title = `${pick(p.name)} · hostdeck`;
  $('service-icon').textContent = iconOf(p);
  $('service').setAttribute('aria-busy', String(state.clearing));
  $('service-more').hidden = !!p.error || p.type !== 'proxy';
  $('service-more').querySelector('.more-trigger').setAttribute('aria-label', t('more.for', { name: pick(p.name) }));
  $('service-name').textContent = pick(p.name);
  paintDot($('service-dot'), p);
  $('service-dot').hidden = p.type !== 'proxy';
  $('service-error').hidden = !p.error;
  $('service-reload').hidden = !!p.error;
  $('service-reload').disabled = ['detail', 'card'].includes(p.type) && state.frames.get(p.id)?.getAttribute('aria-busy') === 'true';
  $('service-open').hidden = $('service-clear').hidden = $('service-notify').hidden = !!p.error || ['detail', 'card'].includes(p.type);
  const clear = $('service-clear');
  clear.textContent = t(state.clearing ? 'service.clearing' : clearArmed ? 'service.clear.confirm' : 'service.clear');
  clear.classList.toggle('armed', !!clearArmed);
  clear.disabled = state.clearing;
  const note = $('clear-note');
  note.hidden = !clearArmed && !state.clearFailure && !state.clearing;
  note.setAttribute('role', state.clearFailure ? 'alert' : 'status');
  const clearMessage = clearArmed ? t('service.clear.explain') : state.clearFailure ? t('error.clear_failed', { detail: state.clearFailure }) : state.clearing ? t('service.clearing') : '';
  if (note.textContent !== clearMessage) note.textContent = clearMessage;
  if (p.error) {
    const message = explain(p.error);
    if ($('service-error').textContent !== message) $('service-error').textContent = message;
  } else if (p.type === 'card') {
    let view = state.frames.get(p.id);
    if (!view) {
      view = el('div', 'frame monitor-view');
      view.append(el('p', 'empty', t('loading')));
      $('frames').append(view);
      state.frames.set(p.id, view);
      loadMonitor(p, view);
    } else if (view.data && view.lang !== state.lang) {
      view.lang = state.lang;
      view.replaceChildren(cardElement(view.data, true));
    }
  } else if (p.view === 'disk-history') {
    let view = state.frames.get(p.id);
    if (!view) {
      view = createDiskHistory(p);
      $('frames').append(view);
      state.frames.set(p.id, view);
      loadDiskHistory(p, view);
    } else if (view.diskData && view.lang !== state.lang) renderDiskHistory(p, view);
    else resumeDiskPolling(p, view);
  } else if (p.type === 'detail') {
    let view = state.frames.get(p.id);
    if (!view) {
      view = el('div', 'frame detail-view');
      view.append(el('p', 'detail-note'), el('div', 'detail-list'));
      view.firstChild.hidden = true;
      view.lastChild.append(el('p', 'empty', t('loading')));
      $('frames').append(view);
      state.frames.set(p.id, view);
      loadDetail(p, view);
    } else if (view.data && view.lang !== state.lang) {
      renderDetail(p, view);
    }
  } else {
    $('service-open').href = p.url;
    if (!state.frames.has(p.id)) {
      const frame = el('iframe', 'frame');
      frame.title = pick(p.name);
      frame.allow = 'clipboard-read; clipboard-write; fullscreen';
      frame.src = p.url;
      $('frames').append(frame);
      state.frames.set(p.id, frame);
    }
  }
  for (const [id, f] of state.frames) {
    f.hidden = id !== p.id || !!p.error;
    if (id === p.id && f.tagName === 'IFRAME') f.title = pick(p.name);
  }
}

// Clearing needs two clicks: the first arms the button and says what clearing
// does. A double-click is one gesture, so its second click does not confirm.
export function onClearClick(e) {
  if (e.detail > 1 || state.clearing || !state.current) return;
  if (!clearArmed) {
    state.clearFailure = null;
    clearArmed = setTimeout(() => {
      disarmClear();
      callRender();
    }, ARMED_FOR);
    callRender();
    return;
  }
  disarmClear();
  closePanel(true);
  clearService(state.current);
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
  state.clearing = true;
  callRender();
  const frame = state.frames.get(p.id);
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
    state.clearFailure = e.message;
  }
  state.clearing = false;
  callRender();
}

// Removing an iframe frees its memory and closes its connections; the next
// visit builds a fresh one.
export function releaseIdleFrames() {
  for (const [id, since] of state.hiddenSince) {
    if (Date.now() - since < IDLE_RELEASE) continue;
    state.frames.get(id).diskObserver?.disconnect();
    state.frames.get(id).remove();
    state.frames.delete(id);
    state.hiddenSince.delete(id);
  }
}

export function reloadService() {
  if (!state.current || !state.frames.has(state.current.id)) return;
  if (state.current.view === 'disk-history') loadDiskHistory(state.current, state.frames.get(state.current.id));
  else if (state.current.type === 'detail') loadDetail(state.current, state.frames.get(state.current.id));
  else if (state.current.type === 'card') loadMonitor(state.current, state.frames.get(state.current.id), true);
  else state.frames.get(state.current.id).src = state.current.url;
}
