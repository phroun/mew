// Package layout provides layout managers for arranging trinkets.
package layout

import (
	"github.com/phroun/kittytk/core"
)

// FlexDirection specifies the main axis direction.
type FlexDirection int

const (
	FlexRow FlexDirection = iota
	FlexRowReverse
	FlexColumn
	FlexColumnReverse
)

// FlexWrap specifies whether items wrap to new lines.
type FlexWrap int

const (
	FlexNoWrap FlexWrap = iota
	FlexWrapNormal
	FlexWrapReverse
)

// FlexAlign specifies alignment along the cross axis.
type FlexAlign int

const (
	FlexAlignStretch FlexAlign = iota
	FlexAlignStart
	FlexAlignEnd
	FlexAlignCenter
	FlexAlignBaseline
)

// FlexJustify specifies content distribution along the main axis.
type FlexJustify int

const (
	FlexJustifyStart FlexJustify = iota
	FlexJustifyEnd
	FlexJustifyCenter
	FlexJustifySpaceBetween
	FlexJustifySpaceAround
	FlexJustifySpaceEvenly
)

// FlexItem represents a trinket with flex properties.
type FlexItem struct {
	Trinket   core.Trinket
	Grow      float64   // Flex grow factor
	Shrink    float64   // Flex shrink factor
	Basis     core.Unit // Base size (0 = auto)
	AlignSelf FlexAlign // Override container alignment
}

// FlexLayout arranges trinkets using flexbox-like semantics.
// This is similar to CSS Flexbox.
type FlexLayout struct {
	BaseLayout
	direction    FlexDirection
	wrap         FlexWrap
	justify      FlexJustify
	alignItems   FlexAlign
	alignContent FlexAlign
	items        []*FlexItem
}

// NewFlexLayout creates a new flex layout.
func NewFlexLayout() *FlexLayout {
	return &FlexLayout{
		direction:    FlexRow,
		wrap:         FlexNoWrap,
		justify:      FlexJustifyStart,
		alignItems:   FlexAlignStretch,
		alignContent: FlexAlignStretch,
	}
}

// Direction returns the flex direction.
func (l *FlexLayout) Direction() FlexDirection {
	return l.direction
}

// SetDirection sets the flex direction.
func (l *FlexLayout) SetDirection(dir FlexDirection) {
	l.direction = dir
}

// Wrap returns the wrap mode.
func (l *FlexLayout) Wrap() FlexWrap {
	return l.wrap
}

// SetWrap sets the wrap mode.
func (l *FlexLayout) SetWrap(wrap FlexWrap) {
	l.wrap = wrap
}

// Justify returns the justify content mode.
func (l *FlexLayout) Justify() FlexJustify {
	return l.justify
}

// SetJustify sets the justify content mode.
func (l *FlexLayout) SetJustify(justify FlexJustify) {
	l.justify = justify
}

// AlignItems returns the align items mode.
func (l *FlexLayout) AlignItems() FlexAlign {
	return l.alignItems
}

// SetAlignItems sets the align items mode.
func (l *FlexLayout) SetAlignItems(align FlexAlign) {
	l.alignItems = align
}

// AddTrinket adds a trinket with default flex properties.
func (l *FlexLayout) AddTrinket(trinket core.Trinket) {
	l.items = append(l.items, &FlexItem{
		Trinket: trinket,
		Grow:    0,
		Shrink:  1,
		Basis:   0,
	})
}

// AddTrinketWithFlex adds a trinket with flex properties.
func (l *FlexLayout) AddTrinketWithFlex(trinket core.Trinket, grow, shrink float64, basis core.Unit) {
	l.items = append(l.items, &FlexItem{
		Trinket: trinket,
		Grow:    grow,
		Shrink:  shrink,
		Basis:   basis,
	})
}

// isMainHorizontal returns true if the main axis is horizontal.
func (l *FlexLayout) isMainHorizontal() bool {
	return l.direction == FlexRow || l.direction == FlexRowReverse
}

// isReversed returns true if items are laid out in reverse.
func (l *FlexLayout) isReversed() bool {
	return l.direction == FlexRowReverse || l.direction == FlexColumnReverse
}

