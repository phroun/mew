package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// propMeasurer is a stand-in graphical text measurer: a distinctive
// 5 units per rune (unlike the 8-unit monospace cell), so a test can
// tell whether a width came from the font/measurer or from cell math.
type propMeasurer struct{}

func (propMeasurer) MeasureText(_ *core.Font, s string) core.Unit {
	return core.Unit(len([]rune(s))) * 5
}
func (propMeasurer) LineHeight(_ *core.Font) core.Unit { return 16 }

// The overflow ellipsis is measured through the menu bar's font (the
// proportional path), not the monospace cell width.
func TestMenuBarEllipsisMeasuredProportionally(t *testing.T) {
	core.SetTextMeasurer(propMeasurer{})
	defer core.SetTextMeasurer(nil)

	mb := NewMenuBar()
	// "..." through the measurer = 3*5 = 15 units; the old cell width
	// would have been 3*8 = 24.
	if got := mb.ellipsisWidth(); got != 15 {
		t.Errorf("ellipsisWidth = %d, want 15 (proportional), not 24 (cell)", got)
	}
}

// On graphical surfaces the clock renders in a monospace face at ~80% of
// the standard size; on text surfaces it keeps cell rendering (nil font).
func TestMenuBarDateTimeFontIsCompactMono(t *testing.T) {
	mb := NewMenuBar()

	mb.graphicalCached = false
	if mb.dateTimeFont() != nil {
		t.Error("text surface should render the clock in cells (nil font)")
	}

	mb.graphicalCached = true
	f := mb.dateTimeFont()
	if f == nil {
		t.Fatal("graphical surface should render the clock in a scaled font")
	}
	if f.Name != core.FontMonday12.Name {
		t.Errorf("clock font family = %q, want monospace %q", f.Name, core.FontMonday12.Name)
	}
	want := (core.FontMonday12.Size*8 + 5) / 10
	if f.Size != want {
		t.Errorf("clock font size = %d, want %d (~80%% of %d)", f.Size, want, core.FontMonday12.Size)
	}
	if f.Size >= core.FontMonday12.Size {
		t.Errorf("clock font size %d not smaller than base %d", f.Size, core.FontMonday12.Size)
	}
}

// With a graphical measurer installed, the reserved clock width comes
// from the compact font, so it is narrower than the monospace cell
// reservation for the same 18-character string.
func TestMenuBarDateTimeWidthNarrowerOnGraphical(t *testing.T) {
	mb := NewMenuBar()

	mb.graphicalCached = false
	textWidth := mb.dateTimeWidth()

	core.SetTextMeasurer(propMeasurer{})
	defer core.SetTextMeasurer(nil)
	mb.graphicalCached = true
	graphicalWidth := mb.dateTimeWidth()

	if graphicalWidth >= textWidth {
		t.Errorf("graphical clock width %d not narrower than text width %d", graphicalWidth, textWidth)
	}
}
