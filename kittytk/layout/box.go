// Package layout provides layout managers for arranging trinkets.
package layout

import (
	"github.com/phroun/kittytk/core"
)

// BoxLayout arranges trinkets in a single row or column.
// This is similar to Qt's QBoxLayout, QHBoxLayout, and QVBoxLayout.
type BoxLayout struct {
	BaseLayout
	orientation   core.Orientation
	items         []*LayoutItem
	metricsSource core.Trinket // container whose effective grid metrics apply
}

// SetMetricsSource sets the trinket whose effective grid metrics this
// layout uses (normally the container; wired by Panel). Layouts are
// not trinkets, so they cannot walk the inheritance chain themselves.
func (l *BoxLayout) SetMetricsSource(w core.Trinket) {
	l.metricsSource = w
}

// effectiveMetrics resolves grid metrics from the given container if
// it is a trinket, else from the stored metrics source, else defaults.
func (l *BoxLayout) effectiveMetrics(container core.Container) core.CellMetrics {
	if w, ok := container.(core.Trinket); ok && w != nil {
		return core.FindEffectiveCellMetrics(w)
	}
	if l.metricsSource != nil {
		return core.FindEffectiveCellMetrics(l.metricsSource)
	}
	return core.DefaultCellMetrics()
}

// effectiveDirection resolves the direction items are placed against from the
// given container if it is a trinket, else from the stored metrics source,
// else left to right. Layouts are not trinkets, so they cannot walk the
// inheritance chain themselves.
func (l *BoxLayout) effectiveDirection(container core.Container) core.Direction {
	if w, ok := container.(core.Trinket); ok && w != nil {
		return core.FindEffectiveDirection(w)
	}
	if l.metricsSource != nil {
		return core.FindEffectiveDirection(l.metricsSource)
	}
	return core.DirLTR
}

// NewBoxLayout creates a new box layout with the given orientation.
func NewBoxLayout(orientation core.Orientation) *BoxLayout {
	return &BoxLayout{
		orientation: orientation,
	}
}

// NewHBoxLayout creates a horizontal box layout.
func NewHBoxLayout() *BoxLayout {
	return NewBoxLayout(core.Horizontal)
}

// NewVBoxLayout creates a vertical box layout.
func NewVBoxLayout() *BoxLayout {
	return NewBoxLayout(core.Vertical)
}

// Orientation returns the layout orientation.
func (l *BoxLayout) Orientation() core.Orientation {
	return l.orientation
}

// SetOrientation sets the layout orientation.
func (l *BoxLayout) SetOrientation(o core.Orientation) {
	l.orientation = o
}

// AddTrinket adds a trinket to the layout, honoring the trinket's own
// layout hints (stretch/align travel with the child).
func (l *BoxLayout) AddTrinket(trinket core.Trinket) {
	item := NewLayoutItem(trinket)
	if h, ok := trinket.(interface{ LayoutStretch() int }); ok {
		if s := h.LayoutStretch(); s > 0 {
			item.Stretch = s
		}
	}
	if h, ok := trinket.(interface{ LayoutAlignment() (core.Alignment, bool) }); ok {
		if a, set := h.LayoutAlignment(); set {
			item.Align = a
		}
	}
	l.items = append(l.items, item)
}

// AddTrinketWithStretch adds a trinket with a stretch factor.
func (l *BoxLayout) AddTrinketWithStretch(trinket core.Trinket, stretch int) {
	item := NewLayoutItem(trinket).WithStretch(stretch)
	l.items = append(l.items, item)
}

// AddStretch adds a stretching spacer.
func (l *BoxLayout) AddStretch(stretch int) {
	spacer := NewStretchSpacer()
	item := NewLayoutItem(spacer).WithStretch(stretch)
	l.items = append(l.items, item)
}

// AddSpacing adds fixed spacing.
func (l *BoxLayout) AddSpacing(size core.Unit) {
	var spacer *Spacer
	if l.orientation == core.Horizontal {
		spacer = NewSpacer(size, 0)
	} else {
		spacer = NewSpacer(0, size)
	}
	l.items = append(l.items, NewLayoutItem(spacer))
}

