// Package core provides fundamental types for KittyTK.
package core

// Unit represents an abstract coordinate unit.
// In text mode, units are translated to character cells via CellMetrics.
// In graphics mode, units could map directly to pixels or be scaled.
type Unit int

// A size nobody gave a value carries -1, never zero.
//
// Zero is a real size: a maximum of zero collapses a trinket to nothing while
// it keeps its place in the layout, which is a thing an author may want to
// say. So the ABSENCE of a size needs a spelling of its own, and -1 is it
// throughout the toolkit -- for a maximum that does not bound, and for a flex
// basis taken from the child rather than stated.
//
// Indices and counts keep their own -1 for "none", which is the same idea
// arrived at from the other side: there, zero is the first one.
const (
	// Unbounded is a maximum that does not bound.
	Unbounded Unit = -1
	// BasisAuto is a flex basis taken from the child rather than stated.
	BasisAuto Unit = -1
)

// UnitPoint represents a 2D coordinate in abstract units.
type UnitPoint struct {
	X, Y Unit
}

// UnitSize represents dimensions in abstract units.
type UnitSize struct {
	Width, Height Unit
}

// UnitRect represents a rectangle in abstract units.
type UnitRect struct {
	X, Y          Unit
	Width, Height Unit
}

// NewUnitRect creates a new unit rectangle.
func NewUnitRect(x, y, width, height Unit) UnitRect {
	return UnitRect{X: x, Y: y, Width: width, Height: height}
}

// Contains checks if a point is inside the rectangle.
func (r UnitRect) Contains(p UnitPoint) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

// Intersects checks if two rectangles overlap.
func (r UnitRect) Intersects(other UnitRect) bool {
	return r.X < other.X+other.Width && r.X+r.Width > other.X &&
		r.Y < other.Y+other.Height && r.Y+r.Height > other.Y
}

// Intersection returns the overlapping area of two rectangles.
func (r UnitRect) Intersection(other UnitRect) UnitRect {
	x1 := max(r.X, other.X)
	y1 := max(r.Y, other.Y)
	x2 := min(r.X+r.Width, other.X+other.Width)
	y2 := min(r.Y+r.Height, other.Y+other.Height)
	if x2 <= x1 || y2 <= y1 {
		return UnitRect{}
	}
	return UnitRect{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1}
}

// IsEmpty returns true if the rectangle has no area.
func (r UnitRect) IsEmpty() bool {
	return r.Width <= 0 || r.Height <= 0
}

// TopLeft returns the top-left corner.
func (r UnitRect) TopLeft() UnitPoint {
	return UnitPoint{X: r.X, Y: r.Y}
}

// BottomRight returns the bottom-right corner (exclusive).
func (r UnitRect) BottomRight() UnitPoint {
	return UnitPoint{X: r.X + r.Width, Y: r.Y + r.Height}
}

// Size returns the dimensions of the rectangle.
func (r UnitRect) Size() UnitSize {
	return UnitSize{Width: r.Width, Height: r.Height}
}

// Translated returns a copy offset by the given delta.
func (r UnitRect) Translated(dx, dy Unit) UnitRect {
	return UnitRect{X: r.X + dx, Y: r.Y + dy, Width: r.Width, Height: r.Height}
}

// UnitMargins represents spacing in abstract units.
type UnitMargins struct {
	Top, Right, Bottom, Left Unit
}

// NewUnitMargins creates uniform margins.
func NewUnitMargins(all Unit) UnitMargins {
	return UnitMargins{Top: all, Right: all, Bottom: all, Left: all}
}

// NewUnitMarginsVH creates margins with vertical and horizontal values.
func NewUnitMarginsVH(vertical, horizontal Unit) UnitMargins {
	return UnitMargins{Top: vertical, Right: horizontal, Bottom: vertical, Left: horizontal}
}

// Horizontal returns the total horizontal margin.
func (m UnitMargins) Horizontal() Unit {
	return m.Left + m.Right
}

// Vertical returns the total vertical margin.
func (m UnitMargins) Vertical() Unit {
	return m.Top + m.Bottom
}

// StyleInsetProvider is the optional capability a trinket implements when part
// of the size it asks for is DECORATION rather than content: a button's drop
// shadow, and whatever else grows one later -- a glow, an outer stroke.
//
// The insets are what the trinket RESERVES, which is not what it paints. A
// button reserves a whole cell to the right and a whole row below on both
// surfaces, while the pixel path draws a softer shadow half a cell out. The
// reservation is the number layout wants: it is the same on both surfaces, so a
// row of trinkets lands identically whichever one is drawing, and it is a whole
// row so the things beside it stay on the grid.
//
// In UNITS, not in cells, so a subtler decoration than a whole cell can be
// stated when one arrives.
type StyleInsetProvider interface {
	StyleInsets() UnitMargins
}

