# Design system

[简体中文](DESIGN.zh-CN.md)

This is what the hostdeck console's interface is held to: designing a view, changing a component and changing the theme all start here. It does not bring a style of its own. It writes down the tokens and components `web/style.css` already uses as rules: which selector each rule lives in, which test keeps it, and why each choice that matters was made.

The tokens have one source, the `:root` blocks at the top of `web/style.css`. Where this document and the code disagree, the code and the tests that keep it win, and this document is corrected.

Every section carries a mark:

| Mark | Meaning |
| --- | --- |
| [Shipped] | The code is like this. The selector is named; changing it means changing the code and its test. |
| [Gap] | The code does not meet this yet. Appendix A says what is left and why. |
| [Reserved] | The product has no such component yet. The rules are set now, to be followed the first time it is built. |

## 0 Assumptions and principles

### 0.1 Assumptions

| Subject | Assumption | Basis |
| --- | --- | --- |
| Brand colour | No brand guide exists; the shipped accent `#2b64c6` (dark `#6c9cf0`) is the brand colour | The repository holds no brand assets; the accent is shipped and passes the contrast tests |
| Users | One person managing the services on their own Mac or Windows computer, technical, often with a terminal open | README, "What it will be" |
| Devices | Desktop browsers: Chrome and Edge are tested, Firefox and Safari best effort. Windows can be narrow, but phones are not a target | README, "Not planned", rules out other devices; the M0 decisions in AGENTS.md |
| Stack | One Go program embedding the page; `web/` is plain ES modules with no build step and no npm; one `style.css`; Go tests and real-browser tests are the gate | AGENTS.md, "How work is done" |
| Languages | English and 简体中文, switchable, with the same keys in both dictionaries | `web/i18n/*.json`, `web/i18n_test.go` |
| Themes | Light and dark, following the system, with no switch of their own | There is no settings page yet |
| Typefaces | No brand typeface; the system's | One binary that works offline downloads no fonts |

### 0.2 Principles

1. **The code is the token source.** The browser reads the CSS custom properties directly, and the Go tests read them with one parser. Why: without a build step there should be no second source to keep in step.
2. **One scale.** Spacing, type sizes, leading, weights, radii, durations and easing take values from their scales only, and the tests refuse anything else. Why: a stray 10px or 17px drifts on its own.
3. **A state is never told by colour alone.** Every state a colour shows also has words or a shape. Why: someone who cannot tell red from green still has to see which service is down.
4. **No behaviour the product does not have.** This document sets how existing interactions look; a new interaction is proposed as a product slice first. Why: a design system is not a requirements document.
5. **Every rule that can be tested is.** Section 6.4 names the test that keeps each rule; a rule without one is a gap.

## 1 Foundations

### 1.1 Colour

#### 1.1.1 Theme colours [Shipped]

A theme is these 14 colours and nothing else. Changing the theme changes these 14 values; every other colour is derived from them.

| Token | Role | Light | Dark | Contrast on `--panel` (light / dark) |
| --- | --- | --- | --- | --- |
| `--bg` | The page, and secondary surfaces (logs, the segmented control) | `#f6f7f9` | `#15171b` | — |
| `--panel` | Cards, the sidebar, panels and the dialog | `#ffffff` | `#1e2126` | — |
| `--text` | Body text and headings | `#1c1f24` | `#e7e9ec` | 16.52 / 13.27 |
| `--muted` | Secondary text, field names, hints, unselected icons | `#606975` | `#9aa1ab` | 5.56 / 6.20 |
| `--line` | Dividers, the default edge of cards and buttons | `#e3e6ea` | `#2d3138` | Not text; see 5.1 |
| `--line-strong` | The edge of a card or row under the pointer | `#c9ced6` | `#3d434d` | Not text; see 5.1 |
| `--line-control` | The edge of a field, a status dot nothing has been heard from | `#868d98` | `#686e78` | Not text: 3.35 / 3.14 (on `--bg` 3.12 / 3.50) |
| `--accent` | The accent: primary buttons, selection, the focus ring, a button's hover edge | `#2b64c6` | `#6c9cf0` | 5.61 / 5.88 |
| `--accent-strong` | A primary button under the pointer | `#2556a9` | `#8ab0f5` | — |
| `--accent-pressed` | A pressed primary button | `#1f4988` | `#a3c3f8` | — |
| `--on-accent` | Words on the accent | `#ffffff` | `#15171b` | On `--accent` 5.61 / 6.54 |
| `--ok` | Healthy, running | `#247a46` | `#4cc27f` | 5.32 / 7.17 |
| `--warn` | Needs attention | `#946313` | `#e0a83a` | 5.18 / 7.56 |
| `--error` | Failed, destructive | `#bf3636` | `#ef6b6b` | 5.53 / 5.37 |

How the four kinds of colour a design system usually names map here:

- **Primary**: the `--accent` family (`-strong`, `-pressed`, `--on-accent`).
- **Secondary**: none. Why: green, yellow and red already mean states, and another hue would be read as one more; grouping is done by headings, spacing and neutral surfaces.
- **Semantic**: `--ok`, `--warn` and `--error`. There is no separate information colour; information sits on the accent's tint (`--tint-accent`). Why: blue means the console speaking and selection only, and does not compete with the states.
- **Neutral**: `--bg`, `--panel`, `--text`, `--muted`, `--line`, `--line-strong` and `--line-control`; the ramp is in 1.1.2.

Two rules for the dark theme:

- The accent gets lighter and `--on-accent` turns dark. Why: blue on a dark surface has to be light to reach 4.5:1, so a primary button becomes dark words on a light fill.
- Hover and pressed get darker step by step in the light theme and lighter in the dark one. Why: the direction is always away from the background, and pressed goes further than hover.

#### 1.1.2 Neutral ramp (raw values) [Shipped]

The two themes share 14 neutrals, from lightest to darkest:

| Value | Relative luminance | Role in the light theme | Role in the dark theme |
| --- | --- | --- | --- |
| `#ffffff` | 1.000 | `--panel`, `--on-accent` | — |
| `#f6f7f9` | 0.930 | `--bg` | — |
| `#e7e9ec` | 0.813 | — | `--text` |
| `#e3e6ea` | 0.789 | `--line` | — |
| `#c9ced6` | 0.614 | `--line-strong` | — |
| `#9aa1ab` | 0.353 | — | `--muted` |
| `#868d98` | 0.264 | `--line-control` | — |
| `#686e78` | 0.155 | — | `--line-control` |
| `#606975` | 0.139 | `--muted` | — |
| `#3d434d` | 0.055 | — | `--line-strong` |
| `#2d3138` | 0.030 | — | `--line` |
| `#1e2126` | 0.015 | — | `--panel` |
| `#1c1f24` | 0.014 | `--text` | — |
| `#15171b` | 0.009 | — | `--bg`, `--on-accent` |

This layer lives in this document only, not as `--gray-*` variables in the CSS. Why: the contrast tests measure hex literals, and a `var()` in between would need another resolver; with two themes and 14 colours the indirection buys nothing.

#### 1.1.3 Derived colours [Shipped]

Derived colours are mixed from the theme colours with `color-mix(in srgb, …)`, so they follow a theme change by themselves.

| Token | Recipe | Used for |
| --- | --- | --- |
| `--tint-accent` | `--accent` 10% + `--panel` | Result notes: `.note`, `.detail-note`, `.catalog-note` |
| `--tint-active` | `--accent` 14% + transparent | The open sidebar entry |
| `--tint-pressed` | `--accent` 22% + transparent | Everything pressable, while pressed |
| `--tint-error-soft` | `--error` 14% + transparent | The error banner |
| `--tint-error-hover` | `--error` 12% + transparent | An armed button under the pointer; 14% takes the red word under 4.5:1 |
| `--tint-error-panel` | `--error` 10% + `--panel` | `[data-kind="error"]` notes, an armed action in a panel |
| `--tint-card-hover` | `--text` 3% + `--panel` | A card that opens something, under the pointer |
| `--tint-row-hover` | `--text` 4% + `--panel` | A management row under the pointer |
| `--tint-status` (local to `.pill`) | `--status` 10% + transparent | A status pill's fill; a stronger tint takes the 12px status word under 4.5:1 |
| `--opacity-disabled` | `.5` | Disabled controls |

Hover tints are mixed from a neutral, not from the accent. Why: a hover must not look like a selection (the comment on `.card:hover` says so).

#### 1.1.4 Scrims [Shipped]

| Token | Value | Used for |
| --- | --- | --- |
| `--scrim` | `#0005` | Behind the narrow window's drawer |
| `--scrim-dialog` | `#11182770` | The dialog's `::backdrop` |

Both themes use the same values. Why: a scrim darkens what is under it, and translucent black does that on either surface.

### 1.2 Type [Shipped]

**Family**: `-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif`. The first three cover Latin text, the last two are the Chinese fallbacks on macOS and Windows. Why: each system's own typeface, nothing downloaded, working offline.