// InsertTrinket inserts a trinket at the given index.
func (l *BoxLayout) InsertTrinket(index int, trinket core.Trinket) {
	item := NewLayoutItem(trinket)
	if index < 0 {
		index = 0
	}
	if index >= len(l.items) {
		l.items = append(l.items, item)
		return
	}
	l.items = append(l.items[:index], append([]*LayoutItem{item}, l.items[index:]...)...)
}

// RemoveTrinket removes a trinket from the layout.
func (l *BoxLayout) RemoveTrinket(trinket core.Trinket) {
	for i, item := range l.items {
		if item.Trinket == trinket {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return
		}
	}
}

// Count returns the number of items.
func (l *BoxLayout) Count() int {
	return len(l.items)
}

// ItemAt returns the item at the given index.
func (l *BoxLayout) ItemAt(index int) *LayoutItem {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	return l.items[index]
}

// spacingTotal is ALL the room Layout puts between and around the items along
// the main axis, which is what SizeHint and MinimumSize have to promise: a box
// handed less than Layout then consumes lays its children out past its own
// edge, and a row of buttons put its last shadow through whatever was drawn to
// the right of it.
func (l *BoxLayout) spacingTotal(container core.Container) core.Unit {
	if len(l.items) < 1 {
		return 0
	}
	metrics := l.effectiveMetrics(container)
	if l.orientation == core.Vertical {
		spacing := core.Unit(metrics.UnitsToCellY(l.spacing)) * metrics.UnitsPerCellHeight
		return spacing * core.Unit(len(l.items)-1)
	}
	base := core.Unit(metrics.UnitsToCellX(l.spacing)) * metrics.UnitsPerCellWidth
	total := l.inlineSpacingForItems(metrics)
	for i := 0; i < len(l.items)-1; i++ {
		if !isInlineTrinket(l.items[i].Trinket) && !isInlineTrinket(l.items[i+1].Trinket) {
			total += base
		}
	}
	return total
}

// itemSize is the size a box gives one item: not below min_width and
// min_height, and otherwise whatever the trinket answers for itself.
//
// min_width and min_height are common wire properties, set in units on any
// trinket. A box read only the trinket's own answer
// and never the properties, so setting them on anything in a box changed
// nothing -- the value was applied and then nothing consulted it.
// GridLayout has always read them.
func itemSize(w core.Trinket) core.UnitSize {
	size := w.SizeHint()
	min := w.MinimumSize()
	if size.Width < min.Width {
		size.Width = min.Width
	}
	if size.Height < min.Height {
		size.Height = min.Height
	}
	return size
}

