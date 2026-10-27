import { $, announce, el, feedback } from './dom.js';
import { getJSON, postJSON } from './api.js';
import { t, pick, explain } from './i18n.js';
import { closePanel, moreActions, openPanelTrigger } from './panel.js';
import { twoClicks } from './confirm.js';
import { hideButton, showId } from './layout.js';
import { errorFile, providesList } from './frag.js';
import { iconOf, loadPlugins, refreshStatus } from './plugins.js';
import { pageEdit } from './editor.js';
import { connectionPrompts, examplePlugin } from './connection-help.js';
import { callRender } from './hooks.js';
import { CATALOG, SEARCH_AFTER, state } from './state.js';

export function pageHint() {
  $('page-hint').textContent = t($('page-kind').value === 'link' ? 'catalog.page.linkHint' : 'catalog.page.embedHint');
}

export function focusCatalogAdd() {
  $('catalog-add').scrollIntoView({ block: 'start' });
  $('page-address').focus();
}

// The plugins page lists the plugins built into hostdeck. Like a detail page,
// it is rebuilt only when it is loaded or the language changes, so that a
// periodic render does not reset an add button waiting for its second click.
export async function loadCatalog() {
  try {
    state.catalog = await getJSON('/api/catalog');
    state.failure = null;
  } catch (e) {
    state.failure = e.message;
  }
  renderCatalog(true);
  if (state.catalogOpen && state.catalogAddRequested) {
    state.catalogAddRequested = false;
    focusCatalogAdd();
  }
}

export function renderCatalog(loaded) {
  if (!state.catalog || (!loaded && state.catalog.lang === state.lang)) return;
  const trigger = openPanelTrigger();
  if (trigger && $('catalog').contains(trigger)) closePanel();
  parkCatalogNote();
  state.catalog.lang = state.lang;
  document.title = `${t('catalog.title')} · hostdeck`;
  $('catalog-intro').textContent = t('catalog.intro');
  $('catalog-directory').textContent = state.catalog.pluginDir;
  $('help-plugin-dir').textContent = state.catalog.pluginDir;
  $('help-plugin-example').textContent = examplePlugin;
  const prompts = connectionPrompts(t, state.catalog.pluginDir, location.origin, state.catalog.executable);
  for (const kind of ['page', 'plugin']) {
    $(`${kind}-agent-prompt`).value = prompts[kind];
    $(`copy-${kind}-note`).hidden = true;
  }
  const byName = (a, b) => pick(a.name).localeCompare(pick(b.name), state.lang);
  const groups = [
    ['use', 'catalog.group.use', (p) => !p.disabled && !p.broken && !p.error],
    ['disabled', 'catalog.group.disabled', (p) => p.disabled && !p.broken && !p.error],
    ['error', 'catalog.group.error', (p) => !!(p.broken || p.error)],
  ].flatMap(([id, key, pred]) => {
    const items = state.catalog.installed.filter(pred).sort(byName).map(installedItem);
    if (!items.length && id !== 'use') return [];
    const section = el('section', 'catalog-group');
    section.dataset.group = id;
    const grid = el('div', 'catalog-list');
    grid.append(...items);
    section.append(el('h2', 'catalog-heading', t(key)));
    if (id === 'use') section.append(el('p', 'catalog-guidance', t('catalog.manageHint')));
    section.append(items.length ? grid : el('p', 'empty', t('catalog.noEnabled')));
    return [section];
  });
  $('catalog-installed').replaceChildren(...(groups.length ? groups : [el('p', 'empty', t('catalog.none'))]));
  const trashed = state.catalog.trash ?? [];
  $('catalog-trash').hidden = trashed.length === 0;
  $('catalog-trash-list').replaceChildren(...trashed.map(trashItem));
  $('catalog-available-heading').hidden = !state.catalog.available.length;
  $('catalog-available').replaceChildren(...[...state.catalog.available].sort(byName).map(availableItem));
  placeCatalogNote();
  updateCatalogSearch();
}

