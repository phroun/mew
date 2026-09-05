package layout

import "github.com/phroun/kittytk/core"

// An INLINE trinket is one that reads as part of a sentence rather than as a
// block of its own: a button, a label, a checkbox, a field. A panel is not.
//
// An inline trinket carries SIDE-BEARINGS -- a column of air on its left and on
// its right -- so it never butts against a frame, a block, or another run. The
// bearing belongs to the TRINKET, not to whatever contains it, which is why
// every layout manager applies it and none of them has to know what it is
// nested in: a row of buttons inside a column lines up with the labels above it
// because both got their bearing from themselves.
//
// Bearings are horizontal, as the name says. Where two meet they collapse into
// one column, the way margins do -- a box does that along its run, and a grid
// has nothing to collapse because its cells are already apart.

// isInlineTrinket reports whether a trinket reads as inline.
func isInlineTrinket(w core.Trinket) bool {
	// A trinket that answers for itself is taken at its word.
	if inline, ok := w.(core.InlineTrinket); ok && inline.IsInlineTrinket() {
		return true
	}
	// A container is a block.
	if _, ok := w.(core.Container); ok {
		return false
	}
	// Everything else reads as inline.
	return true
}

// sideBearing is the column of air an inline trinket keeps on each side, and
// nothing for a block.
func sideBearing(w core.Trinket, metrics core.CellMetrics) core.Unit {
	if isInlineTrinket(w) {
		return metrics.UnitsPerCellWidth
	}
	return 0
}

// insetForBearing takes an inline trinket's bearings out of the space it was
// given: the trinket sits a column in on each side, and a block is untouched.
func insetForBearing(w core.Trinket, metrics core.CellMetrics, r core.UnitRect) core.UnitRect {
	b := sideBearing(w, metrics)
	if b == 0 {
		return r
	}
	r.X += b
	if r.Width -= 2 * b; r.Width < 0 {
		r.Width = 0
	}
	return r
}
