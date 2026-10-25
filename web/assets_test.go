package web

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// cssDecl is one declaration inside a rule.
type cssDecl struct {
	prop  string
	value string
}

// cssRule is one style rule: the selectors it is written for, the custom
// properties and declarations it holds, and the media query it sits in ("" at
// the top level of the sheet).
type cssRule struct {
	media    string
	selector string
	props    []string
	decls    []cssDecl
	line     int
}

const darkMedia = "(prefers-color-scheme: dark)"

var cssComments = regexp.MustCompile(`(?s)/\*.*?\*/`)

// blankComments empties every comment but keeps its length and newlines, so
// offsets and line numbers still describe the original sheet and a brace
// mentioned in prose cannot be mistaken for a block.
func blankComments(css string) string {
	return cssComments.ReplaceAllStringFunc(css, func(c string) string {
		b := []byte(c)
		for i := range b {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
		return string(b)
	})
}

// splitOutsideParens splits s on sep, ignoring separators inside parentheses,
// so a selector list such as :is(a, b) stays one item.
func splitOutsideParens(s, sep string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], sep) {
			parts = append(parts, s[start:i])
			start = i + len(sep)
		}
	}
	return append(parts, s[start:])
}

// collectCSSRules records every style rule of css between from and to. Rules
// inside an at-rule such as @media recurse with the at-rule's condition as
// their media name; other at-rules hold no rules of their own. lineBase is
// the number of newlines before from, so a rule's line stays the sheet's.
func collectCSSRules(css string, from, to int, media string, lineBase int, rules *[]cssRule) {
	for i := from; i < to; {
		rel := strings.IndexByte(css[i:to], '{')
		if rel < 0 {
			return
		}
		open := i + rel
		depth, cut := 0, -1
		for j := open; j < to; j++ {
			if css[j] == '{' {
				depth++
			} else if css[j] == '}' {
				if depth--; depth == 0 {
					cut = j
					break
				}
			}
		}
		if cut < 0 {
			return
		}
		header := strings.TrimSpace(css[i:open])
		body := css[open+1 : cut]
		line := lineBase + 1 + strings.Count(css[from:open], "\n")
		if strings.HasPrefix(header, "@media") {
			base := lineBase + strings.Count(css[from:open+1], "\n")
			collectCSSRules(css, open+1, cut, strings.TrimSpace(strings.TrimPrefix(header, "@media")), base, rules)
		} else if !strings.HasPrefix(header, "@") {
			for _, sel := range splitOutsideParens(header, ",") {
				if sel = strings.TrimSpace(sel); sel == "" {
					continue
				}
				var props []string
				var decls []cssDecl
				for _, decl := range splitOutsideParens(body, ";") {
					name, value, ok := strings.Cut(decl, ":")
					if !ok {
						continue
					}
					name, value = strings.TrimSpace(name), strings.TrimSpace(value)
					if name == "" || value == "" {
						continue
					}
					decls = append(decls, cssDecl{prop: name, value: value})
					if strings.HasPrefix(name, "--") {
						props = append(props, name)
					}
				}
				*rules = append(*rules, cssRule{media: media, selector: sel, props: props, decls: decls, line: line})
			}
		}
		i = cut + 1
	}
}

func scopeName(media string) string {
	if media == "" {
		return "the top level"
	}
	return media
}

// The sheet is one layer: a selector is written once at the top level, so no
// later pass quietly overrides an earlier one, and once per media condition,
// so each breakpoint lives in a single block. Dark mode overrides the light
// tokens only; a token that exists just for the dark theme would have no
// light value.
func TestTheStyleSheetKeepsOneLayerWithNoDuplicatedSelectors(t *testing.T) {
	data, err := Files.ReadFile("style.css")
	if err != nil {
		t.Fatal(err)
	}
	var rules []cssRule
	collectCSSRules(blankComments(string(data)), 0, len(data), "", 0, &rules)

	written := map[string]int{}
	for _, r := range rules {
		key := r.media + "|" + r.selector
		if first, ok := written[key]; ok {
			t.Errorf("selector %q under %s is written twice (lines %d and %d)", r.selector, scopeName(r.media), first, r.line)
			continue
		}
		written[key] = r.line
	}

	for _, r := range rules {
		declared := map[string]bool{}
		for _, p := range r.props {
			if declared[p] {
				t.Errorf("line %d: %s declares %s twice", r.line, r.selector, p)
			}
			declared[p] = true
		}
	}

	light := map[string]bool{}
	for _, r := range rules {
		if r.media == "" && r.selector == ":root" {
			for _, p := range r.props {
				light[p] = true
			}
		}
	}
	for _, r := range rules {
		if r.media != darkMedia {
			continue
		}
		for _, p := range r.props {
			if !light[p] {
				t.Errorf("line %d: %s is only defined for dark mode", r.line, p)
			}
		}
	}
}

