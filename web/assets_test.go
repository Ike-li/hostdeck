package web

import (
	"os"
	"regexp"
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

// rootTokens returns the custom properties the light :root block declares.
func rootTokens(rules []cssRule) map[string]string {
	tokens := map[string]string{}
	for _, r := range rules {
		if r.media != "" || r.selector != ":root" {
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
