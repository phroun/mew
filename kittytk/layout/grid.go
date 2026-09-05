// Package layout provides layout managers for arranging trinkets.
package layout

import (
	"github.com/phroun/kittytk/core"
)

// GridItem represents a trinket placed in a grid.
type GridItem struct {
	Trinket    core.Trinket
	Row        int
	Column     int
	RowSpan    int
	ColumnSpan int
	Align      core.Alignment
	// RowID and ColumnID are the bands the child named, if it named any.
	// They settle into Row and Column on every pass, because a grid may be
	// given its bands after its children.
	RowID    string
	ColumnID string
	// RowStretch and ColumnStretch are what the child asked of the bands it
	// sits in, folded into them once those bands are known.
	RowStretch    int
	ColumnStretch int
}

// GridLayout arranges trinkets in a grid of rows and columns.
// This is similar to Qt's QGridLayout.
type GridLayout struct {
	BaseLayout
	items   []*GridItem
	columns []Band
	rows    []Band
}

// NewGridLayout creates a new grid layout.
func NewGridLayout() *GridLayout {
	return &GridLayout{}
}

// AddColumn and AddRow append a band, which takes the index its position
// gives it. A grid may be described only as far as it needs describing: the
// bands past the last one given behave as bands that ask for nothing.
func (l *GridLayout) AddColumn(b Band) { l.columns = append(l.columns, b) }

// AddRow appends a row band (see AddColumn).
func (l *GridLayout) AddRow(b Band) { l.rows = append(l.rows, b) }

// Columns and Rows return the bands as given.
func (l *GridLayout) Columns() []Band { return l.columns }

// Rows returns the row bands as given (see Columns).
func (l *GridLayout) Rows() []Band { return l.rows }

// effectiveMetrics resolves grid metrics from the given container if it is a
// trinket, else the defaults. Layouts are not trinkets, so they cannot walk the
// inheritance chain themselves.
func (l *GridLayout) effectiveMetrics(container core.Container) core.CellMetrics {
	if w, ok := container.(core.Trinket); ok && w != nil {
		return core.FindEffectiveCellMetrics(w)
	}
	return core.DefaultCellMetrics()
}

// AddTrinket adds a trinket where its own placement hint puts it. A child that
// states none lands in a row of its own, in column zero, so a grid nobody has
// placed anything in reads down the page like a column.
//
// This is the one-argument shape a container calls when it attaches a child,
// which is how a grid is reachable from a build script at all: everything the
// grid needs to know travels on the child.
func (l *GridLayout) AddTrinket(trinket core.Trinket) {
	p := core.GridPlacement{Row: len(l.items), RowSpan: 1, ColumnSpan: 1}
	if h, ok := trinket.(interface {
		LayoutGridPlacement() (core.GridPlacement, bool)
	}); ok {
		if hint, set := h.LayoutGridPlacement(); set {
			p = hint
		}
	}
	l.AddTrinketAtWithSpan(trinket, p.Row, p.Column, p.RowSpan, p.ColumnSpan)
	item := l.items[len(l.items)-1]
	item.RowID, item.ColumnID = p.RowID, p.ColumnID
	item.RowStretch, item.ColumnStretch = p.RowStretch, p.ColumnStretch
}

// resolveBands settles where each item sits and what its bands were asked
// for. A child that named a band is put in the band with that name, and the
// stretch it asked of that band is folded in -- both here rather than when
// the child was added, because a grid may be given its bands afterwards.
//
// A band is one thing and cannot take two answers, so where two children in
// it ask for different stretches the largest is what the band gets. Folding
// the same answer in twice changes nothing, so every pass may run this.
func (l *GridLayout) resolveBands() {
	for _, item := range l.items {
		if i := bandIndex(l.columns, item.ColumnID); i >= 0 {
			item.Column = i
		}
		if i := bandIndex(l.rows, item.RowID); i >= 0 {
			item.Row = i
		}
		if item.RowStretch > 0 {
			l.SetRowStretch(item.Row, maxInt(item.RowStretch, bandAt(l.rows, item.Row).Stretch))
		}
		if item.ColumnStretch > 0 {
			l.SetColumnStretch(item.Column, maxInt(item.ColumnStretch, bandAt(l.columns, item.Column).Stretch))
		}
	}
}