export async function copyAgentPrompt(kind) {
  const prompt = $(`${kind}-agent-prompt`);
  const button = $(`copy-${kind}-prompt`);
  const note = $(`copy-${kind}-note`);
  button.disabled = true;
  try {
    await navigator.clipboard.writeText(prompt.value);
    feedback(note, t('help.agent.copied'));
  } catch {
    $(`${kind}-prompt-preview`).open = true;
    prompt.focus();
    prompt.select();
    feedback(note, t('help.agent.copyFailed'), true);
  } finally {
    button.disabled = false;
  }
}

// The search box is for a long page. At most SEARCH_AFTER plugins stay a plain list.
export function updateCatalogSearch() {
  const search = $('catalog-search');
  const many = (state.catalog?.installed.length ?? 0) + (state.catalog?.available.length ?? 0) > SEARCH_AFTER;
  search.hidden = !many;
  $('catalog-search-wrap').hidden = !many;
  if (!many) search.value = '';
  filterCatalog();
}

export function filterCatalog() {
  const q = $('catalog-search').value.trim().toLowerCase();
  for (const card of document.querySelectorAll('#catalog .catalog-item')) {
    const name = card.querySelector('.card-name')?.textContent.toLowerCase() ?? '';
    card.hidden = q !== '' && !name.includes(q);
  }
  for (const group of document.querySelectorAll('.catalog-group')) {
    group.hidden = !(q === '' && group.dataset.group === 'use') && ![...group.querySelectorAll('.catalog-item')].some((card) => !card.hidden);
  }
  const available = [...document.querySelectorAll('#catalog-available .catalog-item')];
  if (available.length) $('catalog-available-heading').hidden = available.every((card) => card.hidden);
  const matches = [...document.querySelectorAll('#catalog .catalog-item')].filter((card) => !card.hidden);
  $('catalog-no-results').hidden = !q || matches.length > 0;
  if (q) announce(t('catalog.results', { n: matches.length }));
  const trash = $('catalog-trash');
  if (trash) {
    const items = [...trash.querySelectorAll('.catalog-item')];
    trash.hidden = items.length === 0 || items.every((card) => card.hidden);
  }
}

// parkCatalogNote lifts the result out of a card before that card is rebuilt.
function parkCatalogNote() {
  const note = $('catalog-note');
  const form = $('page-form');
  if (note && form && note.closest('.catalog-item')) form.before(note);
}

// placeCatalogNote puts the last result on the card it is about. A result
// that is not about one plugin, such as an address the form rejected, stays
// where the page first drew it.
function placeCatalogNote() {
  if (!state.catalogNoteFor) return;
  const card = document.querySelector(`#catalog .catalog-item[data-id="${CSS.escape(state.catalogNoteFor)}"]`);
  if (card) card.insertBefore($('catalog-note'), card.firstChild);
}

function catalogCard(p) {
  const item = el('article', 'card catalog-item');
  item.dataset.id = p.id;
  const head = el('header', 'card-head');
  head.append(el('span', 'icon', iconOf(p)), el('h3', 'card-name', pick(p.name)));
  item.append(head);
  if (p.description) item.append(el('p', 'catalog-description', pick(p.description)));
  return item;
}

// openControl opens a plugin that this run put in the sidebar.
function openControl(p) {
  const live = state.plugins.find((x) => x.id === p.id);
  if (!live) return null;
  const a = el('a', 'btn catalog-open', t('catalog.open'));
  if (live.type === 'link') {
    a.href = live.url;
    a.target = '_blank';
    a.rel = 'noopener';
  } else {
    a.href = `#/${encodeURIComponent(live.id)}`;
  }
  return a;
}

