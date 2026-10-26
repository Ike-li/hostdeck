// app.js is the composition root: it wires the page's own controls to the
// modules, fills the hooks that would otherwise make import cycles, and owns
// the two things that reach across every view: the hash route and render().
import { $ } from './dom.js';
import { applyI18n, initialLang, setLang, t } from './i18n.js';
import {
  closePanel, closePanelOnOutsideClick, moreActions, positionPanel, positionPanelOnScroll,
} from './panel.js';
import { narrow, onInterfaceKey, setNavigation } from './nav.js';
import {
  loadLayout, pauseLayoutNote, renderLayoutControls, resetLayout, scheduleLayoutNote, undoLayout,
} from './layout.js';
import { sortable, wireDragAndDrop } from './dnd.js';
import { loadPlugins, onSidebarMoved, refreshStatus, renderNav } from './plugins.js';
import {
  flushOverview, loadCards, onCardsMoved, refreshOverview, renderOverview, stopCardsRetry,
} from './overview.js';
import { loadMonitor, stopMonitorRetry } from './monitor.js';
import { disarmClear, onClearClick, releaseIdleFrames, reloadService, renderService } from './service.js';
import { openNotifications } from './notify.js';
import { loadCatalog, pageHint, renderCatalog, wireCatalog } from './catalog.js';
import { setHooks } from './hooks.js';
import { CATALOG, STATUS_EVERY, state } from './state.js';

// The address names the open entry: #/<id>, or #/.plugins for the plugins
// page. Anything else, a malformed address included, is the overview.
function route() {
  stopMonitorRetry();
  if ($('page-editor').open) $('page-editor').close();
  closePanel();
  state.pendingOverview = null;
  setNavigation(false);
  let id = '';
  try {
    id = decodeURIComponent(location.hash.replace(/^#\/?/, ''));
  } catch {
    // a malformed escape: show the overview
  }
  state.catalogOpen = id === CATALOG;
  if (!state.catalogOpen) state.catalogAddRequested = false;
  state.current = state.plugins.find((p) => p.id === id && p.type !== 'link') || null;
  disarmClear();
  state.clearFailure = null;
  for (const frameId of state.frames.keys()) {
    if (state.current && frameId === state.current.id) state.hiddenSince.delete(frameId);
    else if (!state.hiddenSince.has(frameId)) state.hiddenSince.set(frameId, Date.now());
  }
  if (state.catalogOpen) loadCatalog();
  if (!state.current && !state.catalogOpen) loadCards();
  else stopCardsRetry();
  render();
  if (state.current?.type === 'card' && !state.current.error) loadMonitor(state.current, state.frames.get(state.current.id));
}

function render() {
  applyI18n();
  renderLayoutControls();

  $('banner').hidden = !state.failure;
  if (state.failure) {
    const message = t('error.backend', { detail: state.failure });
    if ($('banner').textContent !== message) $('banner').textContent = message;
  }

  // Redrawing the sidebar or the cards would drop what is being dragged.
  if (state.dragging) return;
  pageHint();
  renderNav();
  $('overview').hidden = !!state.current || state.catalogOpen;
  $('catalog').hidden = !state.catalogOpen;
  $('service').hidden = !state.current;
  if (state.current) renderService(state.current);
  else if (state.catalogOpen) renderCatalog(false);
  else renderOverview();
}

// reloadShown reads the sidebar again, and the page that is open.
async function reloadShown() {
  await loadPlugins();
  $('nav').dataset.entries = '';
  if (state.catalogOpen) await loadCatalog();
  if (!state.current && !state.catalogOpen) await loadCards();
  else if (state.overview) state.overview.lang = null;
  render();
}

async function init() {
  setHooks({ render, reloadShown, loadCatalog });

  for (const b of document.querySelectorAll('[data-lang]')) {
    b.addEventListener('click', () => setLang(b.dataset.lang, true));
  }
  $('service-more').append(moreActions(t('more.for', { name: 'hostdeck' }), [$('service-notify'), $('service-clear')]));
  $('service-notify').addEventListener('click', () => state.current && openNotifications(state.current));
  $('service-clear').addEventListener('click', onClearClick);
  wireCatalog();
  wireDragAndDrop();
  $('empty-add').addEventListener('click', (e) => {
    if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) state.catalogAddRequested = true;
  });
  $('layout-undo').addEventListener('click', undoLayout);
  for (const event of ['pointerenter', 'focusin']) {
    $('layout-note').addEventListener(event, pauseLayoutNote);
  }
  for (const event of ['pointerleave', 'focusout']) {
    $('layout-note').addEventListener(event, scheduleLayoutNote);
  }
  $('layout-reset').addEventListener('click', resetLayout);
  $('refresh').addEventListener('click', refreshOverview);
  document.addEventListener('focusout', () => setTimeout(flushOverview, 0));
  $('menu').addEventListener('click', () => setNavigation(!document.querySelector('.app').classList.contains('nav-open')));
  $('nav-backdrop').addEventListener('click', () => setNavigation(false, true));
  document.querySelector('.side-menu').addEventListener('click', (e) => {
    if (e.target.closest('a')) setNavigation(false, true);
  });
  document.addEventListener('keydown', onInterfaceKey);
  document.addEventListener('click', closePanelOnOutsideClick);
  narrow.addEventListener('change', () => {
    setNavigation(document.querySelector('.app').classList.contains('nav-open'));
    positionPanel();
  });
  addEventListener('resize', positionPanel);
  document.addEventListener('scroll', positionPanelOnScroll, true);
  setNavigation(false);
  sortable($('nav'), '.nav-row', onSidebarMoved);
  sortable($('cards'), '.card', onCardsMoved);
  $('service-reload').addEventListener('click', reloadService);
  // #main is not an address. Read as one it would name a plugin that does not
  // exist, and the route would put the reader back on the overview.
  document.querySelector('.skip-link').addEventListener('click', (e) => {
    e.preventDefault();
    const main = document.querySelector('main');
    if (main.inert) setNavigation(false);
    main.focus();
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