// themeTokens returns the custom properties a :root block declares under one
// media condition: "" for the light theme, darkMedia for the dark one, and
// reduceMedia for a browser that asked for less motion.
func themeTokens(rules []cssRule, media string) map[string]string {
	tokens := map[string]string{}
	for _, r := range rules {
		if r.media != media || r.selector != ":root" {
			continue
		}
		for _, d := range r.decls {
			if strings.HasPrefix(d.prop, "--") {
				tokens[d.prop] = d.value
			}
		}
	}
	return tokens
}

// rootTokens returns the custom properties the light :root block declares.
func rootTokens(rules []cssRule) map[string]string {
	return themeTokens(rules, "")
}

// The console's rhythm is one four-point scale: every gap, inset and row step
// is a spacing token, so cards and rows line up without per-component
// guesswork, and a stray 10px or 6px would sit between steps and drift on its
// own. The tokens themselves are checked against the scale, so widening a
// step cannot quietly redefine the rhythm.
func TestTheConsolesSpacingStaysOnTheFourPointScale(t *testing.T) {
	scale := map[string]string{
		"--space-1": "4px", "--space-2": "8px", "--space-3": "12px", "--space-4": "16px",
		"--space-5": "24px", "--space-6": "32px", "--space-7": "48px", "--space-8": "64px",
	}
	steps := map[string]bool{"0": true}
	for _, v := range scale {
		steps[v] = true
	}

	rules := styleRules(t)
	tokens := rootTokens(rules)
	for name, want := range scale {
		if got := tokens[name]; got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}
	for name := range tokens {
		if strings.HasPrefix(name, "--space-") && scale[name] == "" {
			t.Errorf("%s is not one of the spacing steps %v", name, scale)
		}
	}

	for _, r := range rules {
		for _, d := range r.decls {
			if !spacingProps[d.prop] {
				continue
			}
			for _, part := range strings.Fields(d.value) {
				// A var(), calc() or color-mix() carries its own rhythm.
				if strings.Contains(part, "(") {
					continue
				}
				if !strings.HasSuffix(part, "px") {
					continue
				}
				if !steps[part] {
					t.Errorf("line %d: %s sets %s: %s, and %s is off the four-point scale", r.line, r.selector, d.prop, d.value, part)
				}
			}
		}
	}
}

var spacingProps = map[string]bool{
	"margin": true, "padding": true, "gap": true, "row-gap": true, "column-gap": true,
	"margin-top": true, "margin-right": true, "margin-bottom": true, "margin-left": true,
	"padding-top": true, "padding-right": true, "padding-bottom": true, "padding-left": true,
	"margin-block": true, "margin-inline": true, "padding-block": true, "padding-inline": true,
	"margin-block-start": true, "margin-block-end": true, "margin-inline-start": true, "margin-inline-end": true,
	"padding-block-start": true, "padding-block-end": true, "padding-inline-start": true, "padding-inline-end": true,
}

// Type stays on one scale too: a size or leading is always a token, so a new
// view cannot introduce a one-off 17px or 1.6 that reads as neither step.
func TestTheConsolesTypeStaysOnItsScale(t *testing.T) {
	sizes := map[string]string{
		"--text-xs": "12px", "--text-sm": "13px", "--text-base": "14px", "--text-md": "15px",
		"--text-lg": "16px", "--text-xl": "20px", "--text-2xl": "24px", "--text-3xl": "32px",
	}
	leadings := map[string]string{
		"--leading-tight": "1.25", "--leading-normal": "1.5", "--leading-relaxed": "1.65",
	}

	rules := styleRules(t)
	tokens := rootTokens(rules)
	for name, want := range sizes {
		if got := tokens[name]; got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}
	for name, want := range leadings {
		if got := tokens[name]; got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}

	typeToken := regexp.MustCompile(`^var\(--(?:text|glyph)-[a-z0-9-]+\)$`)
	leadingToken := regexp.MustCompile(`^var\(--leading-[a-z0-9-]+\)$`)
	for _, r := range rules {
		for _, d := range r.decls {
			switch d.prop {
			case "font-size":
				if !typeToken.MatchString(d.value) {
					t.Errorf("line %d: %s sets font-size: %s; use a --text-* or --glyph-* token", r.line, r.selector, d.value)
				}
			case "line-height":
				// A bare 1 is the one exception: an icon symbol wants lines
				// as tall as the glyph itself.
				if !leadingToken.MatchString(d.value) && d.value != "1" {
					t.Errorf("line %d: %s sets line-height: %s; use a --leading-* token", r.line, r.selector, d.value)
				}
			}
		}
	}
}

