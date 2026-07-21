package plugins

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

var rulerAnsiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func renderRulerPlain(rtl bool, width int) string {
	c := NewColumnRuler()
	c.SetRTL(rtl)
	w := &window.Window{Type: window.DocWindow}
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
