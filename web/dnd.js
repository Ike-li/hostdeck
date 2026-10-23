import { addPage } from './catalog.js';
import { INTERNAL_DRAG, state } from './state.js';

// sortable lets the user drag the items of list that match selector into a
// new order. Once one is dropped, moved gets the IDs in the order they ended in.
export function sortable(list, selector, moved) {
  let dragged = null;
  let before = null;
  list.addEventListener('dragstart', (e) => {
    if (e.target.closest('button')) return;
    dragged = e.target.closest(selector);
    if (!dragged) return;
    before = [...list.querySelectorAll(selector)].map((item) => item.dataset.id);
    state.dragging = true;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', dragged.dataset.id);
    dragged.classList.add('dragging');
  });
  list.addEventListener('dragover', (e) => {
    const over = e.target.closest(selector);
    if (!dragged || !over) return;
    e.preventDefault();
    if (over === dragged) return;
    const items = [...list.querySelectorAll(selector)];
    if (items.indexOf(dragged) < items.indexOf(over)) over.after(dragged);
    else over.before(dragged);
  });
  list.addEventListener('drop', (e) => {
    if (dragged) e.preventDefault();
  });
  list.addEventListener('dragend', () => {
    if (!dragged) return;
    dragged.classList.remove('dragging');
    dragged = null;
    state.dragging = false;
    // The click that ends a drag would otherwise open the card.
    state.suppressClick = true;
    setTimeout(() => {
      state.suppressClick = false;
    }, 0);
    const order = [...list.querySelectorAll(selector)].map((item) => item.dataset.id);
    moved(order, before);
  });
}

// An external service address dropped onto the console is embedded. Internal
// drags and the console's own origin must not create another plugin.
function onDragOver(e) {
  if (!state.dragging && e.dataTransfer.types.includes('text/uri-list')) e.preventDefault();
}

function onDrop(e) {
  if (!e.dataTransfer.types.includes('text/uri-list')) return;
  e.preventDefault();
  if (state.dragging || e.dataTransfer.types.includes(INTERNAL_DRAG)) return;
  const address = e.dataTransfer
    .getData('text/uri-list')
    .split(/\r?\n/)
    .find((line) => line && !line.startsWith('#'));
  if (!address) return;
  try {
    if (new URL(address.trim()).origin === location.origin) return;
  } catch {
    // The API words invalid addresses, as it does for the add-page form.
  }
  addPage(address.trim());
}

export function wireDragAndDrop() {
  // A URI-list from one of our own links is not a request to add a page.
  document.addEventListener('dragstart', (e) => e.dataTransfer?.setData(INTERNAL_DRAG, '1'));
  document.addEventListener('dragover', onDragOver);
  document.addEventListener('drop', onDrop);
}
