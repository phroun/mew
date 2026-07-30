package plugins

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

var rulerAnsiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func renderRulerPlain(rtl bool, width int) string {
	c := NewColumnRuler()
	c.SetRTL(rtl)
	w := &viewport.Viewport{Type: viewport.DocViewport}
	return rulerAnsiRe.ReplaceAllString(c.RenderContent(w, width, nil), "")
}

// LTR ruler counts from the left; the RTL ruler is its mirror, counting from
// the RIGHT edge (an RTL line's reading start), with multi-digit numbers
// still reading normally at their mirrored positions.
func TestRulerInvertsForRTL(t *testing.T) {
	ltr := renderRulerPlain(false, 40)
	rtl := renderRulerPlain(true, 40)

	if len([]rune(ltr)) != 40 || len([]rune(rtl)) != 40 {
		t.Fatalf("ruler widths: ltr=%d rtl=%d, want 40", len([]rune(ltr)), len([]rune(rtl)))
	}
	// Column 1's extent number: leftmost cell on the LTR ruler, rightmost on
	// the RTL ruler.
	if ltr[0] != '1' {
		t.Fatalf("ltr ruler should start with column 1, got %q", ltr[:5])
	}
	rr := []rune(rtl)
	if rr[len(rr)-1] != '1' {
		t.Fatalf("rtl ruler should END with column 1, got %q", string(rr[len(rr)-5:]))
	}
	// Decade numbers stay digit-readable after mirroring: "10" never "01".
	if !strings.Contains(rtl, "10") || strings.Contains(rtl, "01") {
		t.Fatalf("rtl ruler numbers must read normally: %q", rtl)
	}
	// And the mirrored ruler is exactly the cell-reverse of the LTR one once
	// digit runs are ignored: spot-check the major tick pattern mirrors.
	lr := []rune(ltr)
	for i := 0; i < 40; i++ {
		l, r := lr[i], rr[39-i]
		lDigit := l >= '0' && l <= '9'
		rDigit := r >= '0' && r <= '9'
		if lDigit != rDigit {
			t.Fatalf("cell %d: digit placement should mirror (ltr %q vs rtl %q)", i, l, r)
		}
		if !lDigit && l != r {
			t.Fatalf("cell %d: tick glyphs should mirror (ltr %q vs rtl %q)", i, l, r)
		}
	}
}

// renderRulerForViewport renders with an explicit editor default AND a
// viewport whose own direction option may override it.
func renderRulerForViewport(editorRTL bool, viewportDir string, width int) string {
	c := NewColumnRuler()
	c.SetRTL(editorRTL)
	w := &viewport.Viewport{Type: viewport.DocViewport}
	w.ViewState.Direction = viewportDir
	return rulerAnsiRe.ReplaceAllString(c.RenderContent(w, width, nil), "")
}

// A viewport whose OWN direction option is rtl gets an inverted ruler even
// though the editor default never changed — which is what set_option does
// whenever a viewport is focused. Reading only the editor-wide flag left that
// ruler numbered left-to-right, with column 1 at the wrong end.
func TestRulerInvertsForViewportDirection(t *testing.T) {
	perViewport := renderRulerForViewport(false, "rtl", 40)
	editorWide := renderRulerPlain(true, 40)

	if perViewport != editorWide {
		t.Fatalf("per-viewport rtl ruler differs from the editor-wide one:\n got %q\nwant %q",
			perViewport, editorWide)
	}
	rr := []rune(perViewport)
	if rr[len(rr)-1] != '1' {
		t.Fatalf("column 1 should sit at the RIGHT edge, got %q", string(rr[len(rr)-5:]))
	}
}

// The override runs both ways: an ltr viewport inside an rtl editor keeps the
// left-to-right ruler.
func TestRulerViewportLTROverridesEditorRTL(t *testing.T) {
	got := renderRulerForViewport(true, "ltr", 40)
	want := renderRulerPlain(false, 40)
	if got != want {
		t.Fatalf("ltr viewport in an rtl editor:\n got %q\nwant %q", got, want)
	}
	if got[0] != '1' {
		t.Fatalf("column 1 should sit at the LEFT edge, got %q", got[:5])
	}
}

// A viewport with no direction of its own inherits the editor default.
func TestRulerViewportInheritsEditorDirection(t *testing.T) {
	if got, want := renderRulerForViewport(true, "", 40), renderRulerPlain(true, 40); got != want {
		t.Fatalf("unset viewport direction should inherit rtl:\n got %q\nwant %q", got, want)
	}
	if got, want := renderRulerForViewport(false, "", 40), renderRulerPlain(false, 40); got != want {
		t.Fatalf("unset viewport direction should inherit ltr:\n got %q\nwant %q", got, want)
	}
}
