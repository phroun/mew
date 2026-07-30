// Package core provides fundamental types for KittyTK.
package core

// Unit represents an abstract coordinate unit.
// In text mode, units are translated to character cells via CellMetrics.
// In graphics mode, units could map directly to pixels or be scaled.
type Unit int

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

// CellMetrics defines how abstract units map to character cells (or pixels in GUI mode).
// For text mode: a character cell might be 8x16 units (mimicking pixel dimensions).
// For graphics mode: units might map 1:1 to pixels, or be scaled.
type CellMetrics struct {
	// CellWidth is the width of one character cell in units.
	// Default for TUI: 8 (like a typical 8-pixel wide character)
	CellWidth Unit

	// CellHeight is the height of one character cell in units.
	// Default for TUI: 16 (like a typical 16-pixel tall character)
	CellHeight Unit
}

// DefaultCellMetrics returns standard 8x16 cell metrics (typical terminal font proportions).
func DefaultCellMetrics() CellMetrics {
	return CellMetrics{CellWidth: 8, CellHeight: 16}
}

// SquareCellMetrics returns 1:1 cell metrics (each unit = one character cell).
// Use this for simple text-mode layouts where you don't need sub-cell precision.
func SquareCellMetrics() CellMetrics {
	return CellMetrics{CellWidth: 1, CellHeight: 1}
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
	return m.UnitsToCell(x, m.CellWidth)
}

// UnitsToCellY converts a unit Y coordinate to a cell row.
func (m CellMetrics) UnitsToCellY(y Unit) int {
	return m.UnitsToCell(y, m.CellHeight)
}

// CellToUnitsX converts a cell column to unit X coordinate.
func (m CellMetrics) CellToUnitsX(col int) Unit {
	return Unit(col) * m.CellWidth
}

// CellToUnitsY converts a cell row to unit Y coordinate.
func (m CellMetrics) CellToUnitsY(row int) Unit {
	return Unit(row) * m.CellHeight
}

// UnitsToSize converts a unit size to cell dimensions (rounding up).
func (m CellMetrics) UnitsToSize(size UnitSize) (cols, rows int) {
	cols = int((size.Width + m.CellWidth - 1) / m.CellWidth)
	rows = int((size.Height + m.CellHeight - 1) / m.CellHeight)
	return
}

// CellsToUnits converts cell dimensions to unit size.
func (m CellMetrics) CellsToUnits(cols, rows int) UnitSize {
	return UnitSize{
		Width:  Unit(cols) * m.CellWidth,
		Height: Unit(rows) * m.CellHeight,
	}
}

// TextWidth returns the width in units needed to display text with given character count.
func (m CellMetrics) TextWidth(charCount int) Unit {
	return Unit(charCount) * m.CellWidth
}

// TextHeight returns the height in units for a given number of lines.
func (m CellMetrics) TextHeight(lineCount int) Unit {
	return Unit(lineCount) * m.CellHeight
}

// CharsForWidth returns how many characters fit in the given width.
func (m CellMetrics) CharsForWidth(width Unit) int {
	if m.CellWidth <= 0 {
		return 0
	}
	return int(width / m.CellWidth)
}

// LinesForHeight returns how many lines fit in the given height.
func (m CellMetrics) LinesForHeight(height Unit) int {
	if m.CellHeight <= 0 {
		return 0
	}
	return int(height / m.CellHeight)
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
	return m.RoundDownToCell(x, m.CellWidth)
}

// RoundDownToCellY rounds a Y coordinate down to the nearest cell boundary.
func (m CellMetrics) RoundDownToCellY(y Unit) Unit {
	return m.RoundDownToCell(y, m.CellHeight)
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
	if from.CellWidth == to.CellWidth || from.CellWidth <= 0 || to.CellWidth <= 0 {
		return v
	}
	return Unit(float64(v) * float64(to.CellWidth) / float64(from.CellWidth))
}

// ExchangeY converts a Y-axis value denominated in `from` metrics into
// `to` metrics: the same number of rows, re-expressed.
func ExchangeY(v Unit, from, to CellMetrics) Unit {
	if from.CellHeight == to.CellHeight || from.CellHeight <= 0 || to.CellHeight <= 0 {
		return v
	}
	return Unit(float64(v) * float64(to.CellHeight) / float64(from.CellHeight))
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