// postCatalog posts to url, says how it went with noteOf(the answer), and
// reads the page and the sidebar again.
export async function postCatalog(btn, url, noteOf, id) {
  btn.disabled = true;
  btn.textContent = t('catalog.working');
  let note;
  let failed = false;
  try {
    note = noteOf(await postJSON(url));
  } catch (err) {
    note = explain(err);
    failed = true;
  }
  state.catalogNoteFor = id;
  feedback($('catalog-note'), note, failed);
  await loadPlugins();
  callRender();
  refreshStatus();
  loadCatalog();
}

// catalogButton posts to url on a second click. A double-click does not.
export function catalogButton(label, armedText, url, noteOf, id) {
  const btn = el('button', 'btn', label);
  btn.type = 'button';
  twoClicks(btn, armedText, () => postCatalog(btn, url, noteOf, id));
  return btn;
}

// catalogOnce posts to url on one click. A double-click is one gesture.
export function catalogOnce(label, url, noteOf, id) {
  const btn = el('button', 'btn', label);
  btn.type = 'button';
  btn.addEventListener('click', (e) => {
    if (e.detail > 1 || btn.disabled) return;
    postCatalog(btn, url, noteOf, id);
  });
  return btn;
}

// addPage adds the web page at address and opens it. mode "link" stores a
// link and need not be local; anything else embeds a page on this machine.
// If it cannot, the plugins page says why.
export async function addPage(address, name = '', mode = '') {
  state.catalogNoteFor = null;
  const note = $('catalog-note');
  if (note) $('page-form').before(note);
  const payload = { address, name };
  if (mode === 'link') payload.mode = 'link';
  try {
    const body = await postJSON('/api/pages', payload);
    await loadPlugins();
    location.hash = `#/${encodeURIComponent(body.id)}`;
    refreshStatus();
    return true;
  } catch (err) {
    feedback($('catalog-note'), explain(err), true);
    location.hash = `#/${CATALOG}`;
    return false;
  }
}

export async function onPageSubmit(e) {
  e.preventDefault();
  const form = e.target;
  const btn = form.querySelector('button');
  btn.disabled = true;
  if (await addPage($('page-address').value.trim(), $('page-name').value, $('page-kind').value)) form.reset();
  btn.disabled = false;
  pageHint();
}

// Management rows share the same identity and capability presentation.
// level is the heading the row sits under: h3 below a group's h2, h4 below
// the built-in plugins' own h3.
function catalogIdentity(p, level = 'h3') {
  const name = pick(p.name);
  const identity = el('div', 'catalog-identity');
  const head = el('header', 'card-head');
  const title = el(level, 'card-name', name);
  title.title = name;
  const kind = p.page && !p.card && !p.detail && !p.actions ? (p.link ? 'link' : 'page') : 'plugin';
  head.append(el('span', 'icon', iconOf(p)), title, el('span', 'catalog-kind', t(`catalog.kind.${kind}`)));
  const description = el('p', 'catalog-description', pick(p.description) || p.proxy || p.link || t('catalog.custom'));
  description.title = pick(p.description) || description.textContent;
  identity.append(head, description);
  return identity;
}

function catalogFeatures(p) {
  const features = el('div', 'catalog-summary');
  features.append(el('span', 'catalog-field-label', t('catalog.features')), providesList(p) || el('p', 'empty', t('catalog.features.none')));
  return features;
}