// AddTrinketAt adds a trinket at the given row and column.
func (l *GridLayout) AddTrinketAt(trinket core.Trinket, row, column int) {
	l.AddTrinketAtWithSpan(trinket, row, column, 1, 1)
}

// AddTrinketAtWithSpan adds a trinket that spans several cells.
func (l *GridLayout) AddTrinketAtWithSpan(trinket core.Trinket, row, column, rowSpan, columnSpan int) {
	if rowSpan < 1 {
		rowSpan = 1
	}
	if columnSpan < 1 {
		columnSpan = 1
	}
	item := &GridItem{
		Trinket:    trinket,
		Row:        row,
		Column:     column,
		RowSpan:    rowSpan,
		ColumnSpan: columnSpan,
		Align:      core.DefaultAlignment(),
	}
	// Alignment travels with the child, as it does in a box, so halign,
	// valign and fill mean the same thing wherever the child is put.
	if h, ok := trinket.(interface {
		LayoutAlignment() (core.Alignment, bool)
	}); ok {
		if a, set := h.LayoutAlignment(); set {
			item.Align = a
		}
	}
	l.items = append(l.items, item)
}

// RemoveTrinket removes a trinket from the layout.
func (l *GridLayout) RemoveTrinket(trinket core.Trinket) {
	for i, item := range l.items {
		if item.Trinket == trinket {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return
		}
	}
}

// Count returns the number of items.
func (l *GridLayout) Count() int { return len(l.items) }

// ItemAt returns the item at the given index, or nil.
func (l *GridLayout) ItemAt(index int) *GridItem {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	return l.items[index]
}

// SetRowStretch sets the stretch factor for a row.
func (l *GridLayout) SetRowStretch(row, stretch int) {
	l.rows = growBands(l.rows, row)
	l.rows[row].Stretch = stretch
}

// SetColumnStretch sets the stretch factor for a column.
func (l *GridLayout) SetColumnStretch(column, stretch int) {
	l.columns = growBands(l.columns, column)
	l.columns[column].Stretch = stretch
}

// SetRowMinimumHeight sets the minimum height for a row.
func (l *GridLayout) SetRowMinimumHeight(row int, height core.Unit) {
	l.rows = growBands(l.rows, row)
	l.rows[row].Minimum = height
}

// SetColumnMinimumWidth sets the minimum width for a column.
func (l *GridLayout) SetColumnMinimumWidth(column int, width core.Unit) {
	l.columns = growBands(l.columns, column)
	l.columns[column].Minimum = width
}

// maxInt is the larger of two stretch factors.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RowCount returns the number of rows.
func (l *GridLayout) RowCount() int {
	l.resolveBands()
	maxRow := 0
	for _, item := range l.items {
		endRow := item.Row + item.RowSpan
		if endRow > maxRow {
			maxRow = endRow
		}
	}
	return maxRow
}

// ColumnCount returns the number of columns.
func (l *GridLayout) ColumnCount() int {
	l.resolveBands()
	maxCol := 0
	for _, item := range l.items {
		endCol := item.Column + item.ColumnSpan
		if endCol > maxCol {
			maxCol = endCol
		}
	}
	return maxCol
}

