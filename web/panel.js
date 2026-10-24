import { el } from './dom.js';

// Action panels stay next to their owner in the DOM, but the browser's top
// layer keeps them outside scrolling and clipped containers.
let openPanel = null;
let panelSequence = 0;

export function closePanel(restore = false) {
  if (!openPanel) return;
  const { panel, trigger } = openPanel;
  openPanel = null;
  if (panel.isConnected && panel.matches(':popover-open')) panel.hidePopover();
  panel.hidden = true;
  trigger.setAttribute('aria-expanded', 'false');
  if (restore && trigger.isConnected) trigger.focus();
}

export function positionPanel() {
  if (!openPanel) return;
  const { panel, trigger } = openPanel;
  if (!trigger.isConnected || trigger.closest('[inert]') || !trigger.checkVisibility()) return closePanel();
  const r = trigger.getBoundingClientRect();
  if (r.bottom < 0 || r.top > innerHeight) return closePanel();
  const size = panel.getBoundingClientRect();
  panel.style.left = `${Math.max(8, Math.min(r.right - size.width, innerWidth - size.width - 8))}px`;
  const top = r.bottom + 4 + size.height <= innerHeight - 8 ? r.bottom + 4 : r.top - size.height - 4;
  panel.style.top = `${Math.max(8, Math.min(top, innerHeight - size.height - 8))}px`;
}

// moreActions builds a "More actions" menu. The label arrives already worded,
// so this module needs no dictionary of its own.
export function moreActions(label, controls) {
  const wrap = el('div', 'more');
  const trigger = el('button', 'more-trigger', '⋯');
  trigger.type = 'button';
  trigger.setAttribute('aria-label', label);
  trigger.setAttribute('aria-expanded', 'false');
  const panel = el('div', 'action-panel');
  panel.id = `actions-${++panelSequence}`;
  panel.setAttribute('popover', 'manual');
  // A group, not a menu: the panel holds buttons the Tab key walks through,
  // and a menu would owe them arrow keys and menuitem roles as well.
  panel.setAttribute('role', 'group');
  panel.setAttribute('aria-label', label);
  panel.hidden = true;
  trigger.setAttribute('aria-controls', panel.id);
  trigger.setAttribute('aria-haspopup', 'true');
  panel.append(...controls);
  trigger.addEventListener('click', (e) => {
    e.stopPropagation();
    if (openPanel?.trigger === trigger) return closePanel(true);
    closePanel();
    panel.hidden = false;
    panel.showPopover();
    openPanel = { panel, trigger };
    trigger.setAttribute('aria-expanded', 'true');
    positionPanel();
  });
  panel.addEventListener('click', (e) => e.stopPropagation());
  wrap.append(trigger, panel);
  return wrap;
}

export function openPanelTrigger() {
  return openPanel ? openPanel.trigger : null;
}

export function openPanelPanel() {
  return openPanel ? openPanel.panel : null;
}

export function closePanelOnOutsideClick(e) {
  if (openPanel && !e.composedPath().includes(openPanel.panel) && !e.composedPath().includes(openPanel.trigger)) closePanel();
}

export function positionPanelOnScroll(e) {
  if (openPanel && !openPanel.panel.contains(e.target)) positionPanel();
}
