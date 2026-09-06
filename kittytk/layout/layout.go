// Package layout provides layout managers for arranging trinkets.
package layout

import (
	"github.com/phroun/kittytk/core"
)

// LayoutItem wraps a trinket with additional layout properties.
type LayoutItem struct {
	Trinket core.Trinket
	Stretch int // Stretch factor (0 = use preferred size)
	Align   core.Alignment
}

// NewLayoutItem creates a layout item with default properties.
//
// Alignment defaults to filling both axes and centring on either one that has
// nothing to fill. The item takes the whole of the box across the layout's
// cross axis; stretch, which is a separate property, decides what it gets
// ALONG the axis.
func NewLayoutItem(trinket core.Trinket) *LayoutItem {
	return &LayoutItem{
		Trinket: trinket,
		Stretch: 0,
		Align:   core.DefaultAlignment(),
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
	spacing core.Unit
	margins core.UnitMargins
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

// alignmentFor is how a child asks to be placed: what it says when it says
// anything, else what the layout was given for it when it was added.
//
// Read when the layout runs rather than when the child was added, because
// halign, valign and fill may be set on a child that is already placed --
// over the wire, a `set` on a trinket the script built earlier.
func alignmentFor(w core.Trinket, fallback core.Alignment) core.Alignment {
	if a, set := statedAlignment(w); set {
		// Field by field: a child that asked about one axis has said nothing
		// about the other, and what it did not ask about stays as it was.
		return a.Over(fallback)
	}
	return fallback
}

// stretchFor, flexFor and placementFor are the same bargain as alignmentFor
// for the hints one manager each reads: what the child says when it says
// anything, else what the layout was given for it when it was added.
//
// Every one of them is read where it is used rather than where the child was
// added, for the reason alignmentFor gives: over the wire a `set k grow=3`
// lands on a trinket an earlier build already placed, and a hint that is only
// read at add time is accepted, stored, and then read by nobody.
//
// The child's own statement wins over the stored value, which is what a Go
// caller wrote directly through AddTrinketWithStretch, AddTrinketWithFlex or
// AddTrinketAt. A child that states a hint AND is placed by one of those is
// contradicting itself, and everything the toolkit says about layout hints is
// that they travel on the child.
func stretchFor(w core.Trinket, fallback int) int {
	if h, ok := w.(interface{ LayoutStretchHint() (int, bool) }); ok {
		if s, set := h.LayoutStretchHint(); set {
			return s
		}
	}
	return fallback
}

func flexFor(w core.Trinket, fallback core.FlexHints) core.FlexHints {
	h, ok := w.(interface {
		LayoutFlex() (core.FlexHints, bool)
	})
	if !ok {
		return fallback
	}
	f, set := h.LayoutFlex()
	if !set {
		return fallback
	}
	// Shrink is the one field with a flag of its own: unstated, it keeps
	// whatever the item already had rather than dropping to zero.
	if !f.ShrinkSet {
		f.Shrink = fallback.Shrink
	}
	return f
}

func placementFor(w core.Trinket, fallback core.GridPlacement) core.GridPlacement {
	if h, ok := w.(interface {
		LayoutGridPlacement() (core.GridPlacement, bool)
	}); ok {
		if p, set := h.LayoutGridPlacement(); set {
			return p
		}
	}
	return fallback
}

// statedAlignment is the alignment a child states, and whether it states one.
func statedAlignment(w core.Trinket) (core.Alignment, bool) {
	if h, ok := w.(interface {
		LayoutAlignment() (core.Alignment, bool)
	}); ok {
		return h.LayoutAlignment()
	}
	return core.Alignment{}, false
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

	sizes := make([]core.Unit, len(items))
	for i, item := range items {
		sizes[i] = item.minimum
	}
	growByStretch(sizes, items, extra)
	return sizes
}

// growByStretch hands out extra among the items that stretch, in proportion.
//
// An item that reaches its maximum stops there and what it turned down is
// shared among the others, which is done by going round again rather than in
// one pass: the share each item gets depends on who is still growing, and that
// is only known once the ones that stopped have stopped.
//
// The loop ends because every round either clamps an item -- and there are
// finitely many -- or hands out the whole remainder and returns.
func growByStretch(sizes []core.Unit, items []stretchItem, extra core.Unit) {
	stopped := make([]bool, len(items))
	for extra > 0 {
		totalStretch := 0
		for i, item := range items {
			if item.stretch > 0 && !stopped[i] {
				totalStretch += item.stretch
			}
		}
		if totalStretch == 0 {
			return
		}

		given := core.Unit(0)
		anyStopped := false
		for i, item := range items {
			if item.stretch == 0 || stopped[i] {
				continue
			}
			portion := extra * core.Unit(item.stretch) / core.Unit(totalStretch)
			if room := roomAbove(item, sizes[i]); room >= 0 && portion >= room {
				portion = room
				stopped[i] = true
				anyStopped = true
			}
			sizes[i] += portion
			given += portion
		}
		extra -= given

		if !anyStopped {
			// Nobody stopped, so nothing will be shared out again and what
			// integer division left over goes a unit at a time to the items
			// still growing.
			for i, item := range items {
				if extra == 0 {
					return
				}
				if item.stretch > 0 && !stopped[i] && roomAbove(item, sizes[i]) != 0 {
					sizes[i]++
					extra--
				}
			}
			return
		}
	}
}

// roomAbove is how much further an item may grow, or -1 where nothing bounds
// it. A maximum below where the item already sits leaves no room at all: the
// minimum put it there, and a minimum is the stronger statement.
func roomAbove(item stretchItem, size core.Unit) core.Unit {
	if item.maximum < 0 {
		return -1
	}
	if room := item.maximum - size; room > 0 {
		return room
	}
	return 0
}

// stretchItem is one thing sharing out a run: how small it may be made, how
// far it may grow, and its weight in what is left over.
//
// A maximum of core.Unbounded does not bound. Zero is a real maximum and
// stops the item where its minimum leaves it.
type stretchItem struct {
	minimum core.Unit
	maximum core.Unit
	stretch int
}
