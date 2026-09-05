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
}

// GridLayout arranges trinkets in a grid of rows and columns.
// This is similar to Qt's QGridLayout.
type GridLayout struct {
	BaseLayout
	items          []*GridItem
	rowStretch     map[int]int
	columnStretch  map[int]int
	rowMinHeight   map[int]core.Unit
	columnMinWidth map[int]core.Unit
}

// NewGridLayout creates a new grid layout.
func NewGridLayout() *GridLayout {
	return &GridLayout{
		rowStretch:     make(map[int]int),
		columnStretch:  make(map[int]int),
		rowMinHeight:   make(map[int]core.Unit),
		columnMinWidth: make(map[int]core.Unit),
	}
}

// AddTrinket adds a trinket at the specified position.
func (l *GridLayout) AddTrinket(trinket core.Trinket, row, column int) {
	l.AddTrinketWithSpan(trinket, row, column, 1, 1)
}

// AddTrinketWithSpan adds a trinket that spans multiple cells.
func (l *GridLayout) AddTrinketWithSpan(trinket core.Trinket, row, column, rowSpan, columnSpan int) {
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

// SetRowStretch sets the stretch factor for a row.
func (l *GridLayout) SetRowStretch(row, stretch int) {
	l.rowStretch[row] = stretch
}

// SetColumnStretch sets the stretch factor for a column.
func (l *GridLayout) SetColumnStretch(column, stretch int) {
	l.columnStretch[column] = stretch
}

// SetRowMinimumHeight sets the minimum height for a row.
func (l *GridLayout) SetRowMinimumHeight(row int, height core.Unit) {
	l.rowMinHeight[row] = height
}

// SetColumnMinimumWidth sets the minimum width for a column.
func (l *GridLayout) SetColumnMinimumWidth(column int, width core.Unit) {
	l.columnMinWidth[column] = width
}

// RowCount returns the number of rows.
func (l *GridLayout) RowCount() int {
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
	if w, ok := container.(core.Trinket); ok && w != nil {
		layoutDir = core.FindEffectiveDirection(w)
	}

	rect := l.effectiveBounds(bounds)
	rows := l.RowCount()
	cols := l.ColumnCount()

	if rows == 0 || cols == 0 {
		return
	}

	// Calculate column widths
	colWidths := l.calculateColumnWidths(rect.Width, cols)

	// Calculate row heights
	rowHeights := l.calculateRowHeights(rect.Height, rows)

	// Calculate column positions
	colX := make([]core.Unit, cols+1)
	colX[0] = rect.X
	for i := 0; i < cols; i++ {
		colX[i+1] = colX[i] + colWidths[i] + l.spacing
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

		// Calculate width (sum of spanned columns)
		width := core.Unit(0)
		for c := item.Column; c < item.Column+item.ColumnSpan && c < cols; c++ {
			width += colWidths[c]
			if c > item.Column {
				width += l.spacing
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
		itemBounds = l.alignItem(item, itemBounds, layoutDir)
		item.Trinket.SetBounds(itemBounds)
	}
}

// calculateColumnWidths calculates the width of each column.
func (l *GridLayout) calculateColumnWidths(available core.Unit, cols int) []core.Unit {
	// Collect minimum widths and stretch factors
	items := make([]stretchItem, cols)

	for c := 0; c < cols; c++ {
		// Start with configured minimum
		minWidth := l.columnMinWidth[c]

		// Check trinkets in this column
		for _, item := range l.items {
			if item.Column == c && item.ColumnSpan == 1 {
				hint := item.Trinket.SizeHint()
				if hint.Width > minWidth {
					minWidth = hint.Width
				}
			}
		}

		items[c] = stretchItem{
			minimum: minWidth,
			stretch: l.columnStretch[c],
		}
	}

	// Account for spacing
	totalSpacing := l.spacing * core.Unit(cols-1)
	availableForCols := available - totalSpacing

	return calculateStretch(availableForCols, items)
}

// calculateRowHeights calculates the height of each row.
func (l *GridLayout) calculateRowHeights(available core.Unit, rows int) []core.Unit {
	// Collect minimum heights and stretch factors
	items := make([]stretchItem, rows)

	for r := 0; r < rows; r++ {
		// Start with configured minimum
		minHeight := l.rowMinHeight[r]

		// Check trinkets in this row
		for _, item := range l.items {
			if item.Row == r && item.RowSpan == 1 {
				hint := item.Trinket.SizeHint()
				if hint.Height > minHeight {
					minHeight = hint.Height
				}
			}
		}

		items[r] = stretchItem{
			minimum: minHeight,
			stretch: l.rowStretch[r],
		}
	}

	// Account for spacing
	totalSpacing := l.spacing * core.Unit(rows-1)
	availableForRows := available - totalSpacing

	return calculateStretch(availableForRows, items)
}

// alignItem adjusts item bounds based on alignment. Each axis is placed on
// its own: an item can fill its column and sit at the top of its row.
func (l *GridLayout) alignItem(item *GridItem, bounds core.UnitRect, layoutDir core.Direction) core.UnitRect {
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

	// Calculate preferred column widths
	colWidths := make([]core.Unit, cols)
	for c := 0; c < cols; c++ {
		colWidths[c] = l.columnMinWidth[c]
		for _, item := range l.items {
			if item.Column == c && item.ColumnSpan == 1 {
				hint := item.Trinket.SizeHint()
				if hint.Width > colWidths[c] {
					colWidths[c] = hint.Width
				}
			}
		}
	}

	// Calculate preferred row heights
	rowHeights := make([]core.Unit, rows)
	for r := 0; r < rows; r++ {
		rowHeights[r] = l.rowMinHeight[r]
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

	// Calculate minimum column widths
	colWidths := make([]core.Unit, cols)
	for c := 0; c < cols; c++ {
		colWidths[c] = l.columnMinWidth[c]
		for _, item := range l.items {
			if item.Column == c && item.ColumnSpan == 1 {
				minSize := item.Trinket.MinimumSize()
				if minSize.Width > colWidths[c] {
					colWidths[c] = minSize.Width
				}
			}
		}
	}

	// Calculate minimum row heights
	rowHeights := make([]core.Unit, rows)
	for r := 0; r < rows; r++ {
		rowHeights[r] = l.rowMinHeight[r]
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
