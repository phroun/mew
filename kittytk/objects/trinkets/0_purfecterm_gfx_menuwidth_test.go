package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// A context menu is as wide as its widest label, the way a menu-bar dropdown
// is. A fixed width clips what outgrows it and leaves a gutter beside what
// does not -- and, being a raw unit count, it means a different number of
// columns at every denomination.
func TestContextMenuWidthFollowsItsLabels(t *testing.T) {
	m := core.DefaultCellMetrics()
	font := core.DefaultFont()
	const indent = core.Unit(8)

	short := []termMenuItem{{label: "Cut"}, {label: "Copy"}}
	long := []termMenuItem{{label: "Cut"}, {label: strings.Repeat("Paste and match style ", 2)}}

	shortW := termMenuWidth(font, m, indent, short)
	longW := termMenuWidth(font, m, indent, long)
	if longW <= shortW {
		t.Errorf("a menu of long labels is %d wide and one of short labels %d; it does not follow them",
			longW, shortW)
	}

	// Wide enough for the label it will draw, with the indent kept on both
	// sides -- so nothing it paints runs past its own edge.
	widest := font.MeasureTextIn(long[1].label, m)
	if longW < widest+indent*2 {
		t.Errorf("menu %d wide holds a %d-unit label indented by %d; it would clip",
			longW, widest, indent)
	}

	// A checkable item reserves the tick's room whether or not it is ticked,
	// so ticking one does not widen the menu or shift its text.
	off, on := false, true
	offW := termMenuWidth(font, m, indent, []termMenuItem{{label: "Mouse Reporting", checked: func() bool { return off }}})
	onW := termMenuWidth(font, m, indent, []termMenuItem{{label: "Mouse Reporting", checked: func() bool { return on }}})
	if offW != onW {
		t.Errorf("ticking an item changes the menu width: %d unticked, %d ticked", offW, onW)
	}

	// A menu of one short word is still menu-shaped.
	if tiny := termMenuWidth(font, m, indent, []termMenuItem{{label: "Cut"}}); tiny < m.UnitsPerCellWidth*12 {
		t.Errorf("a one-word menu is %d wide, under the %d floor", tiny, m.UnitsPerCellWidth*12)
	}

	// A separator has no label to measure and must not be read as one.
	if got := termMenuWidth(font, m, indent, []termMenuItem{{separator: true}}); got != m.UnitsPerCellWidth*12 {
		t.Errorf("a menu of one separator is %d wide, want the floor %d", got, m.UnitsPerCellWidth*12)
	}
}

// The width follows the DENOMINATION too: the same labels are the same number
// of columns whatever the units are counted in.
func TestContextMenuWidthFollowsTheDenomination(t *testing.T) {
	font := core.DefaultFont()
	items := []termMenuItem{{label: "Paste and match style"}, {label: "Cut"}}

	base := core.Unit(0)
	for _, m := range []core.CellMetrics{
		{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
		{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
		{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
	} {
		// The indent is a screen quantity, so it is counted in these units too.
		indent := m.UnitsPerCellWidth
		cells := termMenuWidth(font, m, indent, items) / m.UnitsPerCellWidth
		if base == 0 {
			base = cells
			continue
		}
		if d := cells - base; d < -1 || d > 1 {
			t.Errorf("at %dx%d the menu is %d cells wide, want about %d",
				m.UnitsPerCellWidth, m.UnitsPerCellHeight, cells, base)
		}
	}
}