// Layout arranges children within the given bounds.
func (l *BoxLayout) Layout(container core.Container, bounds core.UnitRect) {
	if len(l.items) == 0 {
		return
	}

	// Apply margins
	rect := l.effectiveBounds(bounds)

	// The direction items are placed against is the CONTAINER's, not each
	// item's own: a right-to-left panel sitting in a left-to-right row is
	// placed against the row it sits in.
	layoutDir := l.effectiveDirection(container)

	// Round spacing to whole cell size based on orientation
	metrics := l.effectiveMetrics(container)
	var spacing core.Unit
	if l.orientation == core.Horizontal {
		// Round to UnitsPerCellWidth
		spacing = core.Unit(metrics.UnitsToCellX(l.spacing)) * metrics.UnitsPerCellWidth
	} else {
		// Round to UnitsPerCellHeight
		spacing = core.Unit(metrics.UnitsToCellY(l.spacing)) * metrics.UnitsPerCellHeight
	}

	inlineSpacingTotal := l.inlineSpacingForItems(metrics)

	// Calculate sizes along the primary axis
	var sizes []core.Unit
	if l.orientation == core.Horizontal {
		sizes = l.horizontalItemWidths(rect.Width, metrics, spacing, inlineSpacingTotal)
	} else {
		totalSpacing := spacing * core.Unit(len(l.items)-1)
		stretchItems := make([]stretchItem, len(l.items))

		for i, item := range l.items {
			hint := itemSize(item.Trinket)
			policy := item.Trinket.SizePolicy()

			minSize := hint.Height
			// Height-for-width trinkets (e.g. wrapped text) report their
			// real height at the width they will actually receive.
			if h := itemHeightForWidth(item.Trinket, l.verticalItemWidth(rect.Width, item, metrics)); h > 0 {
				minSize = h
			}
			if m := item.Trinket.MinimumSize().Height; minSize < m {
				minSize = m
			}

			stretch := 0
			if policy.Vertical == core.SizeExpanding || item.Stretch > 0 {
				stretch = item.Stretch
				if stretch == 0 {
					stretch = 1
				}
			}

			stretchItems[i] = stretchItem{
				minimum: minSize,
				stretch: stretch,
			}
		}

		sizes = calculateStretch(rect.Height-totalSpacing, stretchItems)
	}

	// The line's decoration room, set aside before anything is aligned in it.
	allowance := l.styleAllowance()

	// Position trinkets
	var pos core.Unit
	if l.orientation == core.Horizontal {
		pos = rect.X
		// Add margin before first inline trinket
		if len(l.items) > 0 && isInlineTrinket(l.items[0].Trinket) {
			pos += metrics.UnitsPerCellWidth
		}
	} else {
		pos = rect.Y
	}

	for i, item := range l.items {
		var itemBounds core.UnitRect

		if l.orientation == core.Horizontal {
			itemBounds = core.UnitRect{
				X:      pos,
				Y:      rect.Y,
				Width:  sizes[i],
				Height: rect.Height,
			}
			pos += sizes[i]

			// Add spacing after this item (before the next one)
			// For inline trinkets, use inline spacing; for containers, use base spacing
			if i < len(l.items)-1 {
				if isInlineTrinket(item.Trinket) || isInlineTrinket(l.items[i+1].Trinket) {
					pos += metrics.UnitsPerCellWidth // Inline spacing
				} else {
					pos += spacing // Container-to-container spacing
				}
			}
		} else {
			// In vertical layout, apply horizontal margin to inline trinkets
			itemX := rect.X
			itemWidth := rect.Width

			if inlineTrinket, ok := item.Trinket.(core.InlineTrinket); ok && inlineTrinket.IsInlineTrinket() {
				// Add 1-cell horizontal margin on each side
				itemX += metrics.UnitsPerCellWidth
				itemWidth -= metrics.UnitsPerCellWidth * 2
				if itemWidth < 0 {
					itemWidth = 0
				}
			}

			itemBounds = core.UnitRect{
				X:      itemX,
				Y:      pos,
				Width:  itemWidth,
				Height: sizes[i],
			}
			pos += sizes[i] + spacing
		}

		// Apply alignment within the item bounds
		itemBounds = l.alignItem(item, itemBounds, allowance, layoutDir)
		item.Trinket.SetBounds(itemBounds)
	}
}

// styleAllowance is the part of a line's cross-axis size that exists ONLY
// because something in it reserves room for decoration -- the row a button's
// drop shadow adds beneath a line of otherwise one-row trinkets.
//
// The line minus its tallest CONTENT. Where the tallest item is content (a list
// three rows deep beside a button) the shadow already fits inside the line and
// the answer is nothing: no one is being aligned around decoration, so none is
// set aside. Where the button is what made the line tall, the answer is that
// row, and it is kept out of the band its neighbours align in -- which is what
// puts a one-row field level with the button's cap instead of half a row under
// it.
//
// Trailing edges only, which is where the one decoration in this toolkit falls.
// A leading inset would come off the other end and is not implemented.
func (l *BoxLayout) styleAllowance() core.UnitMargins {
	var outerW, outerH, contentW, contentH core.Unit
	for _, item := range l.items {
		hint := itemSize(item.Trinket)
		ins := core.FindStyleInsets(item.Trinket)
		if hint.Width > outerW {
			outerW = hint.Width
		}
		if hint.Height > outerH {
			outerH = hint.Height
		}
		if c := hint.Width - ins.Horizontal(); c > contentW {
			contentW = c
		}
		if c := hint.Height - ins.Vertical(); c > contentH {
			contentH = c
		}
	}
	allow := core.UnitMargins{Right: outerW - contentW, Bottom: outerH - contentH}
	if allow.Right < 0 {
		allow.Right = 0
	}
	if allow.Bottom < 0 {
		allow.Bottom = 0
	}
	return allow
}

