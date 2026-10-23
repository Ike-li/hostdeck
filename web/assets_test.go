package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// cssRule is one style rule: the selectors it is written for, the custom
// properties it declares, and the media query it sits in ("" at the top
// level of the sheet).
type cssRule struct {
	media    string
	selector string
	props    []string
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
				for _, decl := range splitOutsideParens(body, ";") {
					if name, _, ok := strings.Cut(decl, ":"); ok {
						if name = strings.TrimSpace(name); strings.HasPrefix(name, "--") {
							props = append(props, name)
						}
					}
				}
				*rules = append(*rules, cssRule{media: media, selector: sel, props: props, line: line})
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