// FindStyleInsets is what a trinket reserves for decoration, or nothing.
func FindStyleInsets(w Trinket) UnitMargins {
	if p, ok := w.(StyleInsetProvider); ok {
		return p.StyleInsets()
	}
	return UnitMargins{}
}

// CellMetrics is a denomination: how many units subdivide one character cell.
// A cell is a fixed physical size at a given zoom; the denomination says how
// finely a layout may address inside it, and is set per subtree by the
// column_units and row_units properties. 8x16 is only what a subtree gets when
// nothing above it says otherwise.
type CellMetrics struct {
	// UnitsPerCellWidth is how many units span one character cell across --
	// one grid column. The column_units property sets it.
	UnitsPerCellWidth Unit

	// UnitsPerCellHeight is how many units span one character cell down --
	// one grid row. The row_units property sets it.
	UnitsPerCellHeight Unit
}

// DefaultCellMetrics returns standard 8x16 cell metrics (typical terminal font proportions).
func DefaultCellMetrics() CellMetrics {
	return CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
}

// SquareCellMetrics returns 1:1 cell metrics (each unit = one character cell).
// Use this for simple text-mode layouts where you don't need sub-cell precision.
func SquareCellMetrics() CellMetrics {
	return CellMetrics{UnitsPerCellWidth: 1, UnitsPerCellHeight: 1}
}

// UnitsToCell converts a unit coordinate to a cell coordinate.
// The result is the cell that contains the unit coordinate.
func (m CellMetrics) UnitsToCell(units Unit, cellSize Unit) int {
	if cellSize <= 0 {
		return 0
	}
	return int(units / cellSize)
}

// UnitsToCellX converts a unit X coordinate to a cell column.
func (m CellMetrics) UnitsToCellX(x Unit) int {
	return m.UnitsToCell(x, m.UnitsPerCellWidth)
}

// UnitsToCellY converts a unit Y coordinate to a cell row.
func (m CellMetrics) UnitsToCellY(y Unit) int {
	return m.UnitsToCell(y, m.UnitsPerCellHeight)
}

// CellToUnitsX converts a cell column to unit X coordinate.
func (m CellMetrics) CellToUnitsX(col int) Unit {
	return Unit(col) * m.UnitsPerCellWidth
}

// CellToUnitsY converts a cell row to unit Y coordinate.
func (m CellMetrics) CellToUnitsY(row int) Unit {
	return Unit(row) * m.UnitsPerCellHeight
}

// UnitsToSize converts a unit size to cell dimensions (rounding up).
func (m CellMetrics) UnitsToSize(size UnitSize) (cols, rows int) {
	cols = int((size.Width + m.UnitsPerCellWidth - 1) / m.UnitsPerCellWidth)
	rows = int((size.Height + m.UnitsPerCellHeight - 1) / m.UnitsPerCellHeight)
	return
}

// CellsToUnits converts cell dimensions to unit size.
func (m CellMetrics) CellsToUnits(cols, rows int) UnitSize {
	return UnitSize{
		Width:  Unit(cols) * m.UnitsPerCellWidth,
		Height: Unit(rows) * m.UnitsPerCellHeight,
	}
}

// TextWidth returns the width in units needed to display text with given character count.
func (m CellMetrics) TextWidth(charCount int) Unit {
	return Unit(charCount) * m.UnitsPerCellWidth
}

// TextHeight returns the height in units for a given number of lines.
func (m CellMetrics) TextHeight(lineCount int) Unit {
	return Unit(lineCount) * m.UnitsPerCellHeight
}

// CharsForWidth returns how many characters fit in the given width.
func (m CellMetrics) CharsForWidth(width Unit) int {
	if m.UnitsPerCellWidth <= 0 {
		return 0
	}
	return int(width / m.UnitsPerCellWidth)
}

// LinesForHeight returns how many lines fit in the given height.
func (m CellMetrics) LinesForHeight(height Unit) int {
	if m.UnitsPerCellHeight <= 0 {
		return 0
	}
	return int(height / m.UnitsPerCellHeight)
}

// RoundDownToCell rounds a unit value down to the nearest cell boundary.
func (m CellMetrics) RoundDownToCell(units Unit, cellSize Unit) Unit {
	if cellSize <= 0 {
		return units
	}
	return (units / cellSize) * cellSize
}

// RoundDownToCellX rounds an X coordinate down to the nearest cell boundary.
func (m CellMetrics) RoundDownToCellX(x Unit) Unit {
	return m.RoundDownToCell(x, m.UnitsPerCellWidth)
}

// RoundDownToCellY rounds a Y coordinate down to the nearest cell boundary.
func (m CellMetrics) RoundDownToCellY(y Unit) Unit {
	return m.RoundDownToCell(y, m.UnitsPerCellHeight)
}

