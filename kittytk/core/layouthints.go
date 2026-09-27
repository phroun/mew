package core

// Layout hints travel with the child and are read by the parent's layout
// manager when the child is attached, which is why they are properties on a
// trinket rather than arguments to an add call: a build script says everything
// about a child on the child's own statement.
//
// Alignment (see align.go) is the hint every manager reads. The two below are
// read by one manager each, and mean nothing to the others.

// GridPlacement is which cells of a grid a child occupies.
//
// A span of zero is one cell.
//
// RowID and ColumnID name a band instead of counting to one. A child that
// names a band the grid has is put in that band whatever Row or Column says,
// which is what lets a track be inserted without renumbering the form.
//
// How a track divides the space is the track's own business and is written on
// its band (see layout.Band), not here: a child sits in a column and has no
// standing to say how wide that column should be.
type GridPlacement struct {
	Row, Column         int
	RowID, ColumnID     string
	RowSpan, ColumnSpan int
}

// FlexHints are what a flex layout reads off a child: its share of the leftover
// space along the main axis, its share of the shortfall when there is not
// enough, and the size to start from.
//
// ShrinkSet distinguishes a shrink of zero -- "never take anything off me" --
// from one nobody wrote, which is the default of one. Basis makes the same
// distinction with a value rather than a flag: BasisAuto is one nobody wrote,
// and a basis of zero is a real answer -- size me by my grow factor alone,
// paying no attention to what I hold.
//
// Which is why a FlexHints is started from DefaultFlexHints and not from its
// zero value: zero means something here.
type FlexHints struct {
	Grow      float64
	Shrink    float64
	ShrinkSet bool
	Basis     Unit
}

// DefaultFlexHints is what a flex layout gives a child that says nothing: no
// growing, ordinary shrinking, and a size taken from the child itself.
func DefaultFlexHints() FlexHints {
	return FlexHints{Shrink: 1, Basis: BasisAuto}
}

// LayoutGridPlacement returns the child's grid placement and whether one was
// set (a grid keeps its own default otherwise).
func (w *TrinketBase) LayoutGridPlacement() (GridPlacement, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.gridPlacement, w.gridPlacementSet
}

// SetLayoutGridPlacement sets the child's grid placement.
func (w *TrinketBase) SetLayoutGridPlacement(p GridPlacement) {
	w.mu.Lock()
	w.gridPlacement = p
	w.gridPlacementSet = true
	w.mu.Unlock()

	w.notifyAncestorsOfRepaint()
}

// LayoutFlex returns the child's flex hints and whether any were set.
func (w *TrinketBase) LayoutFlex() (FlexHints, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.flexHints, w.flexHintsSet
}

// SetLayoutFlex sets the child's flex hints.
func (w *TrinketBase) SetLayoutFlex(h FlexHints) {
	w.mu.Lock()
	w.flexHints = h
	w.flexHintsSet = true
	w.mu.Unlock()

	w.notifyAncestorsOfRepaint()
}