// Layout arranges children within the given bounds.
func (l *GridLayout) Layout(container core.Container, bounds core.UnitRect) {
	if len(l.items) == 0 {
		return
	}

	// The direction items are placed against is the grid's own, not each
	// item's (see BoxLayout.effectiveDirection).
	layoutDir := core.DirLTR
	metrics := core.DefaultCellMetrics()
	if w, ok := container.(core.Trinket); ok && w != nil {
		layoutDir = core.FindEffectiveDirection(w)
		metrics = core.FindEffectiveCellMetrics(w)
	}

	rect := l.effectiveBounds(bounds)
	rows := l.RowCount()
	cols := l.ColumnCount()

	if rows == 0 || cols == 0 {
		return
	}

	// What the boundaries take -- or give back, where two bearings close up --
	// is not the columns' to divide.
	gaps := l.columnGaps(cols, metrics)
	colWidths := l.calculateColumnWidths(rect.Width-sumGaps(gaps), cols, metrics)

	// Calculate row heights
	rowHeights := l.calculateRowHeights(rect.Height, rows)

	// Calculate column positions.
	colX := make([]core.Unit, cols+1)
	colX[0] = rect.X
	for i := 0; i < cols; i++ {
		colX[i+1] = colX[i] + colWidths[i]
		if i < cols-1 {
			colX[i+1] += gaps[i]
		}
	}

	// Calculate row positions
	rowY := make([]core.Unit, rows+1)
	rowY[0] = rect.Y
	for i := 0; i < rows; i++ {
		rowY[i+1] = rowY[i] + rowHeights[i] + l.spacing
	}

	// Position each item
	for _, item := range l.items {
		x := colX[item.Column]
		y := rowY[item.Row]

		// Calculate width (sum of spanned columns, and the boundaries between)
		width := core.Unit(0)
		for c := item.Column; c < item.Column+item.ColumnSpan && c < cols; c++ {
			width += colWidths[c]
			if c > item.Column {
				width += gaps[c-1]
			}
		}

		// Calculate height (sum of spanned rows)
		height := core.Unit(0)
		for r := item.Row; r < item.Row+item.RowSpan && r < rows; r++ {
			height += rowHeights[r]
			if r > item.Row {
				height += l.spacing
			}
		}

		itemBounds := core.UnitRect{X: x, Y: y, Width: width, Height: height}

		// Apply alignment
		itemBounds = l.alignItem(item, itemBounds, layoutDir, metrics)
		item.Trinket.SetBounds(itemBounds)
	}
}

// columnGaps is the space between one column and the next, one entry per
// boundary, and it is the box's rule read for columns.
//
// A cell keeps its child's side-bearings INSIDE it, which is what lets a block
// whose cell reaches the grid's edge line up with an inline child beside it --
// the block's own trailing bearing lands where the inline child's cell-inset
// one does. So the OUTER bearings stay where they are, and only the boundaries
// between columns are settled here:
//
//   - both sides inline: their two bearings are one column of air, not two, so
//     the boundary closes up by a column to bring them together;
//   - one side inline: its single bearing is the whole of the air, and the
//     boundary adds nothing;
//   - neither: nothing has opened any air, so the configured spacing does.
//
// Which is what a box does along its run -- a column between two items where
// either is inline, the configured spacing between two blocks -- so a form's
// columns are as far apart as the same controls in a row would be.
//
// A column is shared by every row and a boundary can have only one answer, so
// any row that puts an inline child on a side settles that side, as the largest
// stretch asked of a column settles its stretch. A child that SPANS the
// boundary straddles it and brings no bearing to it.
func (l *GridLayout) columnGaps(cols int, metrics core.CellMetrics) []core.Unit {
	if cols < 2 {
		return nil
	}
	endsInline := make([]bool, cols)
	startsInline := make([]bool, cols)
	for _, item := range l.items {
		if !isInlineTrinket(item.Trinket) {
			continue
		}
		if end := item.Column + item.ColumnSpan - 1; end >= 0 && end < cols {
			endsInline[end] = true
		}
		if item.Column >= 0 && item.Column < cols {
			startsInline[item.Column] = true
		}
	}

	gaps := make([]core.Unit, cols-1)
	for c := 0; c < cols-1; c++ {
		left, right := endsInline[c], startsInline[c+1]
		switch {
		case left && right:
			gaps[c] = -metrics.UnitsPerCellWidth
		case left || right:
			gaps[c] = 0
		default:
			gaps[c] = l.spacing
		}
	}
	return gaps
}