// RoundUpToCell rounds a unit value up to the nearest cell boundary. It is
// what an EXTENT does: a box a fraction of a cell wide still needs the whole
// cell to draw in, and rounding down would clip its far edge.
func (m CellMetrics) RoundUpToCell(units Unit, cellSize Unit) Unit {
	if cellSize <= 0 {
		return units
	}
	if r := units % cellSize; r != 0 {
		if units < 0 {
			return units - r
		}
		return units + (cellSize - r)
	}
	return units
}

// RoundUpToCellX rounds a width up to the nearest cell boundary.
func (m CellMetrics) RoundUpToCellX(w Unit) Unit {
	return m.RoundUpToCell(w, m.UnitsPerCellWidth)
}

// RoundUpToCellY rounds a height up to the nearest cell boundary.
func (m CellMetrics) RoundUpToCellY(h Unit) Unit {
	return m.RoundUpToCell(h, m.UnitsPerCellHeight)
}

// GridRect puts a rectangle where a cell surface can render it: the ORIGIN
// floors onto the grid and the EXTENT ceils onto it.
//
// The two rules differ because the two quantities do. A position never ceils
// -- rounding it up moves the thing away from where it was asked to be, past
// whatever it was aligned against. An extent never floors -- rounding it down
// clips the far edge of something that was asked to be that big.
func (m CellMetrics) GridRect(r UnitRect) UnitRect {
	return UnitRect{
		X:      m.RoundDownToCellX(r.X),
		Y:      m.RoundDownToCellY(r.Y),
		Width:  m.RoundUpToCellX(r.Width),
		Height: m.RoundUpToCellY(r.Height),
	}
}

// AlignSize aligns width and height to cell boundaries (rounding down).
func (m CellMetrics) AlignSize(size UnitSize) UnitSize {
	return UnitSize{
		Width:  m.RoundDownToCellX(size.Width),
		Height: m.RoundDownToCellY(size.Height),
	}
}

// AlignRect aligns a rectangle's position and size to cell boundaries.
func (m CellMetrics) AlignRect(r UnitRect) UnitRect {
	return UnitRect{
		X:      m.RoundDownToCellX(r.X),
		Y:      m.RoundDownToCellY(r.Y),
		Width:  m.RoundDownToCellX(r.Width),
		Height: m.RoundDownToCellY(r.Height),
	}
}

// DragTravel is how far a drag has come, at the granularity the surface can
// place things at: whole cells where it has a grid, exact units where it does
// not.
//
// Under the kitty protocol the pointer reports where it is INSIDE a cell, so a
// travel measured in units carries a fraction of a cell that a cell surface
// can never place. Rounded away when the window lands, that fraction is the
// gap between where the pointer is and where the edge it is dragging got to.
func DragTravel(from, to UnitPoint, m CellMetrics, snap bool) (Unit, Unit) {
	if !snap {
		return to.X - from.X, to.Y - from.Y
	}
	return m.RoundDownToCellX(to.X) - m.RoundDownToCellX(from.X),
		m.RoundDownToCellY(to.Y) - m.RoundDownToCellY(from.Y)
}

// DragOrigin is where a dragged window's top-left goes for a pointer at `at`,
// grabbed `offset` in from that corner.
//
// On a cell surface the pointer and the offset are both taken to the cell they
// are in, so the cell under the pointer is the same cell of the window for the
// whole gesture. Carrying the sub-cell part of either is what let the pointer
// run ahead of the window it was dragging.
func DragOrigin(at, offset UnitPoint, m CellMetrics, snap bool) UnitPoint {
	if !snap {
		return UnitPoint{X: at.X - offset.X, Y: at.Y - offset.Y}
	}
	return UnitPoint{
		X: m.RoundDownToCellX(at.X) - m.RoundDownToCellX(offset.X),
		Y: m.RoundDownToCellY(at.Y) - m.RoundDownToCellY(offset.Y),
	}
}

// CellMetricsProvider is implemented by trinkets that can provide a
// grid-metrics override. Grid metrics are a per-container layout
// vocabulary: each container may define how many units a virtual
// row/column occupies, inherited through the container chain like
// fonts (see FontProvider), rooted at the display service's default.
type CellMetricsProvider interface {
	// CellMetricsOverride returns the metrics set on this provider,
	// or nil to inherit from the parent chain.
	CellMetricsOverride() *CellMetrics
}

