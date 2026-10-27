package web

import (
	"strings"
	"testing"
)

// The page-editor focus trap must include textarea so script editing
// (name disabled, focus on content) still cycles Tab through the body field.
func TestThePageEditorFocusCycleIncludesTextareas(t *testing.T) {
	data, err := Files.ReadFile("nav.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	const want = `editor.querySelectorAll('input, select, textarea, button')`
	if !strings.Contains(src, want) {
		t.Fatalf("onInterfaceKey focusable list must include textarea; want %q in nav.js", want)
	}
	if strings.Contains(src, `editor.querySelectorAll('input, select, button')`) {
		t.Fatal("onInterfaceKey still lists only input, select, button")
	}
}
