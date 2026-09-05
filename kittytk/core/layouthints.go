package core

// Layout hints travel with the child and are read by the parent's layout
// manager when the child is attached, which is why they are properties on a
// trinket rather than arguments to an add call: a build script says everything
// about a child on the child's own statement.
//
// Alignment (see align.go) is the hint every manager reads. The two below are
// read by one manager each, and mean nothing to the others.

// GridPlacement is where a grid puts a child, and which of the row and column
// it occupies should take leftover space.
//
// A span of zero is one cell. The stretches are weights, and where two children
// in the same row or column ask for different ones the largest is what that row
// or column gets -- a row is one thing, and cannot take two answers.
type GridPlacement struct {
	Row, Column         int
	RowSpan, ColumnSpan int
	RowStretch          int
	ColumnStretch       int
}

// FlexHints are what a flex layout reads off a child: its share of the leftover
// space along the main axis, its share of the shortfall when there is not
// enough, and the size to start from.
//
// ShrinkSet distinguishes a shrink of zero -- "never take anything off me" --
// from one nobody wrote, which is the default of one.
type FlexHints struct {
	Grow      float64
	Shrink    float64
	ShrinkSet bool
	Basis     Unit
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