// FindEffectiveCellMetrics walks up the trinket tree to find the
// effective grid metrics, mirroring FindEffectiveFont. It checks the
// trinket, then its ancestors (window, MDI pane, desktop). Returns
// DefaultCellMetrics() if no override is set anywhere in the chain.
func FindEffectiveCellMetrics(w Trinket) CellMetrics {
	if w == nil {
		return DefaultCellMetrics()
	}

	if mp, ok := w.(CellMetricsProvider); ok {
		if m := mp.CellMetricsOverride(); m != nil {
			return *m
		}
	}

	current := w.Parent()
	for current != nil {
		if mp, ok := current.(CellMetricsProvider); ok {
			if m := mp.CellMetricsOverride(); m != nil {
				return *m
			}
		}
		if trinket, ok := current.(Trinket); ok {
			current = trinket.Parent()
		} else {
			break
		}
	}

	return DefaultCellMetrics()
}

// ExchangeX converts an X-axis value denominated in `from` metrics into
// `to` metrics: the same number of columns, re-expressed. Identity when
// the denominations match.
func ExchangeX(v Unit, from, to CellMetrics) Unit {
	if from.UnitsPerCellWidth == to.UnitsPerCellWidth || from.UnitsPerCellWidth <= 0 || to.UnitsPerCellWidth <= 0 {
		return v
	}
	return Unit(float64(v) * float64(to.UnitsPerCellWidth) / float64(from.UnitsPerCellWidth))
}

// ExchangeY converts a Y-axis value denominated in `from` metrics into
// `to` metrics: the same number of rows, re-expressed.
func ExchangeY(v Unit, from, to CellMetrics) Unit {
	if from.UnitsPerCellHeight == to.UnitsPerCellHeight || from.UnitsPerCellHeight <= 0 || to.UnitsPerCellHeight <= 0 {
		return v
	}
	return Unit(float64(v) * float64(to.UnitsPerCellHeight) / float64(from.UnitsPerCellHeight))
}

// ExchangeSize converts a size between denominations.
func ExchangeSize(s UnitSize, from, to CellMetrics) UnitSize {
	return UnitSize{
		Width:  ExchangeX(s.Width, from, to),
		Height: ExchangeY(s.Height, from, to),
	}
}

// ParentCellMetrics returns the effective metrics of w's parent context
// — the denomination in which w's bounds are expressed. A trinket with a
// metrics override denominates its interior; its own bounds live in the
// parent's currency.
func ParentCellMetrics(w Trinket) CellMetrics {
	if w == nil {
		return DefaultCellMetrics()
	}
	if pw, ok := w.Parent().(Trinket); ok && pw != nil {
		return FindEffectiveCellMetrics(pw)
	}
	return DefaultCellMetrics()
}

// Transform handles coordinate transformation between different coordinate spaces.
type Transform struct {
	// Offset added to coordinates
	OffsetX, OffsetY Unit

	// Scale factors (1.0 = no scaling)
	ScaleX, ScaleY float64
}

// IdentityTransform returns a transform that doesn't modify coordinates.
func IdentityTransform() Transform {
	return Transform{ScaleX: 1.0, ScaleY: 1.0}
}

// NewTranslation creates a transform that offsets coordinates.
func NewTranslation(dx, dy Unit) Transform {
	return Transform{OffsetX: dx, OffsetY: dy, ScaleX: 1.0, ScaleY: 1.0}
}

// Apply transforms a point.
func (t Transform) Apply(p UnitPoint) UnitPoint {
	x := Unit(float64(p.X)*t.ScaleX) + t.OffsetX
	y := Unit(float64(p.Y)*t.ScaleY) + t.OffsetY
	return UnitPoint{X: x, Y: y}
}

// ApplyRect transforms a rectangle.
func (t Transform) ApplyRect(r UnitRect) UnitRect {
	topLeft := t.Apply(r.TopLeft())
	w := Unit(float64(r.Width) * t.ScaleX)
	h := Unit(float64(r.Height) * t.ScaleY)
	return UnitRect{X: topLeft.X, Y: topLeft.Y, Width: w, Height: h}
}

// Inverse returns the inverse transform.
func (t Transform) Inverse() Transform {
	inv := Transform{}
	if t.ScaleX != 0 {
		inv.ScaleX = 1.0 / t.ScaleX
	}
	if t.ScaleY != 0 {
		inv.ScaleY = 1.0 / t.ScaleY
	}
	inv.OffsetX = -Unit(float64(t.OffsetX) * inv.ScaleX)
	inv.OffsetY = -Unit(float64(t.OffsetY) * inv.ScaleY)
	return inv
}

// Compose combines two transforms (applies t first, then other).
func (t Transform) Compose(other Transform) Transform {
	return Transform{
		OffsetX: Unit(float64(t.OffsetX)*other.ScaleX) + other.OffsetX,
		OffsetY: Unit(float64(t.OffsetY)*other.ScaleY) + other.OffsetY,
		ScaleX:  t.ScaleX * other.ScaleX,
		ScaleY:  t.ScaleY * other.ScaleY,
	}
}
