// The few calls that would otherwise make import cycles: a module that
// renders cannot import the composition root that renders it. The root fills
// these in during init(), before any of them can run.
let render = () => {};
let reloadShown = async () => {};
let loadCatalog = async () => {};

export function setHooks(hooks) {
  if (hooks.render) render = hooks.render;
  if (hooks.reloadShown) reloadShown = hooks.reloadShown;
  if (hooks.loadCatalog) loadCatalog = hooks.loadCatalog;
}

export const callRender = () => render();
export const callReloadShown = () => reloadShown();
export const callLoadCatalog = () => loadCatalog();
