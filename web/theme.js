// A synchronous external script after the stylesheet applies server settings
// before the body is parsed. Later previews use the same engine via one event.
(() => {
  const root = document.documentElement;
  const system = matchMedia('(prefers-color-scheme: dark)');
  const custom = ['--accent', '--accent-strong', '--accent-pressed'];
  let choice;
  try { choice = JSON.parse(root.dataset.appearance).appearance; } catch { /* standalone fixture */ }
  choice ||= { version: 1, preset: 'default', mode: 'system' };

  const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
  const hex = (channels) => '#' + channels.map((c) => Math.round(c * 255).toString(16).padStart(2, '0')).join('');
  const mix = (a, b, amount) => a.map((c, i) => c * (1 - amount) + b[i] * amount);
  const luminance = (channels) => channels.map((c) => c <= .04045 ? c / 12.92 : ((c + .055) / 1.055) ** 2.4).reduce((v, c, i) => v + c * [.2126, .7152, .0722][i], 0);
  const ratio = (a, b) => { const x = luminance(a), y = luminance(b); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05); };
  function hsl(channels) {
    const max = Math.max(...channels), min = Math.min(...channels), delta = max - min, light = (max + min) / 2;
    if (!delta) return [0, 0, light];
    const [r, g, b] = channels;
    let hue = max === r ? (g - b) / delta + (g < b ? 6 : 0) : max === g ? (b - r) / delta + 2 : (r - g) / delta + 4;
    return [hue / 6, delta / (1 - Math.abs(2 * light - 1)), light];
  }
  function fromHSL(h, s, l) {
    const f = (n) => { const k = (n + h * 12) % 12, a = s * Math.min(l, 1 - l); return l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1)); };
    return [f(0), f(8), f(4)];
  }
  // Keep hue/saturation and move lightness toward the surface's opposite.
  // Leave room at either end for distinct hover and pressed colours. Measure
  // quantized sRGB values, including the actual 14% selected-control tint.
  function adapt(accent, dark, style) {
    const [h, s, light] = hsl(rgb(accent));
    const read = (token) => rgb(style.getPropertyValue(token).trim());
    const panel = read('--panel'), bg = read('--bg'), text = read('--text'), label = read('--on-accent');
    const end = dark ? [1, 1, 1] : [0, 0, 0];
    const start = Math.max(.12, Math.min(.88, light));
    for (let i = 0; i <= 1000; i++) {
      const l = dark ? Math.min(.88, start + i / 1000) : Math.max(.12, start - i / 1000);
      const base = rgb(hex(fromHSL(h, s, l)));
      const strong = rgb(hex(mix(base, end, .14))), pressed = rgb(hex(mix(base, end, .28)));
      if ([panel, bg, mix(bg, base, .14)].every((surface) => ratio(base, surface) >= 4.6) &&
          [base, strong, pressed].every((fill) => ratio(label, fill) >= 4.6) &&
          [panel, bg].every((surface) => ratio(text, mix(surface, base, .22)) >= 4.6)) {
        return [hex(base), hex(strong), hex(pressed)];
      }
      if (l === (dark ? .88 : .12)) break;
    }
    // A future palette that cannot fit the constraints keeps its tested accent.
    return null;
  }
  function apply(next) {
    choice = { ...next };
    const dark = choice.mode === 'dark' || (choice.mode === 'system' && system.matches);
    root.dataset.theme = choice.preset;
    root.dataset.mode = dark ? 'dark' : 'light';
    for (const token of custom) root.style.removeProperty(token);
    if (/^#[\da-f]{6}$/i.test(choice.accent || '')) {
      const colours = adapt(choice.accent, dark, getComputedStyle(root));
      if (colours) custom.forEach((token, i) => root.style.setProperty(token, colours[i]));
    }
  }
  document.addEventListener('hostdeck:theme', (event) => apply(event.detail));
  system.addEventListener('change', () => {
    if (choice.mode === 'system') {
      apply(choice);
      document.dispatchEvent(new Event('hostdeck:theme-applied'));
    }
  });
  apply(choice);
})();