// sumGaps is what all the boundaries take out of the room the columns divide.
func sumGaps(gaps []core.Unit) core.Unit {
	total := core.Unit(0)
	for _, g := range gaps {
		total += g
	}
	return total
}

// calculateColumnWidths calculates the width of each column.
func (l *GridLayout) calculateColumnWidths(available core.Unit, cols int, metrics core.CellMetrics) []core.Unit {
	// Collect minimum widths and stretch factors
	items := make([]stretchItem, cols)

	for c := 0; c < cols; c++ {
		// The column's own floor, then what its children need. itemSize is
		// the child's hint raised to its own min_width, so a minimum written
		// on a child reaches the column it sits in -- as it reaches the line
		// it sits in inside a box.
		minWidth := bandAt(l.columns, c).Minimum

		for _, item := range l.items {
			if item.Column == c && item.ColumnSpan == 1 {
				// Plus the child's own side-bearings, which the column has to
				// hold for it: a cell is where the child goes, bearings and all.
				w := itemSize(item.Trinket).Width + 2*sideBearing(item.Trinket, metrics)
				if w > minWidth {
					minWidth = w
				}
			}
		}

		items[c] = stretchItem{
			minimum: minWidth,
			stretch: bandAt(l.columns, c).Stretch,
		}
	}

	// The boundaries were taken out by the caller (see columnGaps).
	return calculateStretch(available, items)
}

// calculateRowHeights calculates the height of each row.
func (l *GridLayout) calculateRowHeights(available core.Unit, rows int) []core.Unit {
	// Collect minimum heights and stretch factors
	items := make([]stretchItem, rows)

	for r := 0; r < rows; r++ {
		// The row's own floor, then what its children need (see above).
		minHeight := bandAt(l.rows, r).Minimum

		for _, item := range l.items {
			if item.Row == r && item.RowSpan == 1 {
				if h := itemSize(item.Trinket).Height; h > minHeight {
					minHeight = h
				}
			}
		}

		items[r] = stretchItem{
			minimum: minHeight,
			stretch: bandAt(l.rows, r).Stretch,
		}
	}

	// Account for spacing
	totalSpacing := l.spacing * core.Unit(rows-1)
	availableForRows := available - totalSpacing

	return calculateStretch(availableForRows, items)
}

// alignItem adjusts item bounds based on alignment. Each axis is placed on
// its own: an item can fill its column and sit at the top of its row.
func (l *GridLayout) alignItem(item *GridItem, bounds core.UnitRect, layoutDir core.Direction, metrics core.CellMetrics) core.UnitRect {
	// The child's side-bearings come off the cell before anything is placed in
	// it, so an inline child in a grid sits a column in from its cell's edges --
	// exactly as it does in a box, and level with one that got there through a
	// box nested in the cell beside it.
	bounds = insetForBearing(item.Trinket, metrics, bounds)

	hint := item.Trinket.SizeHint()

	// Horizontal placement, once the logical alignment is spent against the
	// item's own text and the direction around the grid.
	if !item.Align.FillH && hint.Width < bounds.Width {
		switch core.ResolveHAlign(item.Align.H, core.FindTextDirection(item.Trinket), layoutDir) {
		case core.SideLeft:
			bounds.Width = hint.Width
		case core.SideCenter:
			bounds.X += (bounds.Width - hint.Width) / 2
			bounds.Width = hint.Width
		case core.SideRight:
			bounds.X += bounds.Width - hint.Width
			bounds.Width = hint.Width
		}
	}

	// Vertical placement.
	if !item.Align.FillV && hint.Height < bounds.Height {
		switch item.Align.V {
		case core.AlignTop:
			bounds.Height = hint.Height
		case core.AlignMiddle:
			bounds.Y += (bounds.Height - hint.Height) / 2
			bounds.Height = hint.Height
		case core.AlignBottom:
			bounds.Y += bounds.Height - hint.Height
			bounds.Height = hint.Height
		}
	}

	return bounds
}