// Layout arranges children within the given bounds.
func (l *FlexLayout) Layout(container core.Container, bounds core.UnitRect) {
	if len(l.items) == 0 {
		return
	}

	rect := l.effectiveBounds(bounds)

	// Get main and cross axis sizes
	var mainSize, crossSize core.Unit
	if l.isMainHorizontal() {
		mainSize = rect.Width
		crossSize = rect.Height
	} else {
		mainSize = rect.Height
		crossSize = rect.Width
	}

	// Calculate base sizes
	baseSizes := make([]core.Unit, len(l.items))
	totalBase := core.Unit(0)
	totalGrow := float64(0)
	totalShrink := float64(0)

	for i, item := range l.items {
		hint := item.Trinket.SizeHint()
		var base core.Unit

		if item.Basis > 0 {
			base = item.Basis
		} else if l.isMainHorizontal() {
			base = hint.Width
		} else {
			base = hint.Height
		}

		baseSizes[i] = base
		totalBase += base
		totalGrow += item.Grow
		totalShrink += item.Shrink
	}

	// Add spacing
	totalSpacing := l.spacing * core.Unit(len(l.items)-1)
	totalBase += totalSpacing

	// Calculate final sizes
	finalSizes := make([]core.Unit, len(l.items))
	copy(finalSizes, baseSizes)

	freeSpace := mainSize - totalBase
	if freeSpace > 0 && totalGrow > 0 {
		// Distribute extra space according to grow factors
		for i, item := range l.items {
			if item.Grow > 0 {
				extra := core.Unit(float64(freeSpace) * item.Grow / totalGrow)
				finalSizes[i] += extra
			}
		}
	} else if freeSpace < 0 && totalShrink > 0 {
		// Shrink items according to shrink factors
		deficit := -freeSpace
		for i, item := range l.items {
			if item.Shrink > 0 {
				shrink := core.Unit(float64(deficit) * item.Shrink / totalShrink)
				if shrink > finalSizes[i] {
					shrink = finalSizes[i]
				}
				finalSizes[i] -= shrink
			}
		}
	}

	// Calculate positions
	positions := l.calculatePositions(mainSize, finalSizes, totalSpacing)

	// Position trinkets
	for i, item := range l.items {
		var itemBounds core.UnitRect

		// Handle reversed order
		idx := i
		if l.isReversed() {
			idx = len(l.items) - 1 - i
		}

		if l.isMainHorizontal() {
			itemBounds = core.UnitRect{
				X:      rect.X + positions[idx],
				Y:      rect.Y,
				Width:  finalSizes[idx],
				Height: crossSize,
			}
		} else {
			itemBounds = core.UnitRect{
				X:      rect.X,
				Y:      rect.Y + positions[idx],
				Width:  crossSize,
				Height: finalSizes[idx],
			}
		}

		// Apply cross-axis alignment
		itemBounds = l.alignCross(item, itemBounds)
		item.Trinket.SetBounds(itemBounds)
	}
}

// calculatePositions calculates main-axis positions based on justify mode.
func (l *FlexLayout) calculatePositions(mainSize core.Unit, sizes []core.Unit, spacing core.Unit) []core.Unit {
	n := len(sizes)
	positions := make([]core.Unit, n)

	// Calculate total content size
	totalContent := core.Unit(0)
	for _, s := range sizes {
		totalContent += s
	}

	freeSpace := mainSize - totalContent - spacing

	switch l.justify {
	case FlexJustifyStart:
		pos := core.Unit(0)
		for i := range sizes {
			positions[i] = pos
			pos += sizes[i] + l.spacing
		}

	case FlexJustifyEnd:
		pos := freeSpace
		for i := range sizes {
			positions[i] = pos
			pos += sizes[i] + l.spacing
		}

	case FlexJustifyCenter:
		pos := freeSpace / 2
		for i := range sizes {
			positions[i] = pos
			pos += sizes[i] + l.spacing
		}

	case FlexJustifySpaceBetween:
		if n <= 1 {
			positions[0] = 0
		} else {
			gap := freeSpace / core.Unit(n-1)
			pos := core.Unit(0)
			for i := range sizes {
				positions[i] = pos
				pos += sizes[i] + gap
			}
		}

	case FlexJustifySpaceAround:
		gap := freeSpace / core.Unit(n)
		pos := gap / 2
		for i := range sizes {
			positions[i] = pos
			pos += sizes[i] + gap
		}

	case FlexJustifySpaceEvenly:
		gap := freeSpace / core.Unit(n+1)
		pos := gap
		for i := range sizes {
			positions[i] = pos
			pos += sizes[i] + gap
		}
	}

	return positions
}