// alignItem places an item in its allocation.
//
// Alignment happens inside the BAND -- the allocation less the line's
// decoration room -- so what one trinket keeps for a shadow is not something
// the others line up against. The item's own insets go back on afterwards, so
// its decoration reaches into the room that was set aside for it.
//
// The result is not clamped to the allocation. An item aligned left already
// takes its own size whether or not the box is wide enough to hold it -- a
// panel that came out narrower than its contents still shows them -- and
// clamping here would take that away.
func (l *BoxLayout) alignItem(item *LayoutItem, bounds core.UnitRect, allowance core.UnitMargins, layoutDir core.Direction) core.UnitRect {
	ins := core.FindStyleInsets(item.Trinket)

	// The band is what a FILLING item may take: decoration room is not room to
	// grow into. Positional alignment gets the whole allocation instead -- a
	// button centred in a three-row row puts its cap on the middle row and its
	// shadow on the last, rather than centring the pair.
	band := bounds
	band.Width -= allowance.Right
	band.Height -= allowance.Bottom
	if band.Width < 0 {
		band.Width = 0
	}
	if band.Height < 0 {
		band.Height = 0
	}

	out := l.alignContent(item, bounds, band, ins, layoutDir)

	// Only on the CROSS axis, which is the one alignment placed from the
	// content size. Along the main axis the allocation came from the sizing
	// pass, which measured the whole trinket, decoration included -- adding it
	// again there stretched a button in a vertical box by its own shadow row.
	if l.orientation == core.Horizontal {
		out.Height += ins.Vertical()
	} else {
		out.Width += ins.Horizontal()
	}
	return out
}

// alignContent places an item's CONTENT: positioned within bounds, the whole
// allocation, and grown no further than band when it fills.
func (l *BoxLayout) alignContent(item *LayoutItem, bounds, band core.UnitRect, ins core.UnitMargins, layoutDir core.Direction) core.UnitRect {
	hint := itemSize(item.Trinket)
	hint.Width -= ins.Horizontal()
	hint.Height -= ins.Vertical()
	policy := item.Trinket.SizePolicy()
	align := item.Align

	if l.orientation == core.Horizontal {
		// A cross axis the trinket says is FIXED does not grow, whatever it is
		// asked to do: a button's cap is one row and a field is one row, and a
		// row three deep holds them at their own height rather than stretching
		// them to it. There is nothing to fill, so the item sits where its
		// alignment puts it.
		fill := align.FillV && policy.Vertical != core.SizeFixed

		// A trinket whose cross-axis policy is Expanding fills the
		// allocation; alignment clamps only trinkets that don't want
		// to grow (separators, text inputs vs. buttons, etc.).
		if policy.Vertical == core.SizeExpanding {
			bounds.Height = band.Height
			return bounds
		}

		// Height-for-width trinkets flow within their allocated width;
		// align them using their real height, not the hint.
		height := hint.Height
		if hasHeightForWidth(item.Trinket) {
			height = itemHeightForWidth(item.Trinket, bounds.Width)
		}

		// Filling stretches the child to the row, and is what an item gets
		// when nothing says otherwise; an item that does not fill keeps its
		// natural height, so a one-row text input beside a taller button stays
		// one row instead of growing to the button's height, placed in the row
		// by its own alignment.
		if fill {
			bounds.Height = band.Height
			return bounds
		}
		switch align.V {
		case core.AlignTop:
			bounds.Height = height
		case core.AlignBottom:
			// To the BAND's bottom edge: the row a shadow reserves is not a row
			// to sit on. Middle below ignores the reservation instead, because
			// the shadow is not in the middle and has no business moving it.
			if height < band.Height {
				bounds.Y += band.Height - height
				bounds.Height = height
			}
		default: // AlignMiddle and unspecified
			if height < bounds.Height {
				// Snap the centering offset to the cell grid. A sub-row offset
				// (a 1-row item centered in a 2-row row is half a row down) is
				// drawn snapped to a row on a cell surface but hit-tested at the
				// raw half-row bounds, so clicks land a row off; grid-aligning
				// keeps draw and hit together. Pixel surfaces are unaffected -
				// the offset is already a whole number of rows there or rounds
				// to the same row.
				off := (bounds.Height - height) / 2
				if ch := core.FindEffectiveCellMetrics(item.Trinket).UnitsPerCellHeight; ch > 0 {
					off = (off / ch) * ch
				}
				bounds.Y += off
				bounds.Height = height
			}
		}
	} else {
		fill := align.FillH && policy.Horizontal != core.SizeFixed

		// Cross-axis Expanding fills the allocation (see above).
		if policy.Horizontal == core.SizeExpanding {
			bounds.Width = band.Width
			return bounds
		}

		// Height-for-width trinkets must receive their allocated width —
		// clamping them to the (unwrapped) hint width would defeat
		// wrapping entirely. Alignment keeps its vertical meaning only.
		if hasHeightForWidth(item.Trinket) {
			return bounds
		}

		if fill {
			bounds.Width = band.Width
			return bounds
		}

		// The logical alignments are spent HERE, where the trinket and the
		// direction around it are both known; what is left is a side.
		switch core.ResolveHAlign(align.H, core.FindTextDirection(item.Trinket), layoutDir) {
		case core.SideLeft:
			bounds.Width = hint.Width
		case core.SideCenter:
			if hint.Width < bounds.Width {
				// Grid-snap the offset (see the vertical-centering note) so a
				// cell surface draws and hit-tests the child in the same column.
				off := (bounds.Width - hint.Width) / 2
				if cw := core.FindEffectiveCellMetrics(item.Trinket).UnitsPerCellWidth; cw > 0 {
					off = (off / cw) * cw
				}
				bounds.X += off
				bounds.Width = hint.Width
			}
		case core.SideRight:
			// To the BAND's right edge, so a right-aligned trinket leaves the
			// column a neighbour's shadow falls in free -- and one with a
			// shadow of its own puts its cap there and the shadow beyond it.
			if hint.Width < band.Width {
				bounds.X += band.Width - hint.Width
				bounds.Width = hint.Width
			}
		}
	}

	return bounds
}