// srgbChannel turns one 0-255 channel into its linear contribution to a
// colour's relative luminance.
func srgbChannel(v float64) float64 {
	c := v / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// channels reads the red, green and blue of a #rgb or #rrggbb literal, each
// 0-255. A colour written any other way cannot be read here, and says so.
func channels(hex string) ([3]float64, bool) {
	var rgb [3]float64
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 3 && len(hex) != 6 {
		return rgb, false
	}
	digits := hex
	if len(hex) == 3 {
		digits = string(hex[0]) + string(hex[0]) + string(hex[1]) + string(hex[1]) + string(hex[2]) + string(hex[2])
	}
	for i := range rgb {
		n, err := strconv.ParseUint(digits[i*2:i*2+2], 16, 8)
		if err != nil {
			return rgb, false
		}
		rgb[i] = float64(n)
	}
	return rgb, true
}

// luminance is the WCAG relative luminance of a #rgb or #rrggbb literal. A
// token written any other way cannot be measured here, and says so.
func luminance(hex string) (float64, bool) {
	rgb, ok := channels(hex)
	if !ok {
		return 0, false
	}
	var sum float64
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		sum += weight * srgbChannel(rgb[i])
	}
	return sum, true
}

// paint resolves a colour value to the #rrggbb the browser paints over
// surface: a hex literal, transparent, a var() with or without its fallback,
// or a color-mix(in srgb, ...) of two of those. Mixing in srgb weighs each
// channel linearly, and a colour mixed with transparent is that colour at a
// lower opacity over the surface, which comes to the same mix. Anything else
// is not resolved, so a recipe this cannot read fails instead of passing
// unmeasured.
func paint(value string, tokens func(string) string, surface string) (string, bool) {
	value = strings.TrimSpace(value)
	switch {
	case value == "transparent":
		return surface, surface != ""
	case strings.HasPrefix(value, "#"):
		_, ok := channels(value)
		return value, ok
	case strings.HasPrefix(value, "var(") && strings.HasSuffix(value, ")"):
		parts := splitOutsideParens(value[len("var("):len(value)-1], ",")
		if v := tokens(strings.TrimSpace(parts[0])); v != "" {
			return paint(v, tokens, surface)
		}
		if len(parts) == 2 {
			return paint(parts[1], tokens, surface)
		}
	case strings.HasPrefix(value, "color-mix(") && strings.HasSuffix(value, ")"):
		args := splitOutsideParens(value[len("color-mix("):len(value)-1], ",")
		if len(args) != 3 || strings.TrimSpace(args[0]) != "in srgb" {
			return "", false
		}
		first := strings.TrimSpace(args[1])
		cut := strings.LastIndexByte(first, ' ')
		if cut < 0 || !strings.HasSuffix(first, "%") {
			return "", false
		}
		share, err := strconv.ParseFloat(first[cut+1:len(first)-1], 64)
		if err != nil {
			return "", false
		}
		a, okA := paint(first[:cut], tokens, surface)
		b, okB := paint(args[2], tokens, surface)
		ca, okCA := channels(a)
		cb, okCB := channels(b)
		if !okA || !okB || !okCA || !okCB {
			return "", false
		}
		mixed := "#"
		for i := range ca {
			mixed += fmt.Sprintf("%02x", int(math.Round(ca[i]*share/100+cb[i]*(1-share/100))))
		}
		return mixed, true
	}
	return "", false
}

// contrast is the WCAG contrast ratio of two colour literals.
func contrast(a, b string) (float64, bool) {
	la, okA := luminance(a)
	lb, okB := luminance(b)
	if !okA || !okB {
		return 0, false
	}
	hi, lo := la, lb
	if hi < lo {
		hi, lo = lo, la
	}
	return (hi + 0.05) / (lo + 0.05), true
}

