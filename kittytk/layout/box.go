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

// isInlineTrinket returns true if the trinket is an inline (non-container) trinket.
func isInlineTrinket(w core.Trinket) bool {
	// If it implements InlineTrinket interface and returns true, it's inline
	if inline, ok := w.(core.InlineTrinket); ok && inline.IsInlineTrinket() {
		return true
	}
	// If it's a Container, it's not inline
	if _, ok := w.(core.Container); ok {
		return false
	}
	// Default: treat as inline if not a container
	return true
}

// Layout arranges children within the given bounds.
func (l *BoxLayout) Layout(container core.Container, bounds core.UnitRect) {
	if len(l.items) == 0 {
		return
	}

	// Apply margins
	rect := l.effectiveBounds(bounds)

	// Round spacing to whole cell size based on orientation
	metrics := l.effectiveMetrics(container)
	var spacing core.Unit
	if l.orientation == core.Horizontal {
		// Round to CellWidth
		spacing = core.Unit(metrics.UnitsToCellX(l.spacing)) * metrics.CellWidth
	} else {
		// Round to CellHeight
		spacing = core.Unit(metrics.UnitsToCellY(l.spacing)) * metrics.CellHeight
	}

	// For horizontal layout, calculate additional spacing for inline trinkets
	var inlineSpacingTotal core.Unit
	if l.orientation == core.Horizontal && len(l.items) > 0 {
		// Space before first inline trinket
		if isInlineTrinket(l.items[0].Trinket) {
			inlineSpacingTotal += metrics.CellWidth
		}
		// Space between items where at least one is inline
		for i := 0; i < len(l.items)-1; i++ {
			if isInlineTrinket(l.items[i].Trinket) || isInlineTrinket(l.items[i+1].Trinket) {
				inlineSpacingTotal += metrics.CellWidth
			}
		}
		// Space after last inline trinket
		if isInlineTrinket(l.items[len(l.items)-1].Trinket) {
			inlineSpacingTotal += metrics.CellWidth
		}
	}

	// Calculate sizes along the primary axis
	var sizes []core.Unit
	if l.orientation == core.Horizontal {
		sizes = l.horizontalItemWidths(rect.Width, metrics, spacing, inlineSpacingTotal)
	} else {
		totalSpacing := spacing * core.Unit(len(l.items)-1)
		stretchItems := make([]stretchItem, len(l.items))

		for i, item := range l.items {
			hint := item.Trinket.SizeHint()
			policy := item.Trinket.SizePolicy()

			minSize := hint.Height
			// Height-for-width trinkets (e.g. wrapped text) report their
			// real height at the width they will actually receive.
			if h := itemHeightForWidth(item.Trinket, l.verticalItemWidth(rect.Width, item, metrics)); h > 0 {
				minSize = h
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

	// Position trinkets
	var pos core.Unit
	if l.orientation == core.Horizontal {
		pos = rect.X
		// Add margin before first inline trinket
		if len(l.items) > 0 && isInlineTrinket(l.items[0].Trinket) {
			pos += metrics.CellWidth
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
					pos += metrics.CellWidth // Inline spacing
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
				itemX += metrics.CellWidth
				itemWidth -= metrics.CellWidth * 2
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
		itemBounds = l.alignItem(item, itemBounds)
		item.Trinket.SetBounds(itemBounds)
	}
}

// alignItem adjusts item bounds based on alignment.
func (l *BoxLayout) alignItem(item *LayoutItem, bounds core.UnitRect) core.UnitRect {
	hint := item.Trinket.SizeHint()
	policy := item.Trinket.SizePolicy()

	if l.orientation == core.Horizontal {
		// A trinket whose cross-axis policy is Expanding fills the
		// allocation; alignment clamps only trinkets that don't want
		// to grow (separators, text inputs vs. buttons, etc.).
		if policy.Vertical == core.SizeExpanding {
			return bounds
		}

		// Height-for-width trinkets flow within their allocated width;
		// align them using their real height, not the hint.
		height := hint.Height
		if hasHeightForWidth(item.Trinket) {
			height = itemHeightForWidth(item.Trinket, bounds.Width)
		}

		// Vertical alignment in horizontal layout. Only AlignFill stretches
		// the child to the row; every other value (including the default,
		// unset alignment) keeps the child's natural height, so a one-row
		// text input beside a taller button stays one row instead of growing
		// to the button's height. The default centers it in the row.
		switch item.Align {
		case core.AlignFill:
			// Fill available space - no adjustment needed
		case core.AlignTop:
			bounds.Height = height
		case core.AlignBottom:
			if height < bounds.Height {
				bounds.Y += bounds.Height - height
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
				if ch := core.FindEffectiveCellMetrics(item.Trinket).CellHeight; ch > 0 {
					off = (off / ch) * ch
				}
				bounds.Y += off
				bounds.Height = height
			}
		}
	} else {
		// Cross-axis Expanding fills the allocation (see above).
		if policy.Horizontal == core.SizeExpanding {
			return bounds
		}

		// Height-for-width trinkets must receive their allocated width —
		// clamping them to the (unwrapped) hint width would defeat
		// wrapping entirely. Alignment keeps its vertical meaning only.
		if hasHeightForWidth(item.Trinket) {
			return bounds
		}

		// Horizontal alignment in vertical layout
		switch item.Align {
		case core.AlignFill:
			// Fill available space - no adjustment needed
		case core.AlignLeft:
			bounds.Width = hint.Width
		case core.AlignCenter:
			if hint.Width < bounds.Width {
				// Grid-snap the offset (see the vertical-centering note) so a
				// cell surface draws and hit-tests the child in the same column.
				off := (bounds.Width - hint.Width) / 2
				if cw := core.FindEffectiveCellMetrics(item.Trinket).CellWidth; cw > 0 {
					off = (off / cw) * cw
				}
				bounds.X += off
				bounds.Width = hint.Width
			}
		case core.AlignRight:
			if hint.Width < bounds.Width {
				bounds.X += bounds.Width - hint.Width
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
		hint := item.Trinket.SizeHint()
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
		contentWidth -= metrics.CellWidth * 2
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

// inlineSpacingForItems computes the extra horizontal spacing inline
// trinkets receive in a horizontal layout (mirrors Layout).
func (l *BoxLayout) inlineSpacingForItems(metrics core.CellMetrics) core.Unit {
	var total core.Unit
	if len(l.items) == 0 {
		return 0
	}
	if isInlineTrinket(l.items[0].Trinket) {
		total += metrics.CellWidth
	}
	for i := 0; i < len(l.items)-1; i++ {
		if isInlineTrinket(l.items[i].Trinket) || isInlineTrinket(l.items[i+1].Trinket) {
			total += metrics.CellWidth
		}
	}
	if isInlineTrinket(l.items[len(l.items)-1].Trinket) {
		total += metrics.CellWidth
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
		spacing := core.Unit(metrics.UnitsToCellY(l.spacing)) * metrics.CellHeight
		for i, item := range l.items {
			height += itemHeightForWidth(item.Trinket, l.verticalItemWidth(contentWidth, item, metrics))
			if i < len(l.items)-1 {
				height += spacing
			}
		}
	} else {
		spacing := core.Unit(metrics.UnitsToCellX(l.spacing)) * metrics.CellWidth
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
		hint := item.Trinket.SizeHint()

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

	// Add spacing
	if len(l.items) > 1 {
		spacing := l.spacing * core.Unit(len(l.items)-1)
		if l.orientation == core.Horizontal {
			width += spacing
		} else {
			height += spacing
		}
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
	if len(l.items) > 1 {
		spacing := l.spacing * core.Unit(len(l.items)-1)
		if l.orientation == core.Horizontal {
			width += spacing
		} else {
			height += spacing
		}
	}

	// Add margins
	width += l.margins.Horizontal()
	height += l.margins.Vertical()

	return core.UnitSize{Width: width, Height: height}
}
