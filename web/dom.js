// Small DOM helpers shared by every module. Text is only ever written with
// textContent, never innerHTML, so no plugin-supplied string can run as HTML.
export const $ = (id) => document.getElementById(id);

export function el(tag, className, text) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined && text !== null) e.textContent = text;
  return e;
}

// feedback words a note as the result of something the user did: a status
// while it works, an alert when it failed.
export function feedback(note, text, failed = false) {
  note.setAttribute('role', failed ? 'alert' : 'status');
  note.dataset.kind = failed ? 'error' : 'success';
  if (note.textContent !== text) note.textContent = text;
  note.hidden = false;
}
