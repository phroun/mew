package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window's chrome is drawn and hit-tested in its CONTAINER's
// denomination (its bounds live in that space), so a font_size-scaled
// desktop must scale the titlebar's controls too. Here the container
// carries the denomination and the window inherits it - the real
// arrangement; a fixed 8x16 hit-test would leave the scaled buttons
// unclickable.
func TestTitlebarButtonHitboxScalesWithDenomination(t *testing.T) {
	for _, m := range []core.CellMetrics{{CellWidth: 8, CellHeight: 16}, {CellWidth: 12, CellHeight: 24}} {
		container := NewWindow("container")
		container.SetCellMetrics(&m)
		w := NewWindow("W")
		w.SetParent(container)
		w.SetBounds(core.UnitRect{Width: 60 * m.CellWidth, Height: 10 * m.CellHeight})

		// Normal frame: controls begin one cell in (after the left border);
		// the close button [x] spans the next three cells. Probe its far
		// edge and near the bottom of the titlebar row - both points a
		// fixed 8x16 hit-test would miss once the frame is 12x24.
		x := m.CellWidth*4 - 1 // right edge of the [x] button
		y := m.CellHeight - 2  // inside the scaled titlebar row
		if got := w.buttonAtPosition(x, y); got != TitleButtonClose {
			t.Errorf("metrics %+v: buttonAtPosition(%d,%d) = %v, want Close", m, x, y, got)
		}
		// Just below the (scaled) titlebar is no longer a button.
		if got := w.buttonAtPosition(x, m.CellHeight+1); got != TitleButtonNone {
			t.Errorf("metrics %+v: below titlebar = %v, want None", m, got)
		}
	}
}

// A maximized window reserves exactly one titlebar row (in the
// container's denomination) for its content, so there is no gap or
// overlap between the titlebar and the content area.
func TestMaximizedContentStartsBelowTitlebar(t *testing.T) {
	for _, m := range []core.CellMetrics{{CellWidth: 8, CellHeight: 16}, {CellWidth: 12, CellHeight: 24}} {
		container := NewWindow("container")
		container.SetCellMetrics(&m)
		w := NewWindow("W")
		w.SetParent(container)
		w.SetBounds(core.UnitRect{Width: 400, Height: 300})
		w.Maximize()
		if cb := w.contentBounds(); cb.Y != m.CellHeight {
			t.Errorf("metrics %+v: maximized content Y = %d, want %d (one titlebar row)", m, cb.Y, m.CellHeight)
		}
	}
}

// The window's own denomination override must NOT change its chrome: the
// frame lives in the container's currency, so the titlebar height stays
// one CONTAINER row even when the window re-denominates its interior
// (the layout-invariance contract).
func TestChromeIgnoresWindowOwnDenomination(t *testing.T) {
	container := NewWindow("container")
	cm := core.CellMetrics{CellWidth: 8, CellHeight: 16}
	container.SetCellMetrics(&cm)

	w := NewWindow("W")
	w.SetParent(container)
	w.SetBounds(core.UnitRect{Width: 400, Height: 300})
	w.Maximize()
	before := w.contentBounds().Y

	own := core.CellMetrics{CellWidth: 8, CellHeight: 32}
	w.SetCellMetrics(&own) // re-denominate the window's interior only
	if after := w.contentBounds().Y; after != before {
		t.Errorf("chrome followed the window's own denomination: %d -> %d (want stable %d)", before, after, before)
	}
	if before != cm.CellHeight {
		t.Errorf("titlebar height = %d, want the container's %d", before, cm.CellHeight)
	}
}