// alignCross applies cross-axis alignment to an item.
func (l *FlexLayout) alignCross(item *FlexItem, bounds core.UnitRect) core.UnitRect {
	align := l.alignItems
	if item.AlignSelf != FlexAlignStretch {
		align = item.AlignSelf
	}

	hint := item.Trinket.SizeHint()
	var itemCross, boundsCross core.Unit
	if l.isMainHorizontal() {
		itemCross = hint.Height
		boundsCross = bounds.Height
	} else {
		itemCross = hint.Width
		boundsCross = bounds.Width
	}

	switch align {
	case FlexAlignStart:
		if l.isMainHorizontal() {
			bounds.Height = itemCross
		} else {
			bounds.Width = itemCross
		}

	case FlexAlignEnd:
		if l.isMainHorizontal() {
			bounds.Y += boundsCross - itemCross
			bounds.Height = itemCross
		} else {
			bounds.X += boundsCross - itemCross
			bounds.Width = itemCross
		}

	case FlexAlignCenter:
		if l.isMainHorizontal() {
			bounds.Y += (boundsCross - itemCross) / 2
			bounds.Height = itemCross
		} else {
			bounds.X += (boundsCross - itemCross) / 2
			bounds.Width = itemCross
		}

	case FlexAlignStretch:
		// Use full cross size (default)
	}

	return bounds
}

// SizeHint returns the preferred size for the container.
func (l *FlexLayout) SizeHint(container core.Container) core.UnitSize {
	var mainTotal, crossMax core.Unit

	for _, item := range l.items {
		hint := item.Trinket.SizeHint()
		var main, cross core.Unit

		if l.isMainHorizontal() {
			main = hint.Width
			cross = hint.Height
		} else {
			main = hint.Height
			cross = hint.Width
		}

		if item.Basis > 0 {
			main = item.Basis
		}

		mainTotal += main
		if cross > crossMax {
			crossMax = cross
		}
	}

	// Add spacing
	if len(l.items) > 1 {
		mainTotal += l.spacing * core.Unit(len(l.items)-1)
	}

	// Add margins
	var width, height core.Unit
	if l.isMainHorizontal() {
		width = mainTotal + l.margins.Horizontal()
		height = crossMax + l.margins.Vertical()
	} else {
		width = crossMax + l.margins.Horizontal()
		height = mainTotal + l.margins.Vertical()
	}

	return core.UnitSize{Width: width, Height: height}
}

// MinimumSize returns the minimum size for the container.
func (l *FlexLayout) MinimumSize(container core.Container) core.UnitSize {
	var mainTotal, crossMax core.Unit

	for _, item := range l.items {
		minSize := item.Trinket.MinimumSize()
		var main, cross core.Unit

		if l.isMainHorizontal() {
			main = minSize.Width
			cross = minSize.Height
		} else {
			main = minSize.Height
			cross = minSize.Width
		}

		mainTotal += main
		if cross > crossMax {
			crossMax = cross
		}
	}

	// Add spacing
	if len(l.items) > 1 {
		mainTotal += l.spacing * core.Unit(len(l.items)-1)
	}

	// Add margins
	var width, height core.Unit
	if l.isMainHorizontal() {
		width = mainTotal + l.margins.Horizontal()
		height = crossMax + l.margins.Vertical()
	} else {
		width = crossMax + l.margins.Horizontal()
		height = mainTotal + l.margins.Vertical()
	}

	return core.UnitSize{Width: width, Height: height}
}