// The console's words stay readable on the surfaces they are written on: a
// token is measured against the backgrounds it is drawn over, in both themes,
// and the ratio has to clear WCAG AA. Only the literal colours are measured
// here; the next test measures the tints mixed from them.
func TestTheConsolesColoursStayReadableOnTheirOwnBackgrounds(t *testing.T) {
	pairs := []struct {
		fg, bg string
		why    string
	}{
		{"--text", "--panel", "body text on a card"},
		{"--text", "--bg", "body text on the page"},
		{"--muted", "--panel", "muted text on a card"},
		{"--muted", "--bg", "muted text on the page"},
		{"--accent", "--panel", "a selected control on a card"},
		{"--accent", "--bg", "a selected control on the page"},
		{"--on-accent", "--accent", "the label of a primary button"},
		{"--error", "--panel", "an error on a card"},
		{"--ok", "--panel", "a healthy status on a card"},
		{"--warn", "--panel", "a warning status on a card"},
	}

	rules := styleRules(t)
	light := themeTokens(rules, "")
	dark := themeTokens(rules, darkMedia)
	for _, theme := range []struct {
		name   string
		tokens map[string]string
	}{{"light", light}, {"dark", dark}} {
		// A token the dark theme leaves alone keeps its light value.
		value := func(token string) string {
			if v, ok := theme.tokens[token]; ok {
				return v
			}
			return light[token]
		}
		for _, p := range pairs {
			fg, bg := value(p.fg), value(p.bg)
			got, ok := contrast(fg, bg)
			if !ok {
				t.Errorf("%s: %s is %q or %s is %q, and only plain hex colours can be measured", theme.name, p.fg, fg, p.bg, bg)
				continue
			}
			if got < 4.5 {
				t.Errorf("%s: %s (%s) on %s (%s) is %.2f:1, want at least 4.5:1 for %s", theme.name, p.fg, fg, p.bg, bg, got, p.why)
			}
		}
	}
}

// ruleValue returns what the top-level rule for selector sets prop to. A pair
// that names a rule the sheet no longer has fails, so a component cannot move
// its colours out from under the measurement.
func ruleValue(t *testing.T, rules []cssRule, selector, prop string) string {
	t.Helper()
	for _, r := range rules {
		if r.media != "" || r.selector != selector {
			continue
		}
		for _, d := range r.decls {
			if d.prop == prop {
				return d.value
			}
		}
	}
	t.Fatalf("no top-level %s rule sets %s; point the pair at the rule that paints it now", selector, prop)
	return ""
}

// Words written on a tint stay readable too. A tint is mixed from the theme's
// colours, often with transparent so that the surface under it shows through,
// so each pair reads its colours from the rule that paints them and measures
// them mixed as the browser mixes them, over the surface the component sits
// on. A status pill takes its colour from the --status of the card around it.
func TestWordsStayReadableOnTheTintsTheyAreWrittenOn(t *testing.T) {
	rules := styleRules(t)
	v := func(selector, prop string) string { return ruleValue(t, rules, selector, prop) }
	pairs := []struct {
		why, fg, bg, surface string
		status               string
	}{
		{"a healthy status on its pill", v(".pill", "color"), v(".pill", "background"), "--panel", "--ok"},
		{"a warning on its pill", v(".pill", "color"), v(".pill", "background"), "--panel", "--warn"},
		{"a problem on its pill", v(".pill", "color"), v(".pill", "background"), "--panel", "--error"},
		{"a pill without a state of its own", v(".pill", "color"), v(".pill", "background"), "--panel", ""},
		{"an armed button under the pointer", v(".btn.armed", "color"), v(".btn.armed:hover", "background"), "--panel", ""},
		{"an armed action in a panel", v(".action-panel .armed", "color"), v(".action-panel .armed", "background"), "--panel", ""},
		{"a red action in a panel under the pointer", v(".action-panel > .trash", "color"), v(".action-panel > button:hover:not(:disabled)", "background"), "--panel", ""},
		{"an error note", v(`[data-kind="error"]`, "color"), v(`[data-kind="error"]`, "background"), "--panel", ""},
		{"text on the error banner", "var(--text)", v(".banner", "background"), "--bg", ""},
		{"text on a note", "var(--text)", v(".note", "background"), "--panel", ""},
		{"the open sidebar entry", "var(--text)", v(".nav-row:has(.nav-item.active)", "background"), "--panel", ""},
		{"a pressed button", "var(--text)", v(".btn:active:not(:disabled)", "background"), "--panel", ""},
		{"a primary button under the pointer", v(".btn.primary", "color"), v(".btn.primary:hover", "background"), "--panel", ""},
		{"a pressed primary button", v(".btn.primary", "color"), v(".btn.primary:active", "background"), "--panel", ""},
		{"a card's label under the pointer", "var(--muted)", v(".card:has(.card-name a):hover", "background"), "--panel", ""},
		{"a plugin's description under the pointer", "var(--muted)", v(".catalog-entry:hover", "background"), "--panel", ""},
	}
	pillTint := v(".pill", "--tint-status")

	light := themeTokens(rules, "")
	dark := themeTokens(rules, darkMedia)
	for _, theme := range []struct {
		name   string
		tokens map[string]string
	}{{"light", light}, {"dark", dark}} {
		for _, p := range pairs {
			tokens := func(name string) string {
				switch name {
				case "--status":
					if p.status == "" {
						return ""
					}
					return "var(" + p.status + ")"
				case "--tint-status":
					return pillTint
				}
				if value, ok := theme.tokens[name]; ok {
					return value
				}
				return light[name]
			}
			surface, okS := paint("var("+p.surface+")", tokens, "")
			fg, okF := paint(p.fg, tokens, surface)
			bg, okB := paint(p.bg, tokens, surface)
			if !okS || !okF || !okB {
				t.Errorf("%s: %s: cannot resolve %q on %q over %s", theme.name, p.why, p.fg, p.bg, p.surface)
				continue
			}
			if got, _ := contrast(fg, bg); got < 4.5 {
				t.Errorf("%s: %s is %s on %s (%s over %s), %.3f:1; want at least 4.5:1", theme.name, p.why, fg, bg, p.bg, p.surface, got)
			}
		}
	}
}