// hasHeightForWidth reports whether the trinket currently has
// width-dependent height.
func hasHeightForWidth(w core.Trinket) bool {
	hfw, ok := w.(core.HeightForWidther)
	return ok && hfw.HasHeightForWidth()
}

// horizontalItemWidths computes item widths for the horizontal
// orientation given the content width (margins already removed),
// mirroring Layout's spacing rules.
func (l *BoxLayout) horizontalItemWidths(contentWidth core.Unit, metrics core.CellMetrics, baseSpacing, inlineSpacingTotal core.Unit) []core.Unit {
	// For inline gaps, use inline spacing; for container gaps, use base spacing
	totalSpacing := inlineSpacingTotal
	for i := 0; i < len(l.items)-1; i++ {
		if !isInlineTrinket(l.items[i].Trinket) && !isInlineTrinket(l.items[i+1].Trinket) {
			totalSpacing += baseSpacing
		}
	}

	stretchItems := make([]stretchItem, len(l.items))
	for i, item := range l.items {
		hint := itemSize(item.Trinket)
		policy := item.Trinket.SizePolicy()

		stretch := 0
		if policy.Horizontal == core.SizeExpanding || item.Stretch > 0 {
			stretch = item.Stretch
			if stretch == 0 {
				stretch = 1
			}
		}

		stretchItems[i] = stretchItem{
			minimum: hint.Width,
			stretch: stretch,
		}
	}

	return calculateStretch(contentWidth-totalSpacing, stretchItems)
}

// verticalItemWidth returns the width an item will receive in a
// vertical layout (inline trinkets are inset one cell per side).
func (l *BoxLayout) verticalItemWidth(contentWidth core.Unit, item *LayoutItem, metrics core.CellMetrics) core.Unit {
	if isInlineTrinket(item.Trinket) {
		contentWidth -= metrics.UnitsPerCellWidth * 2
	}
	if contentWidth < 0 {
		contentWidth = 0
	}
	return contentWidth
}

// itemHeightForWidth returns a trinket's height at the given width,
// consulting core.HeightForWidther when implemented and falling back
// to the size hint.
func itemHeightForWidth(w core.Trinket, width core.Unit) core.Unit {
	if hfw, ok := w.(core.HeightForWidther); ok && hfw.HasHeightForWidth() {
		if h := hfw.HeightForWidth(width); h > 0 {
			return h
		}
	}
	return w.SizeHint().Height
}