**Monospace**: `ui-monospace, monospace` for the prompt text areas; `code` and `pre` use the browser's monospace.

**Sizes**: eight steps, each with fixed uses.

| Token | Size | Leading | Weight | Used for |
| --- | --- | --- | --- | --- |
| `--text-xs` | 12px | 1.5 | 400 | Read times, status pills, plugin kinds, the field labels of management rows |
| `--text-sm` | 13px | 1.5 / 1.65 | 400 | Button labels, form labels, hints, card rows, detail fields, explanations |
| `--text-base` | 14px | 1.5 | 400 | Fields, action panel items, card addresses, the plugins page's introduction, the connection help |
| `--text-md` | 15px | 1.5 | 400 / 600 | Body text (the `body` default); card names, `h3` subheadings and the plus card's title at 600 |
| `--text-lg` | 16px | 1.5 | 600 | The brand, section headings (`h2.catalog-heading`), the service name, detail names |
| `--text-xl` | 20px | 1.5 | 600 | The dialog title |
| `--text-2xl` | 24px | 1.5 | 600 | Page titles (`h1`), a detail's main value |
| `--text-3xl` | 32px | 1.25 | 600 | A card's main value; the plus card's "+" at 400 with leading 1 |

**Leading**: `--leading-tight` 1.25 for large numbers only, `--leading-normal` 1.5 as the default, `--leading-relaxed` 1.65 for explanations over several lines and long monospace text. Only an icon glyph may set `line-height: 1`. Why: explanations often run to two or three lines and read better with room; a number is one line, and tight leading keeps it from making the card taller.

**Weights**: three. 400 is the default; 600 (`--weight-semibold`) is for headings, names, numbers and the state words of management rows; 700 (`--weight-bold`) is for the brand mark alone. Why: with one weight for emphasis, the levels read at a glance. A base rule, `:is(h2, h3, h4, strong, b)`, sets 600, and `h1` sets it in its own rule, so a new heading does not fall back to the browser's 700.

**Numbers**: a card's main value uses `font-variant-numeric: tabular-nums` and `letter-spacing: -.025em`. Why: a refreshed number keeps its width, and 32px digits sit tighter.

Body text is 15px and control labels 13–14px. Why: what is read is one step larger than what is clicked, so content and controls part at a glance.

### 1.3 Spacing [Shipped]

One four-point scale.

