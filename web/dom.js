// Small DOM helpers shared by every module. Text is only ever written with
// textContent, never innerHTML, so no plugin-supplied string can run as HTML.
export const $ = (id) => document.getElementById(id);

export function el(tag, className, text) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined && text !== null) e.textContent = text;
  return e;
}

// announce says something the page cannot show: how many cards there are, how
// many entries a search matched, that a reading is done. It writes to the
// page's one live region, which no view rebuilds, and it never writes the same
// words twice: the grid is rebuilt every two seconds while a card is being
// read, and saying the same thing again each time would talk over the reader.
export function announce(text) {
  const region = $('live-region');
  if (!region || !text || region.textContent === text) return;
  region.textContent = text;
}

// feedback words a note as the result of something the user did: a status
// while it works, an alert when it failed. The role is written while the note
// is in the accessibility tree and before the words, which is the order a
// screen reader acts on; a note that kept its words but changed its urgency is
// written again, so the new urgency is spoken.
export function feedback(note, text, failed = false) {
  note.hidden = false;
  const role = failed ? 'alert' : 'status';
  const changedRole = note.getAttribute('role') !== role;
  if (changedRole) note.setAttribute('role', role);
  note.dataset.kind = failed ? 'error' : 'success';
  if (note.textContent === text && !changedRole) return;
  note.textContent = '';
  note.textContent = text;
}
