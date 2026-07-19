// Package layout provides layout managers for arranging trinkets.
package layout

import (
	"github.com/phroun/kittytk/core"
)

// LayoutItem wraps a trinket with additional layout properties.
type LayoutItem struct {
	Trinket  core.Trinket
	Stretch int        // Stretch factor (0 = use preferred size)
	Align   core.Alignment
}

// NewLayoutItem creates a layout item with default properties.
func NewLayoutItem(trinket core.Trinket) *LayoutItem {
	return &LayoutItem{
		Trinket:  trinket,
		Stretch: 0,
		Align:   core.AlignLeft,
	}
}

// WithStretch sets the stretch factor.
func (i *LayoutItem) WithStretch(stretch int) *LayoutItem {
	i.Stretch = stretch
	return i
}

// WithAlign sets the alignment.
func (i *LayoutItem) WithAlign(align core.Alignment) *LayoutItem {
	i.Align = align
	return i
}

// Spacer represents fixed or stretching empty space in a layout.
type Spacer struct {
	core.TrinketBase
	fixedSize core.UnitSize
	stretch   int
}

// NewSpacer creates a fixed-size spacer.
func NewSpacer(width, height core.Unit) *Spacer {
	s := &Spacer{
		fixedSize: core.UnitSize{Width: width, Height: height},
	}
	s.SetSizePolicy(core.NewSizePolicy(core.SizeFixed, core.SizeFixed))
	return s
}

// NewStretchSpacer creates a stretching spacer.
func NewStretchSpacer() *Spacer {
	s := &Spacer{stretch: 1}
	s.SetSizePolicy(core.NewSizePolicy(core.SizeExpanding, core.SizeExpanding))
	return s
}

// SizeHint returns the preferred size.
func (s *Spacer) SizeHint() core.UnitSize {
	return s.fixedSize
}

// BaseLayout provides common layout functionality.
type BaseLayout struct {
	spacing  core.Unit
	margins  core.UnitMargins
}

// Spacing returns the spacing between items.
func (l *BaseLayout) Spacing() core.Unit {
	return l.spacing
}

// SetSpacing sets the spacing between items.
func (l *BaseLayout) SetSpacing(spacing core.Unit) {
	l.spacing = spacing
}

// ContentsMargins returns the margins around the layout.
func (l *BaseLayout) ContentsMargins() core.UnitMargins {
	return l.margins
}

// SetContentsMargins sets the margins around the layout.
func (l *BaseLayout) SetContentsMargins(margins core.UnitMargins) {
	l.margins = margins
}

// effectiveBounds returns bounds adjusted for margins.
func (l *BaseLayout) effectiveBounds(bounds core.UnitRect) core.UnitRect {
	return core.UnitRect{
		X:      bounds.X + l.margins.Left,
		Y:      bounds.Y + l.margins.Top,
		Width:  bounds.Width - l.margins.Horizontal(),
		Height: bounds.Height - l.margins.Vertical(),
	}
}

// calculateStretch distributes available space among stretching items.
func calculateStretch(available core.Unit, items []stretchItem) []core.Unit {
	if len(items) == 0 {
		return nil
	}

	// Calculate total stretch and minimum sizes
	totalStretch := 0
	totalMinimum := core.Unit(0)
	for _, item := range items {
		totalStretch += item.stretch
		totalMinimum += item.minimum
	}

	// If no stretch items, distribute equally among flexible items
	if totalStretch == 0 {
		// Just use minimum sizes
		sizes := make([]core.Unit, len(items))
		for i, item := range items {
			sizes[i] = item.minimum
		}
		return sizes
	}

	// Over-committed: shrink stretch items below their minimums,
	// distributing the deficit proportionally. (Stretch items are
	// elastic in both directions; non-stretch items keep their hints.)
	// Without this, a stale or oversized hint acts as a ratchet -
	// layouts can grow an expanding item but never shrink it back.
	extra := available - totalMinimum
	if extra < 0 {
		deficit := -extra
		var stretchMinTotal core.Unit
		for _, item := range items {
			if item.stretch > 0 {
				stretchMinTotal += item.minimum
			}
		}
		sizes := make([]core.Unit, len(items))
		var taken core.Unit
		for i, item := range items {
			sizes[i] = item.minimum
			if item.stretch > 0 && stretchMinTotal > 0 {
				cut := (deficit * item.minimum) / stretchMinTotal
				if cut > sizes[i] {
					cut = sizes[i]
				}
				sizes[i] -= cut
				taken += cut
			}
		}
		// Trim any rounding remainder from stretch items that still have size.
		for i := 0; i < len(items) && taken < deficit; i++ {
			if items[i].stretch > 0 && sizes[i] > 0 {
				sizes[i]--
				taken++
			}
		}
		return sizes
	}

	// Distribute proportionally
	sizes := make([]core.Unit, len(items))
	usedStretch := 0
	usedExtra := core.Unit(0)

	for i, item := range items {
		sizes[i] = item.minimum
		if item.stretch > 0 && totalStretch > 0 {
			portion := (extra * core.Unit(item.stretch)) / core.Unit(totalStretch)
			sizes[i] += portion
			usedExtra += portion
		}
		usedStretch += item.stretch
	}

	// Distribute any remaining pixels due to rounding
	remainder := extra - usedExtra
	for i := 0; i < len(items) && remainder > 0; i++ {
		if items[i].stretch > 0 {
			sizes[i]++
			remainder--
		}
	}

	return sizes
}

type stretchItem struct {
	minimum core.Unit
	stretch int
}
