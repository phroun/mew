package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// Browse mode hides dokuwiki inline markers and keeps the styled text; the
// grammar's bold/italic/underline attribute rides the content.
func TestBrowseMarkupMarkersHidden(t *testing.T) {
	e, w, out := renderedEditorWithConfig(t,
		"a **bold** b //it// c __un__ d\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	plain := stripSGR(out.String())
	for _, marker := range []string{"**", "//", "__"} {
		if strings.Contains(plain, marker) {
			t.Fatalf("browse mode should hide %q markers; got %q", marker, plain)
		}
	}
	for _, word := range []string{"bold", "it", "un"} {
		if !strings.Contains(plain, word) {
			t.Fatalf("styled word %q should remain; got %q", word, plain)
		}
	}
}

// Browse mode hides heading "=" and restyles by level: the equals go away, the
// heading color paints, and the per-level bold/underline attributes apply.
func TestBrowseHeadingLevels(t *testing.T) {
	// L1 ======, L3 ====, L5 == : bold on 1&3, underline on 1&3 (not 5).
	e, w, out := renderedEditorWithConfig(t,
		"====== Big ======\n==== Mid ====\n== Small ==\n", "[options]\nsyntax=dokuwiki\n")
	w.SetCursorPos(window.Position{Line: 2, Rune: 0}) // keep caret off the styled lines
	w.BrowseActive = true
	out.Reset()
	e.performRender()
	full := out.String()
	plain := stripSGR(full)
	if strings.Contains(plain, "=") {
		t.Fatalf("heading '=' markers should be hidden; got %q", plain)
	}
	for _, word := range []string{"Big", "Mid", "Small"} {
		if !strings.Contains(plain, word) {
			t.Fatalf("heading text %q should remain; got %q", word, plain)
		}
	}
	// The heading base color (bright cyan) paints, and bold+underline appear
	// somewhere (L1/L3).
	if !strings.Contains(full, "\x1b[0;96;40m") {
		t.Fatal("heading base color should paint")
	}
	if !strings.Contains(full, "\x1b[1m") || !strings.Contains(full, "\x1b[4m") {
		t.Fatal("bold and underline attributes should appear on higher levels")
	}
}
