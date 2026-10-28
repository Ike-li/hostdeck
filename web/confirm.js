import { ARMED_FOR } from './state.js';

// twoClicks makes btn run confirmed only on a second click: the first arms it
// for a few seconds and shows armedText. A double-click is one gesture, so its
// second click does not confirm.
export function twoClicks(btn, armedText, confirmed) {
  let label = btn.textContent;
  let armed = null;
  btn.addEventListener('click', (e) => {
    if (e.detail > 1 || btn.disabled) return;
    if (!armed) {
      label=btn.textContent;
      btn.textContent = typeof armedText==='function'?armedText():armedText;
      btn.classList.add('armed');
      armed = setTimeout(() => {
        armed = null;
        btn.textContent = label;
        btn.classList.remove('armed');
        btn.dispatchEvent(new Event('disarm', { bubbles: true }));
      }, ARMED_FOR);
      return;
    }
    clearTimeout(armed);
    armed = null;
    btn.classList.remove('armed');
    confirmed();
  });
}
