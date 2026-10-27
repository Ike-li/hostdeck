import { $ } from './dom.js';
import { closePanel, openPanelPanel, openPanelTrigger } from './panel.js';

export const narrow = matchMedia('(max-width: 640px)');

export function setNavigation(open, restore = false) {
  open = narrow.matches && open;
  const wasOpen = document.querySelector('.app').classList.contains('nav-open');
  if (open !== wasOpen) closePanel();
  document.querySelector('.app').classList.toggle('nav-open', open);
  $('menu').setAttribute('aria-expanded', String(open));
  document.querySelector('.side-menu').inert = narrow.matches && !open;
  document.querySelector('main').inert = open;
  $('nav-backdrop').hidden = !open;
  if (open && !wasOpen) $('nav-overview').focus();
  else if (restore && narrow.matches) $('menu').focus();
}

// onInterfaceKey keeps the keyboard where the user left it: Tab circles inside
// the editor and the drawer, Escape closes the panel or the drawer, and Tab
// from a panel's trigger enters the panel.
export function onInterfaceKey(e) {
  const editor = $('page-editor');
  if (editor.open) {
    if (e.key === 'Tab') {
      const controls = [...editor.querySelectorAll('input, select, textarea, button')].filter((node) => !node.disabled && !node.closest('fieldset:disabled'));
      const first = controls[0], last = controls.at(-1);
      if (!first) e.preventDefault();
      else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
    return;
  }
  if (e.key === 'Escape') {
    if (openPanelTrigger()) { e.preventDefault(); closePanel(true); }
    else if (document.querySelector('.app').classList.contains('nav-open')) {
      e.preventDefault();
      setNavigation(false, true);
    }
    return;
  }
  if (e.key !== 'Tab') return;
  const panel = openPanelPanel();
  const trigger = openPanelTrigger();
  if (panel && trigger && document.activeElement === trigger && !e.shiftKey) {
    const first = panel.querySelector('button:not(:disabled), a[href]');
    if (first) { e.preventDefault(); first.focus(); }
    return;
  }
  if (!document.querySelector('.app').classList.contains('nav-open')) return;
  const targets = [...document.querySelector('.sidebar').querySelectorAll('a[href], button:not(:disabled)')]
    .filter((node) => node.getClientRects().length && !node.closest('[hidden]'));
  const first = targets[0], last = targets.at(-1);
  if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
  if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
}
