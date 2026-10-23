import { el } from './dom.js';
import { t } from './i18n.js';

// errorFile is the manifest path, folded away from the sentence that says why.
export function errorFile(path) {
  const d = el('details', 'error-file');
  d.append(el('summary', null, t('error.file')), el('p', null, path));
  return d;
}

// providesList names the parts a plugin's manifest declares.
export function providesList(p) {
  const keys = ['page', 'card', 'detail', 'actions'].filter((key) => p[key]);
  if (!keys.length) return null;
  const list = el('ul', 'catalog-provides');
  for (const key of keys) list.append(el('li', null, t(`catalog.provides.${key}`)));
  return list;
}
