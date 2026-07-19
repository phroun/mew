package window

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// The title ellipsis is three periods, not the "…" glyph, and the
// need-for-ellipsis math reserves their full width.
func TestTitleEllipsisUsesThreePeriods(t *testing.T) {
	font := core.DefaultFont() // cell metrics: 1 cell per rune
	cell := font.MeasureText("x")

	// A wide-enough avail returns the string unchanged.
	if got := ellipsizeToWidth("Report", 100*cell, font); got != "Report" {
		t.Errorf("fitting title changed: %q", got)
	}

	// Too narrow: the result ends in "..." (not "…") and fits.
	got := ellipsizeToWidth("Report Viewer", 8*cell, font)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("ellipsis = %q, want a '...' suffix", got)
	}
	if strings.ContainsRune(got, '…') {
		t.Errorf("result still contains the unicode ellipsis: %q", got)
	}
	if font.MeasureText(got) > 8*cell {
		t.Errorf("ellipsized %q exceeds the available width", got)
	}
	// Three periods reserve 3 cells, so 8 cells leaves 5 for text.
	if n := len([]rune(got)); n != 8 {
		t.Errorf("ellipsized to %d runes (%q), want 5 text + 3 dots", n, got)
	}
}