// SizeHint returns the preferred size for the container.
func (l *GridLayout) SizeHint(container core.Container) core.UnitSize {
	rows := l.RowCount()
	cols := l.ColumnCount()

	if rows == 0 || cols == 0 {
		return core.UnitSize{}
	}

	metrics := l.effectiveMetrics(container)

	// Calculate preferred column widths, bearings included: a column has to
	// hold them, so the grid has to ask for them.
	colWidths := make([]core.Unit, cols)
	for c := 0; c < cols; c++ {
		colWidths[c] = bandAt(l.columns, c).Minimum
		for _, item := range l.items {
			if item.Column == c && item.ColumnSpan == 1 {
				w := item.Trinket.SizeHint().Width + 2*sideBearing(item.Trinket, metrics)
				if w > colWidths[c] {
					colWidths[c] = w
				}
			}
		}
	}

	// Calculate preferred row heights
	rowHeights := make([]core.Unit, rows)
	for r := 0; r < rows; r++ {
		rowHeights[r] = bandAt(l.rows, r).Minimum
		for _, item := range l.items {
			if item.Row == r && item.RowSpan == 1 {
				hint := item.Trinket.SizeHint()
				if hint.Height > rowHeights[r] {
					rowHeights[r] = hint.Height
				}
			}
		}
	}

	// Sum up
	var width, height core.Unit
	for _, w := range colWidths {
		width += w
	}
	for _, h := range rowHeights {
		height += h
	}

	// Add spacing
	width += l.spacing * core.Unit(cols-1)
	height += l.spacing * core.Unit(rows-1)

	// Add margins
	width += l.margins.Horizontal()
	height += l.margins.Vertical()

	return core.UnitSize{Width: width, Height: height}
}

// MinimumSize returns the minimum size for the container.
func (l *GridLayout) MinimumSize(container core.Container) core.UnitSize {
	rows := l.RowCount()
	cols := l.ColumnCount()

	if rows == 0 || cols == 0 {
		return core.UnitSize{}
	}

	metrics := l.effectiveMetrics(container)

	// Calculate minimum column widths, bearings included (see SizeHint).
	colWidths := make([]core.Unit, cols)
	for c := 0; c < cols; c++ {
		colWidths[c] = bandAt(l.columns, c).Minimum
		for _, item := range l.items {
			if item.Column == c && item.ColumnSpan == 1 {
				w := item.Trinket.MinimumSize().Width + 2*sideBearing(item.Trinket, metrics)
				if w > colWidths[c] {
					colWidths[c] = w
				}
			}
		}
	}

	// Calculate minimum row heights
	rowHeights := make([]core.Unit, rows)
	for r := 0; r < rows; r++ {
		rowHeights[r] = bandAt(l.rows, r).Minimum
		for _, item := range l.items {
			if item.Row == r && item.RowSpan == 1 {
				minSize := item.Trinket.MinimumSize()
				if minSize.Height > rowHeights[r] {
					rowHeights[r] = minSize.Height
				}
			}
		}
	}

	// Sum up
	var width, height core.Unit
	for _, w := range colWidths {
		width += w
	}
	for _, h := range rowHeights {
		height += h
	}

	// Add spacing
	width += l.spacing * core.Unit(cols-1)
	height += l.spacing * core.Unit(rows-1)

	// Add margins
	width += l.margins.Horizontal()
	height += l.margins.Vertical()

	return core.UnitSize{Width: width, Height: height}
}