function installedItem(p) {
  const item = el('article', 'card catalog-item catalog-entry');
  item.dataset.id = p.id;
  const name = pick(p.name);
  const identity = catalogIdentity(p);
  const features = catalogFeatures(p);
  const status = el('div', 'catalog-status');
  const broken = p.error || p.broken;
  const state_ = broken ? 'broken' : p.disabled ? (p.loaded ? 'disabling' : 'disabled') : p.loaded ? 'loaded' : 'added';
  const stateLine = el('p', 'catalog-state', t(`catalog.state.${state_}`));
  stateLine.dataset.state = broken ? 'broken' : p.disabled ? 'disabled' : p.loaded ? 'enabled' : 'pending';
  const hidden = state.stored.hidden.includes(p.id);
  const visibility = p.disabled ? (hidden ? 'offHidden' : 'offVisible') : hidden ? 'hidden' : p.loaded || broken ? 'visible' : 'pending';
  status.append(el('span', 'catalog-field-label', t('catalog.connectionState')), stateLine, el('p', 'catalog-visibility', t(`catalog.visibility.${visibility}`)));

  const actions = el('div', 'catalog-actions');
  const open = openControl(p);
  if (open) actions.append(open);
  if (hidden && !p.disabled) {
    const show = el('button', 'btn show', t('layout.show'));
    show.type = 'button';
    show.addEventListener('click', () => showId(p.id));
    actions.append(show);
  }
  const verb = p.disabled ? 'enable' : 'disable';
  const toggle = catalogButton(t(`catalog.${verb}`), t(`catalog.${verb}.confirm`),
    `/api/plugins/${encodeURIComponent(p.id)}/${verb}`, () => t(`catalog.${verb}d.note`, { name }), p.id);
  toggle.classList.add('catalog-toggle');
  const trash = catalogOnce(t('catalog.trash'), `/api/plugins/${encodeURIComponent(p.id)}/trash`, () => t('catalog.trashed.note', { name }), p.id);
  trash.classList.add('trash');
  const options = [];
  if (!hidden && !p.disabled) options.push(hideButton());
  if (p.disabled) actions.append(toggle);
  else options.push(toggle);
  options.push(trash);
  actions.append(pageEdit(p), moreActions(name, options));
  item.append(identity, features, status, actions);
  const detail = p.error ? explain(p.error) : p.broken ? t('catalog.state.broken') : p.disabled && p.loaded ? t('catalog.disablingHint') : !p.disabled && !p.loaded ? t('catalog.pendingHint') : '';
  if (detail) item.append(el('p', 'catalog-state-detail', detail));
  if (p.error?.file) item.append(errorFile(p.error.file));
  return item;
}

// trashItem is one folder in the trash: restore puts it back enabled, and
// purge deletes it on a second click.
function trashItem(p) {
  const item = catalogCard(p);
  const name = pick(p.name);
  const actions = el('div', 'catalog-actions');
  const restore = catalogOnce(
    t('catalog.restore'),
    `/api/trash/${encodeURIComponent(p.folder)}/restore`,
    () => t('catalog.restored.note', { name }),
    p.id,
  );
  restore.classList.add('restore');
  const purge = catalogButton(
    t('catalog.purge'),
    t('catalog.purge.confirm'),
    `/api/trash/${encodeURIComponent(p.folder)}/purge`,
    () => t('catalog.purged.note', { name }),
    p.id,
  );
  purge.classList.add('purge');
  item.querySelector('.card-head').append(moreActions(name, [purge]));
  actions.append(restore);
  item.append(actions);
  return item;
}

function availableItem(p) {
  const item = el('article', 'card catalog-item catalog-entry catalog-offer');
  item.dataset.id = p.id;
  const actions = el('div', 'catalog-actions');
  const btn = catalogButton(
    t('catalog.add'),
    t('catalog.add.confirm'),
    `/api/catalog/${encodeURIComponent(p.id)}`,
    (body) => t('catalog.added', { path: body.path }),
    p.id,
  );
  btn.classList.add('add');
  actions.append(btn);
  item.append(catalogIdentity(p, 'h4'), catalogFeatures(p), actions);
  return item;
}

// wireCatalog binds the plugins page's own controls; the rest of the page is
// built from the API's answers.
export function wireCatalog() {
  $('page-form').addEventListener('submit', onPageSubmit);
  for (const kind of ['page', 'plugin']) {
    $(`copy-${kind}-prompt`).addEventListener('click', () => copyAgentPrompt(kind));
  }
  $('page-kind').addEventListener('change', pageHint);
  $('catalog-add-open').addEventListener('click', focusCatalogAdd);
  $('catalog-search').addEventListener('input', filterCatalog);
}