| Token | Value | Typical uses |
| --- | --- | --- |
| — | 0 | Removing a default margin |
| `--space-1` | 4px | A button's vertical padding, a pill's padding, card rows, the segmented control's gaps, between sidebar rows |
| `--space-2` | 8px | Between an icon and its words, between buttons, between a field's label and the field, a panel's padding |
| `--space-3` | 12px | A button's horizontal padding, between the blocks of a card, the sidebar's horizontal padding, a note's padding |
| `--space-4` | 16px | A card's padding, between cards, the page edge in a narrow window, between detail items |
| `--space-5` | 24px | Between sections, a detail item's and the dialog's padding, the page's top and bottom, the plus card's padding |
| `--space-6` | 32px | The page's sides in a wide window, between the plugins page's large sections |
| `--space-7` | 48px | No use yet; it is a step of the scale, so it stays (see 2.5) |
| `--space-8` | 64px | The room above a scrolled-to anchor (`#catalog-add`'s `scroll-margin-top`) |

Rules:

- A px literal in `margin`, `padding` or `gap` is 0 or a step of the scale; everything else is `var(--space-*)`.
- A negative space is `calc(var(--space-2) * -1)`, not `-8px`.
- Related things sit close (4–8px), unrelated things apart (16–24px). Why: spacing is the grouping, and no line has to draw it.

### 1.4 Radii [Shipped]

| Token | Value | Used for |
| --- | --- | --- |
| `--radius-sm` | 6px | Buttons, segmented buttons, small sidebar icons, panel items, code blocks, the skip link |
| `--radius-md` | 8px | Fields, sidebar rows, notes, the More actions button, the segmented control |
| `--radius-lg` | 12px | Cards, action panels, the add form, detail items, management rows |
| `--radius-xl` | 16px | The dialog |
| `--radius-full` | 999px | Status pills, capability tags |
| 50% | — | Status dots, the plus card's circle |

An inner corner is smaller than the outer one: a 6px item in a 12px panel, a 6px button in the 8px segmented control. Why: equal radii make a nested corner look swollen. The larger the container, the larger the radius; the dialog has the largest, so the levels show.

### 1.5 Elevation and stacking [Shipped]

| Level | Surface | Shadow | Edge | Examples |
| --- | --- | --- | --- | --- |
| 0 page | `--bg` | None | — | The overview, the detail view |
| 1 card | `--panel` | None; `--shadow-card-lift` `0 4px 16px #0002` on a card that opens something, under the pointer | 1px `--line` | Cards, management rows, detail items, the sidebar |
| 2 floating | `--panel` | `--shadow-popover` `0 8px 32px #0002` | 1px `--line` | More actions panels |
| 3 dialog | `--panel` | `--shadow-dialog` `0 24px 80px #0003` | 1px `--line` | The editor, over `--scrim-dialog` |

Cards lie flat; only a card that opens something rises under the pointer. Why: a screen of cards stays quiet, and the one that rises is the one about to open.

`z-index` has four values: `--z-backdrop` 10 (the drawer's scrim), `--z-drawer` 20 (the drawer), `--z-header` 30 (the narrow top bar), `--z-skip` 40 (the skip link). Action panels and the dialog use the browser's top layer (`popover`, `<dialog>`) and no `z-index`. Why: the top layer is above everything and is not clipped by a scrolling container.

### 1.6 Edges [Shipped]

| Use | Value |
| --- | --- |
| Default edges and dividers | 1px solid `--line` |
| A field's edge | 1px solid `--line-control`; `--muted` under the pointer |
| Under the pointer | `--line-strong` for cards and rows; `--accent` for buttons |
| The plus card | 1px dashed `--line`; `--accent` under the pointer |
| States | The card's edge in `--warn` or `--error`, plus a 3px (`--rail-w`) rail on its left |
| Focus | A 2px solid `--accent` outline, offset 3px |
| A dot whose address did not answer | A 2px solid `--error` ring with a see-through middle |

The rail is drawn (`::before`, positioned absolutely) and takes no layout. Why: with or without a state, the number in the card stays where it was.

A field's edge is `--line-control`, darker than `--line`. Why: WCAG 1.4.11 asks 3:1 of the edge that says "type here", and `--line` is 1.25:1. A button has its label and a card its content, so neither needs a 3:1 edge, and both keep the quiet `--line`.

### 1.7 Motion [Shipped]

| Token | Value | Used for |
| --- | --- | --- |
| `--dur-fast` | 120ms | Hover and pressed colour changes, action panels fading in, the skip link sliding in |
| `--dur-base` | 200ms | The drawer sliding in, the dialog and notes fading in |
| `--dur-slow` | 320ms | No use yet; a step of the scale, kept |
| `--ease-standard` | `cubic-bezier(.2, 0, 0, 1)` | Every transition; the only curve |

Rules:

- A transition names its properties, never `all`; its duration is a `--dur-*` token and its easing `--ease-standard`. Why: with `all`, a property a test has just read can still be mid-animation.
- What appears fades in through `@starting-style`, and only its `opacity` moves. Why: panels are placed by measured pixels and the dialog is measured as it opens; moving `transform` or size would measure wrong.
- What leaves goes at once, with no exit animation (the drawer too), so there is no exit curve. Why: a drawer on its way out still covers the page and swallows the click meant for what is under it.
- `prefers-reduced-motion: reduce` takes the three durations to zero, and a `*` rule takes every transition's and animation's duration to zero as well.

Interaction timings live in `web/state.js`, and count as tokens too:

| Constant | Value | Meaning |
| --- | --- | --- |
| `ARMED_FOR` | 4000ms | How long a destructive button waits for its second click |
| `LAYOUT_NOTE_FOR` | 8000ms | "Layout saved." and its undo close after eight idle seconds, pausing while the pointer or focus is on them |
| `PENDING_AGAIN` | 2000ms | How soon a card still being read is asked for again |
| `STATUS_EVERY` | 15000ms | How often the sidebar's status dots are read |
| `SEARCH_AFTER` | 10 | The plugins page shows its search from eleven plugins |

A detail view's action result closes after eight seconds (`web/monitor.js`).

### 1.8 Grid and breakpoints [Shipped]

No 12-column grid. Why: the console lays out only a card grid and lists, and columns set by the content's minimum width hold up better than a fixed count.

**Shell**: `grid-template-columns: 240px minmax(0, 1fr)`, `100vh` tall. The sidebar is a fixed 240px (`--sidebar-w`), and its plugin list scrolls on its own.

**Content**: 24px top and bottom, 32px on the sides; 16px all round in a narrow window. **No maximum width**: the overview and the plugins page fill the window. Why: an agreed product decision, as homedeck does, so a wide screen has no blank band.

**Card grid**:

```css
grid-template-columns: repeat(auto-fill, minmax(max(min(var(--card-min), 100%), (100% - (var(--cards-per-row) - 1) * var(--space-4)) / var(--cards-per-row)), 1fr));
gap: var(--space-4);
```

No column is narrower than 220px (`--card-min`), and a row holds at most 4 cards (`--cards-per-row`). A wider window widens the cards instead of adding a fifth. A widened card spans 2 columns, and takes 1 when the grid is narrower than 455px (two 220px columns and a gap). Why: too wide a card puts its number far from its label; too many columns cannot be scanned.

**The trash grid** (`#catalog .cards`): columns of at least 280px (`--card-min-catalog`), aligned at the top, not stretched to one height.

**Breakpoints**:

| Condition | Change |
| --- | --- |
| Viewport ≤ 640px | The sidebar becomes a 48px top bar and a drawer; the page edge is 16px; management rows take one column; detail field names sit above their values |
| Viewport ≤ 1100px | Management rows go from 4 columns to 2 |
| Card grid ≤ 455px | A widened card takes 1 column (a container query) |

Media queries own viewport-level layout; component-level adaptation belongs to container queries. A new breakpoint first says why the two there are not enough. Why: two cover the widths of a desktop browser window, and every breakpoint is one more to test.

**Widths checked**: 520, 768, 1280 and 2964px, in both languages. Headless Chrome lays pages out at least 500px wide, so narrow layouts are checked at 520.

### 1.9 Sizes [Shipped]

| Token | Value | Used for |
| --- | --- | --- |
| `--control-h` | 44px | The minimum height of every button, field, select and `summary`; a sidebar row's height |
| `--icon-xs` | 18px | Vector icons in the sidebar |
| `--icon-sm` | 22px | The default small icon tile |
| `--icon-md` | 24px | The sidebar's icon column, which the brand and the entries share |
| `--icon-lg` | 28px | Card icons |
| `--icon-xl` | 32px | Management row icons |
| `--icon-2xl` | 48px | The plus card's circle |
| `--glyph-lg` | 24px | The "⋯" in the More actions button |
| — | 8px | Status dots (`--space-2`) |
| `--dot-row` | 6px | The dots of card rows |
| `--rail-w` | 3px | A card's state rail |
| `--topbar-h` | 48px | The narrow window's top bar |
| `--drawer-w` | `min(280px, 88vw)` | The drawer |
| `--editor-w` | `min(560px, calc(100vw - 32px))` | The dialog |
| `--panel-min` | 176px | An action panel's minimum width |
| `--add-card-min-h` | 192px | The plus card's minimum height |
| `--detail-log-max` | 480px | A detail log's maximum height before it scrolls |
| `--record-max` | 240px | A disk-history record list's maximum height before it scrolls |

Controls are at least 44px. Why: easy to hit with a trackpad or on a touch screen laptop, and well above the 24px WCAG 2.5.8 asks.

## 2 Tokens

### 2.1 Where they are and what they are [Shipped]

- The tokens are the CSS custom properties in the first `:root` block of `web/style.css`: the light theme and every scale.
- The dark theme is the one `:root` block under `@media (prefers-color-scheme: dark)`, and overrides theme colours only.
- Reduced motion is the one `:root` block under `@media (prefers-reduced-motion: reduce)`, and overrides durations only.
- Tools read them with the parser already in `web/assets_test.go` (`collectCSSRules`, `themeTokens`). A new tool reuses it rather than writing another.

**No `tokens.json`.** Why: only the browser and the Go tests read tokens, and both read the CSS; a JSON copy would need a generator or a test keeping it in step. When something does read JSON (say M3's desktop shell drawing native colours), a golden-file test in `web/` will export it from the same parser, updated with `go test ./web -run TestTokens -update`. The CSS stays the only source.

### 2.2 Layers

| Layer | What | Count | Changed when |
| --- | --- | --- | --- |
| L1 theme colours | `--bg` … `--error`, see 1.1.1 | 14, each with a light and a dark value | The theme changes |
| L2 derived | `--tint-*`, `--opacity-disabled`, `--shadow-*`, `--scrim*` | 14 | Rarely; a recipe change means measuring contrast again |
| L3 scales | `--space-*`, `--text-*`, `--leading-*`, `--weight-*`, `--radius-*`, `--dur-*`, `--ease-standard`, `--z-*` | 34 | Changing one changes the whole console; the tests spell out every value |
| L4 component sizes | `--control-h`, `--icon-*`, `--sidebar-w`, `--card-min`, `--cards-per-row` and others | 20 | Their component changes |
| Runtime | `--status` (set by `[data-status]`), `--tint-status` inside `.pill` | 2 | Never written by hand |

Components read L1 to L4 and write no literal colour, size, leading, weight, spacing, radius, duration or easing. Colour literals appear in the two `:root` blocks and nowhere else in the sheet (`TestColoursAreWrittenOnlyInTheThemeBlocks`).

### 2.3 Names [Shipped]

- A scale token is `--kind-step`: `--space-3`, `--text-sm`, `--radius-lg`, `--dur-fast`. Steps are numbers (spacing) or T-shirt sizes (type, radii, icons), one style per kind.
- Theme colours are bare role names with no `--color-` prefix: `--bg`, `--panel`, `--text`, `--muted`, `--line`, `--accent`, `--ok`. Why: the whole sheet and the contrast tests use these names, and a prefix would change nothing on screen.
- A suffix steps the same role further: `-strong` (hover), `-pressed`.
- `on-` names the foreground for a fill: `--on-accent`.
- `tint-` names a derived fill: `--tint-active`.
- Component sizes end in `-w`, `-h`, `-min` or `-max`: `--sidebar-w`, `--topbar-h`, `--card-min`, `--detail-log-max`.
- A token set at runtime by an attribute is read with a fallback: `var(--status, var(--muted))`.

### 2.4 Changing the theme

A theme is a light and a dark value for each of the 14 L1 colours. L2 to L4 stay, and the derived colours follow. "Replacing the theme in one go" means replacing the two `:root` colour blocks in `style.css`:

1. Change the 14 colours in the light `:root` and the same 14 in the dark block.
2. Run `go test -short ./web`; the three contrast tests have to pass (see 5.1).
3. Run `HOSTDECK_E2E=1 go test -count=1 ./e2e/` and look at both languages at 520px and 1280px.
4. Update the table in 1.1.1 and the measurements in 5.1 here.

[Reserved] A theme the user picks needs a settings page, which does not exist, so there is none yet. When there is: `data-theme="name"` on `<html>`, one `:root[data-theme="name"]` block per theme holding those 14 colours only, and the choice kept in `localStorage` as `hostdeck.lang` is. `TestTheStyleSheetKeepsOneLayerWithNoDuplicatedSelectors` refuses a token only the dark theme defines, and a theme block keeps to the same rule: a theme overrides tokens that exist.

### 2.5 Adding and removing tokens

- A token is made when at least two places use a value, or a test checks against it; a size used once can be a component token (such as `--editor-w`).
- It is written in the light `:root` first; the dark block overrides L1 colours only.
- Changing a scale (spacing, type sizes, leading, weights, radii) changes the scale written out in `web/assets_test.go` in the same commit.
- A step of a scale stays even while nothing uses it, as `--space-7` and `--dur-slow` do. Why: a scale says which values are allowed; without 48px there is nothing between 32px and 64px, and the next place that needs 48px would have to add it back.
- A token that is not a step of a scale and that nothing uses is removed. `--ease-exit` went that way: nothing animates on its way out, so an exit curve can never be used.

## 3 Components

### 3.0 States [Shipped]

| State | Looks | Expressed by |
| --- | --- | --- |
| Default | See each component | — |
| Under the pointer | A neutral tint or a darker edge; a button's edge turns accent; a disabled control does not answer | `:hover:not(:disabled)` |
| Pressed | `--tint-pressed` | `:active` |
| Selected | `--tint-active`, or accent words on `--panel` (the segmented control) | `aria-current`, `aria-pressed`, `aria-expanded` |
| Focused | A 2px `--accent` outline, offset 3px | The global `:focus-visible` |
| Disabled | Opacity 0.5, the default cursor | Native `:disabled` |
| Loading | The control disabled, its words saying what is under way, `aria-busy="true"` on the container | No spinners |
| Error | `--error` words, or an edge with a `--tint-error-panel` fill | `[data-kind="error"]`, `[data-status="error"]` |
| Armed | `--error` edge and words; `--tint-error-hover` under the pointer | `.armed`, reset after four seconds |

A state is expressed by a native or ARIA attribute where one exists, and the style selects on that attribute. Why: what a screen reader hears and what the eye sees come from one attribute and cannot disagree.

The three button hover rules are written `:hover:not(:disabled)`. Why: a button that cannot be pressed would look as if it could (`TestADisabledButtonDoesNotAnswerThePointer`).

### 3.1 Buttons [Shipped]

**Size**: one. At least 44px tall, padding 4px 12px, 13px words, 6px corners, a 1px edge. Why: every control is 44px, and density is set by type and spacing rather than by several button sizes.

| Variant | Selector | Default | Under the pointer | Pressed | Selected | Disabled | Loading |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Secondary (default) | `.btn` | `--panel` fill, `--line` edge, `--text` words | `--accent` edge | `--tint-pressed` | — | 0.5 opacity, no hover | Disabled, words such as "Refreshing…" |
| Primary | `.btn.primary` | `--accent` fill and edge, `--on-accent` words | `--accent-strong` | `--accent-pressed` | — | 0.5 opacity, no hover | The same |
| Armed | `.btn.armed` | `--error` edge and words | `--tint-error-hover` fill | — | — | — | — |
| Icon | `.more-trigger` | 44×44, no fill or edge, `--muted`, a 24px glyph, 8px corners | `--bg` fill, `--line` edge, `--text` | `--tint-pressed` | `aria-expanded="true"` looks as under the pointer | — | — |
| Segmented | `.lang button` | No fill, `--muted` words, 13px, padding 4px 8px, 6px corners | `--line-strong` edge, `--text` words | `--tint-pressed` | `aria-pressed="true"`: `--panel` fill, `--accent` words, `--line` edge | — | — |

The segmented control around them: padding 4px, gap 4px, a 1px `--line` edge, 8px corners, `--bg` fill.

Use:

- At most one primary button in a region: "Refresh" on the overview, "Add content" on the plugins page, "Save" in the dialog. Why: with one primary button, what to do next is plain.
- The primary button comes last in its group: "Reset layout" then "Refresh"; "Cancel" then "Save".
- A button's words start with a verb (Refresh, Undo, Add, Save, Edit). While its work is under way it is disabled and says "Refreshing…", "Saving…".
- A destructive action (move to the trash, purge, clear a cache) has no standing red button. The first click arms it and its words turn into the confirmation; a second click within four seconds runs it, and a double click is one gesture, not a confirmation. Why: no dialog interrupts the work, and a stray click does nothing.
- An icon-only button has an `aria-label` naming what it acts on, such as "More actions for Grafana".
- A button's words may wrap (detail actions do) but are never cut off.

### 3.2 Fields [Shipped]

**Anatomy**: a visible label above, the field, a hint below.

| Part | Value |
| --- | --- |
| Label `.edit-field` | A column, gap 8px, 13px, `--muted` |
| Field | At least 44px tall, full width, padding 8px 12px, 14px, a 1px `--line-control` edge, 8px corners, `--panel` fill, `--text` words |
| Under the pointer | `--muted` edge (not while focused) |
| Focused | The global focus ring |
| Disabled | 0.5 opacity |
| Hint `.page-hint` | 8px above, 13px, `--muted`; tied to the control it explains with `aria-describedby` (such as the Embed / Link select) |
| Error | See below |

**Kinds**:

- Text: names and the like.
- Address: `type="text"` with `inputmode="url"`, not `type="url"`. Why: it has to take `127.0.0.1:8080` without a scheme. Address fields also turn off `autocomplete` and `spellcheck`.
- Search: `type="search"`, at most 360px wide, shown from eleven plugins.
- Long read-only text: 13px monospace, leading 1.65, at least 180px tall, resizable vertically only.

**Widths in the add form**: fields flex at `1 1 160px`, the address at `2 1 260px`, wrapping when narrow.

**Use**:

- A placeholder is an example, never the label.
- A required field uses native `required` and `reportValidity()`, as an empty name does.

**Errors**: when the server refuses, the error is shown beside the whole form (`[data-kind="error"]`, `role="alert"`), and its words say which field is wrong, such as "That is not a web address."

[Reserved] An error is not yet tied to its field. When an error code belongs to one field and its words cannot say which, that field gets `aria-invalid="true"`, an `--error` edge and an `aria-describedby` to the error's words. Why: the words already meet WCAG 3.3.1 (the error is identified in text), and building the state first would add one nothing uses.

### 3.3 Selects and menus [Shipped]

Two different things here:

**Choosing a value**: a native `<select>` styled as a field (such as the add form's Embed / Link). No custom listbox or combobox. Why: the native control brings its keyboard handling, screen reader support and the system's option list, reliably on both systems.

**Action menus** ("More actions", `.action-panel`):

| Part | Value |
| --- | --- |
| Trigger | `.more-trigger` (3.1) with `aria-expanded` |
| Panel | `popover="manual"`, `position: fixed`; padding 8px; at least 176px wide; at most the viewport less 16px wide and tall, scrolling past that; a 1px `--line` edge; 12px corners; `--panel`; `--shadow-popover` |
| Placement | Its right edge on the trigger's, 4px below it; above it when there is no room below; at least 8px from the viewport's edges |
| Items | Full width, at least 44px tall, padding 8px 12px, 14px, aligned left, 6px corners; `--bg` under the pointer, `--tint-pressed` pressed |
| Destructive items | `--error` words; armed, a `--tint-error-panel` fill |
| Appearing | Fades in over 120ms |

**Behaviour**: Escape closes the panel and returns focus to its trigger; the panel closes when scrolling takes its trigger out of view. A closed panel's items are out of the Tab order.

**Use**: a menu holds infrequent actions; frequent ones sit on the row (a management row's Open and Edit). A destructive item's first click leaves the panel open, so the confirmation happens where it started.

### 3.4 Tables [Reserved]

There is no `<table>` yet. Data is shown three ways: card rows (`.rows`), detail fields (a two-column `<dl>` grid) and the management list (grid rows). M1's ports and processes or Docker containers may be the first table.

A table is for at least 3 columns compared across rows. Otherwise a `<dl>` or a list of rows.

| Part | Value |
| --- | --- |
| Container | On a card surface: a 1px `--line` edge, 12px corners; scrolls sideways when too wide, the scrolling area reachable from the keyboard |
| Header | 13px, 600, `--muted` (5.56:1 on `--panel`), a 1px `--line` below; `position: sticky` with a `--panel` fill when the table scrolls in a bounded height |
| Cells | Padding 8px 12px, 13px, leading 1.5, aligned left; a 1px `--line` below each row but the last |
| Number columns | Aligned right, `tabular-nums` |
| State columns | A status pill (3.7), never a bare dot |
| Row actions | In the last column: at most one direct button, the rest in More actions; a row with actions is at least 44px tall |
| Under the pointer | `--tint-row-hover`, only when the whole row opens something |
| Zebra stripes | None. Why: the dividers already separate rows, and stripes under a hover tint get muddy |
| Narrow ≤ 640px | Each row becomes a `<dl>` block with names above values, as detail fields do |

Sorting, multiple selection and paging: none now; each comes as a product slice when needed. Sorting uses a button in the header and `aria-sort`.

### 3.5 Dialog [Shipped]

There is one dialog element, a native `<dialog>` opened with `showModal()`. It holds the editor, or a service's notifications: a step for each address the browser asks about, each with its heading, what it covers, its answer as a `status` (an `alert` when blocked, in `--error`) and an Allow button, and Close alone in the foot. A step's button goes once its answer is On. A page cannot ask again for an address the browser blocked, so a blocked step says where to turn it on and follows the change by itself: the page step names the icon left of this page's address bar and has no button; the push step's button becomes Open 127.0.0.1:<port>, a full tab, since a tab is sure to show the address bar. Why: the user is never left to find the setting for a port they did not know about.

| Part | Value |
| --- | --- |
| Size | `--editor-w`, that is `min(560px, calc(100vw - 32px))`; at most the viewport less 32px tall |
| Surface | No padding, a 1px `--line` edge, 16px corners, `--panel`, `--shadow-dialog`, `--scrim-dialog` on `::backdrop` |
| Head | Padding 24px 24px 16px; an `h2` title at 20px, 600, at most two lines |
| Body | Padding 0 24px 24px; only the body scrolls when long; its explanation 13px, leading 1.65, `--muted`; 16px between fields |
| Foot | Aligned right, 8px between buttons, padding 16px 24px, a 1px `--line` above; "Cancel" (secondary) then "Save" (primary) |
| Appearing | Fades in over 200ms |

**Behaviour**: focus stays inside — Tab and Shift+Tab cycle through enabled `input`, `select`, `textarea` and `button` controls; Escape and Cancel close it, except Escape while saving; closing returns focus to the button that opened it. A refused save keeps the draft and shows the error inside; a successful save closes it and announces the result on the row that changed. `main` does not scroll while it is open. The task form, the script form and the notifications dialog keep that foot inside a 640px-tall window at 1280 and 520px wide; the task form scrolls its body there. `TestAShortWindowKeepsDialogActionsInView`.

**Use**:

- No dialog asks for confirmation; destructive actions arm instead (3.1). Why: a confirmation dialog interrupts, and teaches people to press OK without reading.
- A dialog holds one task's form, and never opens another dialog.

### 3.6 Navigation [Shipped]

**Sidebar**: 240px wide, padding 16px 12px, 12px between its blocks, `--panel`, a 1px `--line` on its right.

| Part | Value |
| --- | --- |
| Brand | At least 44px tall, 16px, 600, padding 0 8px; the mark is `--panel` on `--accent`, 14px, 700 |
| Rows | 44px tall, 8px corners, 4px apart; three columns `24px 1fr 12px` with an 8px gap, padding 0 8px |
| Icon column | 24px, shared by the brand and the entries; vector icons 18px, `--muted`, `--accent` when selected |
| Names | Cut with an ellipsis when too long |
| State column | 12px, centring a status dot or the external-link arrow "↗" |
| Under the pointer | The whole row `--bg` |
| Selected | The whole row `--tint-active`; the link has `aria-current="page"` |
| More actions | On every row: move up, move down, hide from workspace |
| Foot | A 1px `--line` above, 8px padding above; the Pages & plugins entry and the language switch |

**Narrow (≤ 640px)**: the sidebar's head becomes a fixed 48px top bar (`--panel`, a 1px edge below) showing a Menu button; the navigation becomes a drawer from the left, `min(280px, 88vw)` wide, padding 12px, sliding in over 200ms and leaving at once; the page beside it is covered by `--scrim`. While it is open, focus stays in the sidebar and the content is `inert`; Escape, the scrim or a language change closes it and returns focus to Menu.

**Skip link**: the page's first element. Out of view until focused, then in the top left corner, 8px in: padding 8px 16px, a 1px `--line` edge, 6px corners, `--panel`, 13px.

**Not built**: tabs and breadcrumbs. Why: every view is one sidebar entry, one level deep, and the sidebar already says where you are.

### 3.7 Tags [Shipped]

| Kind | Selector | Value | Used for |
| --- | --- | --- | --- |
| Status pill | `.pill` | 12px, padding 4px 8px, 999px corners, 8px above; `--status` words on a 10% tint of `--status` | A card's or detail's state: OK, Warning, Problem |
| Capability tag | `.catalog-provides li` | 12px, `--muted`, padding 4px 8px, a 1px `--line` edge, 999px corners | What a plugin provides (built-in plugins) |
| Capability words | `.catalog-entry .catalog-provides li` | 12px, leading 1.65, no edge, separated by " ·" | The same on a management row, in less room |
| Kind | `.catalog-kind` | 12px, `--muted` | A plugin's kind |
| State words | `.catalog-state` | 13px, 600; enabled `--ok`, disabled `--muted`, broken `--error` | A management row's state |

Use:

- A status pill always says its state in words, never by colour alone (5.5).
- Tags show; they are not clicked. Anything clickable is a button.

The pill's tint is 10% of its state colour. Why: its 12px word has to reach 4.5:1 in both themes, and at 16% the four light pills and the dark Problem pill did not (5.1).

### 3.8 Feedback [Shipped]

| Kind | Selector | When | Role | Look | Lasts |
| --- | --- | --- | --- | --- | --- |
| Banner | `#banner`, `#service-error` | The console or the open service has a problem, such as an unreachable backend | `alert` | Margin 12px 16px 0, padding 12px, 8px corners, `--tint-error-soft` | Until the problem is gone |
| Result note | `.note` | What was just done has a result, with at most one Undo | `status` | Padding 8px 12px, 13px, `--tint-accent`, a 1px edge below | The layout note: eight idle seconds, paused under the pointer or focus |
| Local note | `.detail-note`, `.catalog-note` | The result of an action on one row or form | `status`, or `alert` on failure | Padding 8px 12px, 8px corners, `--tint-accent`; on failure `--tint-error-panel` and `--error` words | Eight seconds on the detail view |
| Card error | `.card-error` | A card could not be read | — | `--error` words, 12px above | Until the next reading |
| Overview note | `#watch-error`, `#watch-missing` | `notifications.json` was refused, or watches a card that is not there | `alert`, its words written only when they change, so the 15-second reading does not say them again | As the banner, but inside the overview: margin 0 0 12px | Until the next reading of the notifications says otherwise |
| Under way | Button words, `.card-pending` | An action or a reading has not finished | `aria-busy` on the container | 13px, `--muted` | Until it finishes |
| Screen reader | `#live-region` | What cannot be seen, such as how many cards or search matches there are | `status` | Visually hidden | The same words are never said twice |

Rules:

- A success is a `status`, a failure an `alert`. The role is written before the words; when only the urgency changes, the words are written again so a screen reader speaks them (`feedback()` in `web/dom.js`).
- A result appears beside what produced it, not in a corner of the screen, so there are **no toasts**. Why: the eyes are on the button just pressed, and the result belongs there.
- No browser `alert()` or `confirm()`.
- Notes fade in (1.7) and leave at once.

### 3.9 Cards [Shipped]

A card is the overview's unit.

| Part | Value |
| --- | --- |
| Surface | Padding 16px, a 1px `--line` edge, 12px corners, `--panel` |
| Head | At least 44px tall, gap 8px: a 28px icon (1px `--line` edge, 600), the name at 15px 600, More actions |
| Main value | 32px, 600, leading 1.25, `tabular-nums`, letter-spacing -0.025em; on one baseline with its label, gap 8px, 12px above |
| Label | `--muted`, `flex: 1 1 8em`, wrapping when there is no room |
| Rows | 13px; 4px above and below each, gap 8px: a 6px dot, the name (`--muted`), the value (at most 55% of the row) |
| Read time | 12px, `--muted`, 16px above, 12px padding above, a 1px `--line` above |
| Watch lines | Only on a card `notifications.json` watches, after the read time: 12px, 4px above, `--muted`; when the notification command last failed, a second line in `--error` words. Written into the card every 15 seconds without rebuilding it |
| Under the pointer | `--line-strong` edge; a card that opens a view also gets `--tint-card-hover`, the lift shadow and a pointer cursor |
| States | `[data-status="warn"]` and `[data-status="error"]` change the edge colour and draw the 3px rail |
| Widened | `.wide` spans 2 columns, 1 when the grid is narrower than 455px |
| Being dragged | 0.4 opacity |

**The plus card** (`.add-card`): always the grid's last cell. A 1px dashed `--line` edge, 12px corners, at least 192px tall, padding 24px, centred, gap 8px, `--muted`; a 48px `--bg` circle holding a 32px `--accent` "+"; a 15px `--text` title at 600 and a 13px hint. Under the pointer its edge and words turn `--accent`. It is not ordered with the cards and not saved in the layout.

**Use**: numbers and long words wrap inside the card (`overflow-wrap: anywhere`), never spilling out or being cut.

### 3.10 Management rows [Shipped]

Each plugin on the Pages & plugins page is a row (`.catalog-entry`, also a `.card`).

| Width | Columns |
| --- | --- |
| > 1100px | `minmax(0, 1.5fr) minmax(0, 1fr) 136px 272px`: identity, summary, state, actions; 24px between columns, 16px between rows, at least 128px tall, centred vertically |
| 641–1100px | `minmax(0, 1fr) auto`: identity on its own line, summary beside state, actions on their own line |
| ≤ 640px | One column; summary and state each in a line, actions aligned left and wrapping |

- A 32px icon; the name at 15px on one line with an ellipsis; the description 13px, leading 1.65, `--muted`, at most two lines, at least 42px tall, aligned with the name (indented by the icon and 8px).
- Under the pointer: `--line-strong` edge, `--tint-row-hover` fill.
- A broken plugin's row has an `--error` edge, and its error spans the row.
- Built-in plugins (`.catalog-offer`) use `1fr 208px 184px` and show their description in full.

### 3.11 Detail items [Shipped]

| Part | Value |
| --- | --- |
| View | Padding 24px (16px narrow), `--bg` |
| Item | Padding 24px (16px narrow), 16px below, a 1px `--line` edge, 12px corners, `--panel` |
| Name | 16px, 600 |
| Main value | 24px, 600, 12px above and below. An optional label sits on the same baseline, gap 8px, in `--muted`, and wraps when the row is too narrow. The compact list does not show that label |
| Fields | A `<dl>` grid `minmax(80px, 160px) 1fr`, gap 8px 16px, 13px, names `--muted`; one column when narrow, names above values |
| Log | A `<details>`, 13px; its `<pre>` at most 480px tall and scrolling, padding 16px, `--bg`, a 1px `--line` edge, 6px corners |
| Actions | Wrapping, gap 8px, 16px margin and 16px padding above, a 1px `--line` above; button words aligned left and wrapping |

### 3.11.1 Disk history [Shipped]

The native disk-history view has a centred 960px maximum reading width (`--disk-content-w`), 24px outer padding, 16px panel padding, 12px gaps and the shared panel, edge, radius and type tokens. Current size and period change are 24px semibold in two columns; below 460px of content width they stack. The order is scan metadata, configured target and range, breadcrumb, confirmed scan controls, coverage notes, the two values, size curve, period changes, direct subfolders and collapsed scan readings. This lets a reader find the amount, time and cause without reading repeated field cards. Only this view has the reading-width cap.

Charts use native SVG, the accent for size and shrinkage, error colour for growth, signed values and written explanations; colour alone never conveys direction. Arrow keys/Home/End read values, Enter selects a period, Escape clears it, and scan values also appear in a bounded 240px scrollable table. A period selection updates the comparison and folder list while retaining the complete curve. The SVG viewport and tick density adapt to the actual view width, preserving legible labels. Folder rows are semantic tables with 44px named buttons, initially 20 rows. Paths wrap, timestamps use browser-local time and unavailable comparisons use an em dash with a reason. Controls keep focus across reads; polling never rebuilds an armed confirmation.

`TestDiskHistoryLetsReadersFollowTheNumbersThroughTimeAndIntoADirectory` checks the values, period selection, navigation, refresh, chart keyboard controls and layout in both languages, both themes and 1280/768/520px windows. The installation browser test retains real temporary-file scan/tracking and Origin coverage; console tests keep reading free of writes and reject invalid selections or invented deletions.

### 3.12 Disclosures [Shipped]

Secondary content (the connection help, the plugins folder, logs, prompt previews) sits in a `<details>`. Its `<summary>` is at least 44px tall, `--muted` or 600, with a pointer cursor. Why: native disclosure needs nothing extra for the keyboard or screen readers, and the first screen keeps what can be done directly.

### 3.13 Status dots [Shipped]

8px dots in the sidebar's state column, in three states:

| State | Looks |
| --- | --- |
| The address answered | A filled `--ok` disc |
| It did not | A 2px `--error` ring, the sidebar showing through |
| Nothing heard yet | A filled `--line-control` disc |

All three use the same 8px box, so the column does not move. A dot has `role="img"`, an `aria-label` and a `title`, so a screen reader says its state. Why: red and green alone are one colour to many eyes (WCAG 1.4.1), so the state that matters most, no answer, has a shape of its own; the grey disc differs from the green one in lightness, and shows only just after opening. (`TestASidebarDotSaysUpOrDownByItsShapeAsWellAsItsColour`)

## 4 Patterns

### 4.1 Page layout [Shipped]

| View | Structure |
| --- | --- |
| Overview | Header (a 16px `h1` on the left; "Reset layout", "Add content" and "Refresh" on the right, Refresh the only primary; 12px gap when it wraps, 16px below; the header scrolls with the page; "Add content" opens the same add area as the plus card) → the card grid (the plus card last) |
| Pages & plugins | Header (`h1` and "Add content") → search (from eleven plugins) → Enabled → Disabled → Needs attention → Trash → Add content (the form, built-in plugins, the connection help) → the plugins folder (collapsed) |
| Service | Toolbar (padding 12px 16px, `--panel`, an edge below; the name as a 16px `h1`; Refresh, open in a new tab, More actions) → the iframe filling the rest |
| Detail and monitor | A list of items, see 3.11; a monitor view's card is at most 720px wide |

Each view is a `<section>` named by its own heading, holding one `h1`, with headings going down one level at a time. Only `main` scrolls the content; the sidebar's plugin list scrolls on its own.

### 4.2 Form validation [Shipped]

1. **While typing**: nothing is checked, no error appears. Why: the server decides; only it knows whether `nas.local` resolves.
2. **On submit**: native checks first (`required`, `reportValidity()`). Then the submit button is disabled and says "Saving…", and the form has `aria-busy="true"`.
3. **Refused**: the server answers with an error code, and the page words it from the dictionary; the draft stays; the error appears beside the form with `role="alert"`.
4. **Accepted**: the dialog closes or the add form clears, and the result is announced beside the row added or changed, with `role="status"`.

An error's words say what is wrong and what would be right. For example:

> That is not a web address. Enter one like 127.0.0.1:8080 or http://nas.local:5000.

### 4.3 Empty states [Shipped]

- An empty state is written where the content would be (the sidebar list, the card grid), not only in a banner.
- One sentence says why it is empty, the next how to fill it, with the way in where possible:

> No cards yet. Add one on Pages & plugins, or put plugin folders in {dir} and use plugin reload.

- `.empty`, `--muted`, long paths wrapping anywhere.
- An empty overview still shows the plus card, the most direct way in.
- A search with no matches says "No matching pages or plugins." where the list was, and announces the count.

### 4.4 Error pages [Shipped]

The console routes by hash and has no error pages. An error is shown where the problem is:

| Situation | Where | Words, for example |
| --- | --- | --- |
| An address naming no plugin, or a malformed one | Back to the overview, no 404 | — |
| The backend cannot be reached | The banner | Cannot reach the hostdeck backend: {detail} |
| A plugin's `plugin.yaml` is wrong | Kept as a broken entry under Needs attention, with its file in a disclosure | This plugin's plugin.yaml has a problem: {detail} |
| A card cannot be read | `.card-error` in the card | Could not get the value: {detail} |
| An embedded service will not open, or its port is taken | The `#service-error` banner at the top of the service view | Port {detail} belongs to this plugin, and another program is using it. … |
| The first reading never arrived | Where the card grid would be | — |

Why: a console for one machine has no "page not found"; what matters is which service has which problem, not a generic error page.

### 4.5 Loading and skeletons [Shipped]

**No skeleton screens.** Why: the data is on this machine and mostly arrives in tens of milliseconds; a skeleton that flashes past is more jarring than the content itself.

| Situation | Handling |
| --- | --- |
| The overview's first load | "Loading…" (`role="status"`) where the grid will be, `aria-busy="true"` on the grid |
| A card still being read | That card shows 13px `--muted` words saying so, the others show as usual; it is asked for again every two seconds |
| Refresh pressed | The button is disabled and says "Refreshing…" |
| A detail view reading again | What it showed stays, its refresh is disabled, and the new reading replaces it |
| A card's control has focus | A new reading waits until focus leaves, so the panel in use is not swept away |
| A detail action runs | Once confirmed, the button is disabled and says "Running…"; the result goes to the view's local note, and the list is read again 1.5 seconds later |
| Clearing an embedded service's cache | The second click closes the panel, and progress shows on the toolbar, not in the panel |

## 5 Accessibility and usability

### 5.1 Colour contrast [Shipped]

**Floors**:

- Text, 12px and 13px included: 4.5:1.
- 24px and up, or 18.66px and up in bold: 3:1.
- Non-text (focus ring, control edges, status dots, rails): 3:1 (WCAG 1.4.11).

How it is measured: WCAG relative luminance; a tint is mixed as `color-mix(in srgb)` mixes it before measuring; a translucent tint is laid over the surface it sits on first.

**Gates**: three tests in `web/assets_test.go`, each run for both themes.

- `TestTheConsolesColoursStayReadableOnTheirOwnBackgrounds`: 10 pairs of plain text and background colours, 4.5:1.
- `TestWordsStayReadableOnTheTintsTheyAreWrittenOn`: 16 kinds of words written on a tint. It reads each recipe from the rule that paints it, resolves `var()` fallbacks and `color-mix(in srgb)`, lays it over the component's surface, and wants 4.5:1. A rule that moves fails the test instead of escaping it.
- `TestFieldEdgesTheFocusRingAndStatusDotsStandOutFromTheirSurfaces`: a field's edge at rest and under the pointer, the focus ring and the three status dots, 3:1.

**Measured (2026-10-25, `web/style.css` on main)**:

| Pair | Light | Dark | Floor |
| --- | --- | --- | --- |
| OK / Warning / Problem / neutral pills, 10% tint | 4.63 / 4.53 / 4.76 / 4.88 | 5.95 / 6.25 / 4.67 / 5.23 | 4.5 |
| An armed button under the pointer: `--error` on `--tint-error-hover` | 4.62 | 4.56 | 4.5 |
| An error note: `--error` on `--tint-error-panel` | 4.77 | 4.71 | 4.5 |
| The open sidebar entry: `--text` on `--tint-active` | 13.53 | 10.60 | 4.5 |
| A primary button's words, under the pointer / pressed | 7.04 / 8.87 | 8.21 / 10.01 | 4.5 |
| A field's edge `--line-control` on `--panel` / `--bg` | 3.35 / 3.12 | 3.14 / 3.50 | 3.0 |
| A field's edge under the pointer, `--muted`, on `--panel` / `--bg` | 5.56 / 5.19 | 6.20 / 6.89 | 3.0 |
| The focus ring `--accent` on `--panel` / `--bg` | 5.61 / 5.24 | 5.88 / 6.54 | 3.0 |
| Status dots: answered / no answer (ring) / nothing heard yet | 5.32 / 5.53 / 3.35 | 7.17 / 5.37 / 3.14 | 3.0 |

The pill row was measured in headless Chrome, reading the computed colours and laying them over `--panel`; the rest are computed with the same formula the tests use.

Where some values come from:

- The pill tint went from 16% to 10%. At 16% the four light pills were 4.18–4.49 and the dark Problem pill 4.28. 10% is the smallest change that clears every pill, but the light Warning pill is only 0.03 above the floor, which is why a gate measures it.
- An armed button under the pointer used to borrow the banner's 14% red at 4.44–4.48, so it has a 12% of its own.
- `--line-control` is the lightest grey between `--line-strong` and `--muted` that clears 3.1:1 on both surfaces; the extra tenth absorbs browser rounding. A field's edge used to be `--line`, at 1.17–1.37:1.
- `--line-control` is for fields and status dots, not for buttons or cards. Why: a button has its label and a card its content, and WCAG 1.4.11 does not ask 3:1 of their edges; it asks it of the edge that says "type here". Cards and dividers keep `--line`, and the console stays quiet.

### 5.2 Focus [Shipped]

- The global `:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }`. Never `outline: none` without something in its place.
- Focus moves in DOM order; no positive `tabindex`.
- The dialog and the drawer keep focus inside, and return it to what opened them when they close.
- When a focused control goes away, focus moves to a sensible neighbour (Reset layout leaving moves it to Refresh), never back to `body`.
- The skip link is first in the Tab order.

### 5.3 Keyboard [Shipped]

| Keys | Do |
| --- | --- |
| Tab / Shift+Tab | Move between controls in DOM order; closed panels and the closed drawer are skipped |
| Enter / Space | Activate buttons, links and `summary` as the browser does |
| Escape | Close the action panel, the drawer or the dialog (not while saving), returning focus to what opened it |
| Reordering | "Up" and "Down" in More actions; dragging is the other way |

**No single-key shortcuts** such as `j` / `k`. Why: embedded pages and fields use the keyboard too, and single keys would collide with them.

### 5.4 Target size and pointer [Shipped]

- Everything clickable is at least 44×44px. The two exceptions, `.move` and `.card-width`, appear only in action panels, whose items are themselves 44px tall.
- Everything clickable sets `touch-action: manipulation`.
- Dragging has a keyboard alternative (5.3). An address dropped on the console is taken only from an outside service; dragging the console's own links adds nothing.

### 5.5 Not by colour alone [Shipped]

- Card states: the rail and edge colour, plus a pill that says the state.
- Management row states: words such as "Enabled" and "Disabled".
- Destructive actions: besides the red, the button's words turn into the confirmation.
- Sidebar dots: no answer is a ring, an answer a filled disc (3.13).

### 5.6 Motion [Shipped]

- `prefers-reduced-motion` is honoured (1.7).
- Nothing flashes, auto-rotates or parallaxes.
- Motion only says that something appeared or where something went; it is never decoration.

### 5.7 Semantics and screen readers [Shipped]

- Landmarks: the sidebar is one named `complementary` holding the only `navigation`; each view is a region named by its heading; `main` can take focus.
- Headings: one `h1` per view, going down one level at a time.
- State: `aria-current`, `aria-pressed`, `aria-expanded` and `aria-busy` follow the real state.
- External links, popovers and code samples say in their names what they are.
- Words are written with `textContent`, never `innerHTML`. Why: a string a plugin supplies must never run as HTML.

### 5.8 Voice [Shipped]

**Overall**: a colleague who knows the system explaining what happened: clear, specific, calm.

| Rule | Do | Do not |
| --- | --- | --- |
| Say what happened, then what to do | The environment variable {detail} is not set. Start hostdeck with it set. | Something went wrong, try again. |
| Put technical detail after a colon | The layout could not be saved: {detail} | Show an error code to the user |
| Do not demand when the user did nothing wrong | The plugin no longer offers this action on this item. The list is read again. | Your action is invalid. |
| Buttons are verbs | Refresh, Undo, Add, Save, Hide from workspace | OK, Confirm |
| States are short words | OK, Warning, Problem (not Error) | Critical failure |
| Work under way ends in "…" | Refreshing…, Saving… | Please wait |
| Stay calm | End with a full stop | Exclamation marks (none in either language today) |

**English**: sentence case, buttons included; no contractions such as can't or don't (a possessive 's is fine); the console calls itself hostdeck, never we.

**Chinese**:

- No 「您」 (none today); 「请」 only where the user is asked to do something (ten today).
- Full-width punctuation; names on screen in Chinese quotation marks, such as “页面与插件”.
- Commands, file names and field names stay as they are, with a space between them and Chinese: “这个插件的 plugin.yaml 有问题”, “然后用 plugin reload 重新加载”.
- Translate by what a Chinese speaker would say, not word for word: Warning is 「注意」, not 「警告」; Problem is 「异常」; Read at {time} is 「读取于 {time}」.

**Both languages**: one key carries the same meaning and urgency in both; new words go into both dictionaries together (`web/i18n_test.go` checks).

## 6 Implementation

### 6.1 Files [Shipped]

```text
web/
  index.html        # the page's shell: landmarks, the three views, the dialog, the live region
  style.css         # tokens and every component, one layer, see 6.2
  *.js              # one module per view or kind of component
  dom.js            # el(), announce(), feedback(): DOM helpers every module shares
  panel.js          # More actions panels
  confirm.js        # two-click confirmation
  editor.js         # the editor dialog
  i18n/en.json      # English dictionary
  i18n/zh-CN.json   # Chinese dictionary, the same keys
  assets_test.go    # stylesheet gates: one layer, spacing, type, colour, radii, contrast, motion
  i18n_test.go      # the two dictionaries share their keys
  embed.go          # embeds the files above in the program
e2e/                # real-browser tests: layout, focus, semantics, motion, narrow windows
docs/DESIGN.md      # this document
docs/DESIGN.zh-CN.md
```

**Not added**: a `design/` folder, a component gallery page, `tokens.json`, Python scripts or an npm toolchain. Why: see 6.8.

### 6.2 Organising the stylesheet [Shipped]

`style.css` from top to bottom: tokens (light) → dark tokens → reset and base → layout → components in the order the page meets them → entrances → viewport breakpoints → reduced motion.

Rules:

- A selector is written once per scope, and each breakpoint is one media block (tested). Why: no later rule quietly overrides an earlier one.
- `!important` appears twice: `[hidden]` and the reduced-motion catch-all.
- No inline styles, apart from the `left` and `top` a panel is placed at by measurement.
- No utility classes (such as `.mt-2`, `.text-sm`) and no styles written from JavaScript. Why: a component's style lives in one place and the tests keep its scales; utility classes would go around both.

### 6.3 Naming [Shipped]

| What | Convention | Examples |
| --- | --- | --- |
| Tokens | See 2.3 | `--space-3`, `--tint-active` |
| Component classes | Lower case with hyphens; the component first, its parts after | `.card`, `.card-head`, `.card-value` |
| Variants | One more short class | `.btn.primary`, `.card.wide` |
| States | Native attributes, ARIA attributes or `data-*` | `:disabled`, `[aria-pressed="true"]`, `[data-status="warn"]`, `[data-kind="error"]` |
| Elements the page has once | An `id`, lower case with hyphens | `#overview`, `#catalog-add` |
| Dictionary keys | `area.name`, camel case | `layout.saved`, `catalog.addContent`, `error.not_web` |
| Error codes | Lower case with underscores, from the server; the page finds their words under `error.` | `not_local` → `error.not_local` |
| Test names | A sentence saying the behaviour | `TestTheConsolesTypeStaysOnItsScale` |

### 6.4 Rules and the tests that keep them

| Rule | Test |
| --- | --- |
| One layer, no selector written twice, no token defined for the dark theme only | `TestTheStyleSheetKeepsOneLayerWithNoDuplicatedSelectors` |
| Spacing keeps to the four-point scale, and the scale keeps its values | `TestTheConsolesSpacingStaysOnTheFourPointScale` |
| Type sizes, leading and weights are tokens, and their scales keep their values | `TestTheConsolesTypeStaysOnItsScale` |
| Colour literals only in the theme's `:root` blocks | `TestColoursAreWrittenOnlyInTheThemeBlocks` |
| Radii are `--radius-*`, 0 or 50% | `TestCornersComeFromTheRadiusScale` |
| Plain text colours reach 4.5:1 | `TestTheConsolesColoursStayReadableOnTheirOwnBackgrounds` |
| Words on tints reach 4.5:1 | `TestWordsStayReadableOnTheTintsTheyAreWrittenOn` |
| Field edges, the focus ring and status dots reach 3:1 | `TestFieldEdgesTheFocusRingAndStatusDotsStandOutFromTheirSurfaces` |
| Durations and easing are tokens, never `all` | `TestMotionKeepsToTheDurationAndEasingTokens` |
| Reduced motion takes every duration to zero | `TestReducedMotionTurnsEveryDurationOff` |
| Every JS module is embedded | `TestEveryWebModuleIsEmbedded` |
| The dictionaries share their keys | `web/i18n_test.go` |
| Status dots tell up from down by shape | `TestASidebarDotSaysUpOrDownByItsShapeAsWellAsItsColour` |
| A disabled button does not answer the pointer | `TestADisabledButtonDoesNotAnswerThePointer` |
| Headings and strong words are 600 | `TestHeadingsAndStrongWordsKeepToTheConsolesSemibold` |
| Controls share one look and one state system | `TestEveryControlSharesOneChromeAndOneStateSystem` |
| Layout, focus, semantics, narrow and wide windows, motion | The other browser tests in `e2e/`, such as `wide_window_test.go`, `overview_columns_test.go`, `semantics_test.go`, `motion_test.go` |

### 6.5 Adding a component or a view

1. Look for an existing component here first, and use it if it fits.
2. Use existing tokens. When a new value is needed, add a token by 2.5 and change the scale in the tests in the same commit.
3. Add words to both dictionaries; a new server error code gets words in both languages.
4. Express states with native or ARIA attributes (3.0).
5. Add new colour pairs to the contrast gates.
6. Cover 520px and 1280px, both languages and the keyboard path in browser tests, and reduced motion when something moves.
7. Commit by the slice flow in AGENTS.md: one user-visible behaviour per branch, commit and PR, with the trade-offs in trailers.
8. Update the matching section here, in both languages, in the same commit as the code.

### 6.6 Extending

- **A token**: 2.5.
- **A theme**: 2.4.
- **A component**: write it here as [Reserved] first, saying why the existing ones are not enough; mark it [Shipped] when it is built.
- **A breakpoint**: say why the two there are not enough, and add browser tests at the new width.

### 6.7 Versions

- The design system is versioned with the program, with no version number of its own. Why: one program uses it, and a second number would only fall out of step.
- The change log is `git log`: every commit that changes a rule says what and why in its body, with the trade-offs in `Rejected:` and `Constraint:` trailers.
- A breaking change such as renaming a token is made in one commit across every use and test, with no period where both names exist. Why: with one user, a transition only leaves two names to maintain.
- The measurements here (1.1.1, 5.1) are updated in the commit that changes a colour.

### 6.8 Deliberately not done

| Not done | Why |
| --- | --- |
| `tokens.json` and a generator | Nothing reads JSON, and a second source has to be kept in step (2.1) |
| Style Dictionary, npm, PostCSS | The project uses no npm and has no build step |
| Checks written in Python | The gates are Go tests, CI installs no Python, and the contrast checks are already in Go |
| A component gallery page | A second rendering path no browser test covers, which drifts from the real console; the console with the `e2e/` fixtures is the gallery |
| A 12-column grid | 1.8 |
| A maximum content width | An agreed product decision: fill the window |
| Skeleton screens | 4.5 |
| Toasts | 3.8 |
| A custom select | 3.3 |
| Confirmation dialogs | 3.5 |
| Single-key shortcuts | 5.3 |
| Several button sizes | 3.1 |

## Appendix A: what this round did, and what is left

Writing this document against the code found six places where the code fell short. Each was fixed as a slice, with a test:

| PR | Change | Test |
| --- | --- | --- |
| #91 | The pill tint from 16% to 10%; an armed button under the pointer gets a 12% red of its own | `TestWordsStayReadableOnTheTintsTheyAreWrittenOn` |
| #92 | `--line-control` for field edges and dots nothing has been heard from; a field's edge darkens to `--muted` under the pointer | `TestFieldEdgesTheFocusRingAndStatusDotsStandOutFromTheirSurfaces` |
| #93 | A dot whose address did not answer is a red ring | `TestASidebarDotSaysUpOrDownByItsShapeAsWellAsItsColour` |
| #94 | A disabled button no longer answers the pointer | `TestADisabledButtonDoesNotAnswerThePointer` |
| #95 | Headings and strong words are 600 throughout | `TestHeadingsAndStrongWordsKeepToTheConsolesSemibold` |
| #96 | Colours, radii, weights and easing are tested; the unused `--ease-exit` is removed | `TestColoursAreWrittenOnlyInTheThemeBlocks`, `TestCornersComeFromTheRadiusScale` and others |

One item is left on purpose: tying a server error to its field (the [Reserved] note in 3.2). Today's error words already say which field is wrong; it waits for an error code whose words cannot.

Grouped detail pages place a labelled name/ID search and wrapping counted filter buttons above their items. Search is case-insensitive, trims outer whitespace, intersects the selected group and keeps input focus. Counts reflect the search across groups. Empty data, no search matches, an empty group and a failed reading have distinct messages. Search and group choices last for the browser session, through refreshes and language changes.

Compact detail lists keep the name, written runtime state and actions in one wrapping header with 44px controls. Items use 16px padding and 12px separation; fields use 13px words, a 96px label column and 4px/12px gaps. Fields stay expanded and paths wrap, while logs remain collapsed and bounded at 480px. Disk-history record lists stay bounded at 240px. Other detail layouts keep their larger values and separate action area.

A compact detail state and its descriptive field values can be localized text, selected on the page just like labels. Configuration and log read failures appear as local explanatory fields instead of replacing a healthy task or removing its actions. Log previews remain read-only, collapsed by default, and use the existing scrollable disclosure.

The selected detail filter uses accent words on the accent tint; while pressed its words use the text token on the shared pressed tint, keeping at least 4.5:1 in both themes. The stylesheet tint test measures both states.

Docker containers reuse the compact grouped detail list: running, stopped, paused and needs attention, with localized values and bounded collapsed logs. Shared search and result-count copy refers to items rather than jobs. Item ordering puts errors before warnings, then uses the declared group order and localized name; a paused warning cannot precede a failed container merely because its tab comes first.

Docker details use the shared confirmed action control in the header: Start for created/stopped containers and Restart for running ones. Paused/transition/dead states remain informational. After a successful action, the standard re-read keeps the search and selected group; an item can leave that filtered result as its state changes.

Scheduled-task details reuse compact grouped rows, existing confirmed actions and bounded collapsed logs. The same page has a Tasks | Scripts segment and one primary button for the segment that is showing: New task, or New script. Both forms reuse the existing page dialog. New task writes the name, an enabled checkbox, five cron fields plus an explicit timezone or one interval, a timeout and one command line. The id is shown and is not an input. Edit and Delete sit beside the existing row actions; Delete uses the existing two-click arming. An empty task list is a localized note with the configuration and example paths, not a counted row. Scripts reuse that dialog to save one text file; saving a script does not schedule or run it. Schedule on a script row opens the task form in that same dialog with the default command filled in, and Run uses the existing two-click arming for one execution that does not write the task file. Result fields distinguish reason, observed exit code and revision. Task browser fixtures check both languages, both themes and 1280/520px without adding visual tokens. New task and New script also keep Cancel and Save inside a 640px-tall window (`TestAShortWindowKeepsDialogActionsInView`).

Listening ports reuse the compact grouped detail list: hostdeck, loopback, all interfaces and other addresses, with localized values. Items show port, address, PID, process name, working directory and command line (truncated past 512 runes with a localized suffix); unread directory and command report localized fallback notes rather than blank fields.

Built-in service observations use a Logs and status disclosure on every supported item, including those without log text. After confirmation it opens immediately with Submitting, followed by the operation result and an independently labelled latest runtime state. Only its status, source notes, timestamp and bounded log body change; action nodes, confirmation, focus, filters and scroll remain intact. An expanded visible observation waits one second after a completed read; failures retain content/time and back off for 2, 5 and 10 seconds. Collapse, leaving the page and backgrounding pause reads, and returning reads immediately. Log text follows the end until the reader scrolls up; Go to latest resumes following. Existing content is a separate disclosure. Recent operations sit outside current items/counts and retain access when the item disappears. `TestAnOpenObservationFollowsWithoutReplacingTheFocusedAction` checks both languages/themes at 1280/520px and scroll/focus preservation.

Listeners and services started by hand use the same observation component, including an explicit terminal-output explanation. After a confirmed operation, surviving row/control objects remain mounted; disappeared rows move to recent operations with actions disabled and leave the current filter counts. Browser fixtures count only current-list rows when checking that a stopped process disappeared.

A submitted operation can keep reading its observation while the action response is pending. A failed update shows Unknown while retaining the last successful timestamp and text. Returning during cancellation queues an immediate reading once that request settles, rather than waiting another polling interval. Actions reuse the same button when their offered operation changes, with confirmation text calculated for the current action. Captured output is labelled as Hostdeck output; capture failure is explicit. These behaviors are exercised by `TestObservationPollingWaitsForCompletionBacksOffAndResumesImmediately` and the detached-service reopen acceptance.

An observation opened for reading paints the initial snapshot returned by session creation immediately; its next incremental read waits one second after completion, avoiding a second source lookup before showing any content. Confirmation and action sessions retain their separate validation.

Processes reuse the existing compact grouped detail list, with hostdeck and other-process filters, name/PID search and manual refresh. Each PID remains a separate item even when names match. CPU and memory are descriptive fields without invented health colours; unavailable metrics are localized text. No process actions, new visual tokens or styles are added. `TestTheProcessesPluginInstallsSearchesAndRefreshesWithoutProcessActions` covers native installation, name/PID filtering, retained search after refresh, both languages/themes and 1280/520px.

Process items now also show the executable path, parent process name, start time (explicit UTC), uptime, thread count and accumulated CPU time as existing compact fields. Long paths keep the shared wrapping rule; unavailable optional metadata stays bilingual. Search, filters and refresh retain their behavior, with no new tabs or process actions.