const reduceMedia = "(prefers-reduced-motion: reduce)"

// zeroDuration reports whether a duration is nothing at all, however it is
// written, including the !important a catch-all needs to win.
func zeroDuration(value string) bool {
	return strings.HasPrefix(value, "0ms") || strings.HasPrefix(value, "0s")
}

// Motion keeps to the duration tokens, so a one-off 250ms cannot appear beside
// the console's own three steps, and "all" cannot quietly animate a property a
// test reads the moment it changes, such as a disabled control's fade.
func TestMotionKeepsToTheDurationTokens(t *testing.T) {
	bare := regexp.MustCompile(`^[\d.]+m?s$`)
	motion := map[string]bool{
		"transition": true, "transition-duration": true,
		"animation": true, "animation-duration": true,
	}
	for _, r := range styleRules(t) {
		for _, d := range r.decls {
			if !motion[d.prop] {
				continue
			}
			for _, part := range strings.Fields(d.value) {
				if part == "all" {
					t.Errorf("line %d: %s sets %s: %s; name the properties, never \"all\"", r.line, r.selector, d.prop, d.value)
					continue
				}
				if bare.MatchString(part) && !zeroDuration(part) {
					t.Errorf("line %d: %s sets %s: %s, and %s is not a --dur-* token", r.line, r.selector, d.prop, d.value, part)
				}
			}
		}
	}
}

// Reduced motion takes the durations down to nothing at the source: the tokens
// every transition reads, and a catch-all for a rule that names one of its own.
func TestReducedMotionTurnsEveryDurationOff(t *testing.T) {
	rules := styleRules(t)

	tokens := themeTokens(rules, reduceMedia)
	if len(tokens) == 0 {
		t.Fatalf("no :root rules under %s: the sheet ignores a browser that asks for less motion", reduceMedia)
	}
	for _, name := range []string{"--dur-fast", "--dur-base", "--dur-slow"} {
		got, ok := tokens[name]
		if !ok {
			t.Errorf("%s is not zeroed under %s: a transition that reads it still plays", name, reduceMedia)
			continue
		}
		if !zeroDuration(got) {
			t.Errorf("%s is %s under %s, want 0ms", name, got, reduceMedia)
		}
	}

	catchAll := false
	for _, r := range rules {
		if r.media != reduceMedia || r.selector != "*" {
			continue
		}
		for _, d := range r.decls {
			if d.prop != "transition-duration" && d.prop != "animation-duration" {
				continue
			}
			if !zeroDuration(d.value) {
				t.Errorf("line %d: * sets %s: %s under %s, want 0ms", r.line, d.prop, d.value, reduceMedia)
			}
			catchAll = true
		}
	}
	if !catchAll {
		t.Errorf("%s has no * rule zeroing transition-duration: a rule that names its own duration keeps it", reduceMedia)
	}
}

func styleRules(t *testing.T) []cssRule {
	t.Helper()
	data, err := Files.ReadFile("style.css")
	if err != nil {
		t.Fatal(err)
	}
	var rules []cssRule
	collectCSSRules(blankComments(string(data)), 0, len(data), "", 0, &rules)
	return rules
}

// Every script the page loads is embedded: a module the binary does not carry
// would answer 404 at runtime, which no unit test of the handlers would see.
func TestEveryWebModuleIsEmbedded(t *testing.T) {
	embed, err := os.ReadFile("embed.go")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".js") {
			continue
		}
		if !strings.Contains(string(embed), name) {
			t.Errorf("embed.go does not embed %s: the console would answer 404 for it", name)
		}
	}
}