// inlineSpacingForItems is the breathing room a horizontal box opens around
// INLINE trinkets -- the small controls that read as part of a sentence rather
// than as blocks: a column before the first if it is inline, one between any
// two where either is, and one after the last if it is.
//
// It is not the configured spacing and does not replace it; between two items
// that are both blocks the configured spacing applies instead. Layout consumes
// it, so spacingTotal has to promise it.
func (l *BoxLayout) inlineSpacingForItems(metrics core.CellMetrics) core.Unit {
	var total core.Unit
	if len(l.items) == 0 {
		return 0
	}
	if isInlineTrinket(l.items[0].Trinket) {
		total += metrics.UnitsPerCellWidth
	}
	for i := 0; i < len(l.items)-1; i++ {
		if isInlineTrinket(l.items[i].Trinket) || isInlineTrinket(l.items[i+1].Trinket) {
			total += metrics.UnitsPerCellWidth
		}
	}
	if isInlineTrinket(l.items[len(l.items)-1].Trinket) {
		total += metrics.UnitsPerCellWidth
	}
	return total
}

// HasHeightForWidth reports whether any item in this layout has
// width-dependent height. Together with HeightForWidth this lets
// containers (Panel) propagate core.HeightForWidther upward.
func (l *BoxLayout) HasHeightForWidth() bool {
	for _, item := range l.items {
		if hfw, ok := item.Trinket.(core.HeightForWidther); ok && hfw.HasHeightForWidth() {
			return true
		}
	}
	return false
}

// HeightForWidth returns the height this layout requires at the given
// container width.
func (l *BoxLayout) HeightForWidth(width core.Unit) core.Unit {
	if len(l.items) == 0 {
		return 0
	}

	metrics := l.effectiveMetrics(nil)
	contentWidth := width - l.margins.Horizontal()
	if contentWidth < 0 {
		contentWidth = 0
	}

	var height core.Unit
	if l.orientation == core.Vertical {
		spacing := core.Unit(metrics.UnitsToCellY(l.spacing)) * metrics.UnitsPerCellHeight
		for i, item := range l.items {
			height += itemHeightForWidth(item.Trinket, l.verticalItemWidth(contentWidth, item, metrics))
			if i < len(l.items)-1 {
				height += spacing
			}
		}
	} else {
		spacing := core.Unit(metrics.UnitsToCellX(l.spacing)) * metrics.UnitsPerCellWidth
		widths := l.horizontalItemWidths(contentWidth, metrics, spacing, l.inlineSpacingForItems(metrics))
		for i, item := range l.items {
			if h := itemHeightForWidth(item.Trinket, widths[i]); h > height {
				height = h
			}
		}
	}

	return height + l.margins.Vertical()
}

// SizeHint returns the preferred size for the container.
func (l *BoxLayout) SizeHint(container core.Container) core.UnitSize {
	var width, height core.Unit

	for _, item := range l.items {
		hint := itemSize(item.Trinket)

		if l.orientation == core.Horizontal {
			width += hint.Width
			if hint.Height > height {
				height = hint.Height
			}
		} else {
			height += hint.Height
			if hint.Width > width {
				width = hint.Width
			}
		}
	}

	if spacing := l.spacingTotal(container); l.orientation == core.Horizontal {
		width += spacing
	} else {
		height += spacing
	}

	// Add margins
	width += l.margins.Horizontal()
	height += l.margins.Vertical()

	return core.UnitSize{Width: width, Height: height}
}

// MinimumSize returns the minimum size for the container.
func (l *BoxLayout) MinimumSize(container core.Container) core.UnitSize {
	var width, height core.Unit

	for _, item := range l.items {
		minSize := item.Trinket.MinimumSize()

		if l.orientation == core.Horizontal {
			width += minSize.Width
			if minSize.Height > height {
				height = minSize.Height
			}
		} else {
			height += minSize.Height
			if minSize.Width > width {
				width = minSize.Width
			}
		}
	}

	// Add spacing
	if spacing := l.spacingTotal(container); l.orientation == core.Horizontal {
		width += spacing
	} else {
		height += spacing
	}

	// Add margins
	width += l.margins.Horizontal()
	height += l.margins.Vertical()

	return core.UnitSize{Width: width, Height: height}
}
