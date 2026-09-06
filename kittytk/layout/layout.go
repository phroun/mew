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

// cellSpacing is the gap between tracks, rounded DOWN to whole cells.
//
// A gap of half a cell cannot be drawn, and a gap of two and a half lands as
// two cells at one boundary and three at the next, because where a track
// starts is rounded and the gap it was given is not. Rounding the gap down
// gives every boundary the same width. The box rounds its own spacing the same
// way, so a script asking two managers for the same spacing gets it.
func (l *BaseLayout) cellSpacing(q core.Unit) core.Unit {
	if q <= 1 {
		return l.spacing
	}
	return (l.spacing / q) * q
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

// cellQuantum is the size every track a container lays out must be a whole
// number of, or 0 where a track may be any size at all.
//
// A cell surface draws by dividing units by the cell size and hit-tests in
// units, so a track that is not a whole number of cells puts everything after
// it between cells: it draws in one and answers the mouse in another. A
// smooth surface has no such constraint.
//
// What is quantized is the distribution, so the tracks themselves land on whole
// cells. Rounding a child's origin afterwards without that would move it out of
// the space the layout gave it and into its neighbour's; with it, the only
// fraction of a cell left to round away is the one an alignment put inside a
// child's own track (see placeChild).
func cellQuantum(container core.Container, size core.Unit) core.Unit {
	w, ok := container.(core.Trinket)
	if !ok || w == nil || core.FindSmoothPositioning(w) {
		return 0
	}
	return size
}

// ceilToQuantum rounds a size up to a whole number of q. An extent ceils: a
// track a fraction of a cell wide still needs the whole cell to draw in.
func ceilToQuantum(v, q core.Unit) core.Unit {
	if q <= 1 {
		return v
	}
	return floorToQuantum(v+q-1, q)
}

// floorToQuantum rounds a position down to a whole number of q. A position
// floors: the cell a child starts in is the one that holds its first column,
// however far into that cell the arithmetic put it.
func floorToQuantum(v, q core.Unit) core.Unit {
	if q <= 1 {
		return v
	}
	r := v % q
	if r < 0 {
		r += q
	}
	return v - r
}

// placeChild hands a child its bounds with its origin on a whole cell.
//
// Drawing divides units by the cell size and hit-testing does not, so a child
// standing a fraction of a cell in draws in one cell and answers the mouse in
// another -- a button whose clicks are a column out. The tracks a layout
// divides its room into are already whole cells (see quantizeSizes), so the
// fraction taken off here is the one an alignment put there: centring a child
// in a track wider than it is. Taking it off can only move the child back
// towards the start of the track it was given.
func placeChild(container core.Container, w core.Trinket, bounds core.UnitRect, metrics core.CellMetrics) {
	bounds.X = floorToQuantum(bounds.X, cellQuantum(container, metrics.UnitsPerCellWidth))
	bounds.Y = floorToQuantum(bounds.Y, cellQuantum(container, metrics.UnitsPerCellHeight))
	w.SetBounds(bounds)
}

// mirrorX reflects a child's horizontal slot within the room its layout is
// dividing, which is how a run laid out from the left reads from the right.
//
// The children keep their order and the sizes they were given: a child standing
// d units in from the room's leading edge stands d units in from its trailing
// one. Everything the main-axis pass settled reflects with it -- where justify
// packed the run, which way a reversed flex walked its line, the column of air
// an inline trinket keeps -- so each of those is stated against the direction
// without being asked about it a second time.
//
// Reflection is the one thing in this package that SUBTRACTS a position, so a
// room whose own trailing edge is not on a cell -- margins are units and need
// not be whole cells -- reflects its children off the cells by that remainder.
// What puts them back is placeChild, which floors every origin it is handed;
// this is the operation that gives it something to do on the horizontal axis.
func mirrorX(room, bounds core.UnitRect) core.UnitRect {
	bounds.X = room.X*2 + room.Width - bounds.X - bounds.Width
	return bounds
}

// quantizeSizes rounds each size to a whole number of q and keeps the total
// within available.
//
// An extent CEILS, so a track a fraction of a cell over still gets the whole
// cell it needs to draw in. Rounding up can overrun the room, and what is
// taken back is only ever the fraction of a cell that rounding up added: a
// room too small for the sizes it was handed stays too small, because
// quantizing settles where the cell boundaries fall and not who gets clipped.
func quantizeSizes(sizes []core.Unit, available, q core.Unit) {
	if q <= 1 || len(sizes) == 0 {
		return
	}
	total, floor := core.Unit(0), make([]core.Unit, len(sizes))
	for i := range sizes {
		floor[i] = (sizes[i] / q) * q
		sizes[i] = ceilToQuantum(sizes[i], q)
		total += sizes[i]
	}

	// Given back from the end of the run, so what was asked for first is what
	// keeps the cell it was rounded up to.
	for total > available {
		took := false
		for i := len(sizes) - 1; i >= 0 && total > available; i-- {
			if sizes[i] <= floor[i] {
				continue
			}
			sizes[i] -= q
			total -= q
			took = true
		}
		if !took {
			break
		}
	}
}

// calculateStretch distributes available space among stretching items. Every
// size it returns is a whole number of q where the surface places on cells
// (see cellQuantum); q of 0 leaves them exactly as the arithmetic fell.
func calculateStretch(available core.Unit, items []stretchItem, q core.Unit) []core.Unit {
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
		quantizeSizes(sizes, available, q)
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
		quantizeSizes(sizes, available, q)
		return sizes
	}

	sizes := make([]core.Unit, len(items))
	for i, item := range items {
		sizes[i] = item.minimum
	}
	growByStretch(sizes, items, extra)
	quantizeSizes(sizes, available, q)
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
