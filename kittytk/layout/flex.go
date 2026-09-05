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
//
// Cross-axis placement is the child's own alignment -- the same halign/valign/
// fill every other manager reads -- and AlignSet says whether the child stated
// one. A child that did not takes the container's AlignItems.
type FlexItem struct {
	Trinket  core.Trinket
	Grow     float64   // Flex grow factor
	Shrink   float64   // Flex shrink factor
	Basis    core.Unit // Base size (0 = auto)
	Align    core.Alignment
	AlignSet bool
}

// FlexLayout arranges trinkets using flexbox-like semantics.
// This is similar to CSS Flexbox.
type FlexLayout struct {
	BaseLayout
	direction  FlexDirection
	wrap       FlexWrap
	justify    FlexJustify
	alignItems FlexAlign
	items      []*FlexItem
}

// NewFlexLayout creates a new flex layout.
func NewFlexLayout() *FlexLayout {
	return &FlexLayout{
		direction:  FlexRow,
		wrap:       FlexNoWrap,
		justify:    FlexJustifyStart,
		alignItems: FlexAlignStretch,
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

// AddTrinket adds a trinket, honoring the flex hints and the alignment that
// travel with the child.
func (l *FlexLayout) AddTrinket(trinket core.Trinket) {
	item := &FlexItem{Trinket: trinket, Grow: 0, Shrink: 1, Basis: 0}
	if h, ok := trinket.(interface {
		LayoutFlex() (core.FlexHints, bool)
	}); ok {
		if f, set := h.LayoutFlex(); set {
			item.Grow, item.Basis = f.Grow, f.Basis
			if f.ShrinkSet {
				item.Shrink = f.Shrink
			}
		}
	}
	if h, ok := trinket.(interface {
		LayoutAlignment() (core.Alignment, bool)
	}); ok {
		item.Align, item.AlignSet = h.LayoutAlignment()
	}
	l.items = append(l.items, item)
}

// AddTrinketWithFlex adds a trinket with flex properties given outright.
func (l *FlexLayout) AddTrinketWithFlex(trinket core.Trinket, grow, shrink float64, basis core.Unit) {
	l.AddTrinket(trinket)
	item := l.items[len(l.items)-1]
	item.Grow, item.Shrink, item.Basis = grow, shrink, basis
}

// Count returns the number of items.
func (l *FlexLayout) Count() int { return len(l.items) }

// ItemAt returns the item at the given index, or nil.
func (l *FlexLayout) ItemAt(index int) *FlexItem {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	return l.items[index]
}

// RemoveTrinket removes a trinket from the layout.
func (l *FlexLayout) RemoveTrinket(trinket core.Trinket) {
	for i, item := range l.items {
		if item.Trinket == trinket {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return
		}
	}
}

// isMainHorizontal returns true if the main axis is horizontal.
func (l *FlexLayout) isMainHorizontal() bool {
	return l.direction == FlexRow || l.direction == FlexRowReverse
}

// isReversed returns true if items are laid out in reverse.
func (l *FlexLayout) isReversed() bool {
	return l.direction == FlexRowReverse || l.direction == FlexColumnReverse
}

// flexLine is one run of items that fit along the main axis together. Without
// wrapping there is exactly one, holding everything.
type flexLine struct {
	first, last int         // half-open range of items
	sizes       []core.Unit // resolved main sizes, parallel to that range
	cross       core.Unit   // how deep the line is across
	crossPos    core.Unit   // where it starts across
}

// mainSize and crossSize split a size along the layout's axes.
func (l *FlexLayout) mainCross(w, h core.Unit) (main, cross core.Unit) {
	if l.isMainHorizontal() {
		return w, h
	}
	return h, w
}

// baseSize is what an item starts at along the main axis: its basis when it
// states one, else its size hint.
func (l *FlexLayout) baseSize(item *FlexItem) core.Unit {
	if item.Basis > 0 {
		return item.Basis
	}
	main, _ := l.mainCross(itemSize(item.Trinket).Width, itemSize(item.Trinket).Height)
	return main
}

// minMain is the smallest an item may be squeezed to along the main axis.
// Shrinking past it is what a minimum exists to prevent.
func (l *FlexLayout) minMain(item *FlexItem) core.Unit {
	min := item.Trinket.MinimumSize()
	main, _ := l.mainCross(min.Width, min.Height)
	if main < 0 {
		return 0
	}
	return main
}

// breakIntoLines fills lines up to mainSize, starting a new one when the next
// item will not fit. Every line holds at least one item, however small the box:
// an item that fits nowhere still has to go somewhere.
func (l *FlexLayout) breakIntoLines(base []core.Unit, mainSize core.Unit) []flexLine {
	if l.wrap == FlexNoWrap {
		return []flexLine{{first: 0, last: len(l.items)}}
	}

	var lines []flexLine
	first := 0
	used := core.Unit(0)
	for i := range l.items {
		next := base[i]
		if i > first {
			next += l.spacing
		}
		if i > first && used+next > mainSize {
			lines = append(lines, flexLine{first: first, last: i})
			first, used = i, base[i]
			continue
		}
		used += next
	}
	return append(lines, flexLine{first: first, last: len(l.items)})
}

// resolveMain divides the line's main axis: grow shares out what is left over,
// shrink shares out what is missing, and neither takes an item below its own
// minimum.
func (l *FlexLayout) resolveMain(line *flexLine, base []core.Unit, mainSize core.Unit) {
	n := line.last - line.first
	line.sizes = make([]core.Unit, n)
	copy(line.sizes, base[line.first:line.last])

	total := core.Unit(0)
	var totalGrow, totalShrink float64
	for i := line.first; i < line.last; i++ {
		total += base[i]
		totalGrow += l.items[i].Grow
		totalShrink += l.items[i].Shrink
	}
	if n > 1 {
		total += l.spacing * core.Unit(n-1)
	}

	free := mainSize - total
	switch {
	case free > 0 && totalGrow > 0:
		for i := line.first; i < line.last; i++ {
			if g := l.items[i].Grow; g > 0 {
				line.sizes[i-line.first] += core.Unit(float64(free) * g / totalGrow)
			}
		}
	case free < 0 && totalShrink > 0:
		deficit := -free
		for i := line.first; i < line.last; i++ {
			item := l.items[i]
			if item.Shrink <= 0 {
				continue
			}
			take := core.Unit(float64(deficit) * item.Shrink / totalShrink)
			floor := l.minMain(item)
			if got := line.sizes[i-line.first] - take; got < floor {
				take = line.sizes[i-line.first] - floor
			}
			if take < 0 {
				take = 0
			}
			line.sizes[i-line.first] -= take
		}
	}
}

// lineCross is how deep a line is: the deepest thing in it.
func (l *FlexLayout) lineCross(line flexLine) core.Unit {
	deepest := core.Unit(0)
	for i := line.first; i < line.last; i++ {
		hint := itemSize(l.items[i].Trinket)
		_, cross := l.mainCross(hint.Width, hint.Height)
		if cross > deepest {
			deepest = cross
		}
	}
	return deepest
}

// Layout arranges children within the given bounds.
func (l *FlexLayout) Layout(container core.Container, bounds core.UnitRect) {
	if len(l.items) == 0 {
		return
	}

	rect := l.effectiveBounds(bounds)
	mainSize, crossSize := l.mainCross(rect.Width, rect.Height)

	base := make([]core.Unit, len(l.items))
	for i, item := range l.items {
		base[i] = l.baseSize(item)
	}

	lines := l.breakIntoLines(base, mainSize)
	for i := range lines {
		l.resolveMain(&lines[i], base, mainSize)
		lines[i].cross = l.lineCross(lines[i])
	}

	// One line takes the whole depth, so a stretched item fills the box. Two or
	// more are packed at their own depths, from the far edge when the wrap runs
	// backwards, and whatever depth is left over is left over.
	if len(lines) == 1 {
		lines[0].cross = crossSize
	}
	crossPos := core.Unit(0)
	if l.wrap == FlexWrapReverse && len(lines) > 1 {
		used := l.spacing * core.Unit(len(lines)-1)
		for _, line := range lines {
			used += line.cross
		}
		if crossPos = crossSize - used; crossPos < 0 {
			crossPos = 0
		}
	}
	for i := range lines {
		lines[i].crossPos = crossPos
		crossPos += lines[i].cross + l.spacing
	}

	layoutDir := core.DirLTR
	if w, ok := container.(core.Trinket); ok && w != nil {
		layoutDir = core.FindEffectiveDirection(w)
	}

	for _, line := range lines {
		n := line.last - line.first
		spacing := core.Unit(0)
		if n > 1 {
			spacing = l.spacing * core.Unit(n-1)
		}
		positions := l.calculatePositions(mainSize, line.sizes, spacing)

		for k := 0; k < n; k++ {
			// A reversed direction walks the line backwards; the positions
			// themselves are unchanged, so the last item takes the first slot.
			at := k
			if l.isReversed() {
				at = n - 1 - k
			}
			item := l.items[line.first+at]
			size := line.sizes[k]

			var itemBounds core.UnitRect
			if l.isMainHorizontal() {
				itemBounds = core.UnitRect{
					X: rect.X + positions[k], Y: rect.Y + line.crossPos,
					Width: size, Height: line.cross,
				}
			} else {
				itemBounds = core.UnitRect{
					X: rect.X + line.crossPos, Y: rect.Y + positions[k],
					Width: line.cross, Height: size,
				}
			}
			item.Trinket.SetBounds(l.alignCross(item, itemBounds, layoutDir))
		}
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

// alignCross places an item across its line.
//
// The child's own alignment decides when it states one -- the same halign,
// valign and fill every other manager reads, so a trinket is placed the same
// way wherever it is put. A child that states none takes the container's
// AlignItems, which is what that setting is for.
func (l *FlexLayout) alignCross(item *FlexItem, bounds core.UnitRect, layoutDir core.Direction) core.UnitRect {
	align := l.alignItems
	if item.AlignSet {
		align = l.alignFromChild(item, layoutDir)
	}

	hint := itemSize(item.Trinket)
	itemCross, boundsCross := hint.Height, bounds.Height
	if !l.isMainHorizontal() {
		itemCross, boundsCross = hint.Width, bounds.Width
	}

	set := func(pos, size core.Unit) core.UnitRect {
		if l.isMainHorizontal() {
			bounds.Y, bounds.Height = pos, size
		} else {
			bounds.X, bounds.Width = pos, size
		}
		return bounds
	}
	origin := bounds.Y
	if !l.isMainHorizontal() {
		origin = bounds.X
	}

	switch align {
	case FlexAlignStart:
		return set(origin, itemCross)
	case FlexAlignEnd:
		return set(origin+boundsCross-itemCross, itemCross)
	case FlexAlignCenter:
		return set(origin+(boundsCross-itemCross)/2, itemCross)
	}
	// Stretch and baseline take the line's whole depth; a baseline pass would
	// need a shared baseline to align to, which nothing reports yet.
	return bounds
}

// alignFromChild reads the child's own alignment as a cross-axis placement.
// Filling that axis is stretch; anything else is where it sits.
func (l *FlexLayout) alignFromChild(item *FlexItem, layoutDir core.Direction) FlexAlign {
	if l.isMainHorizontal() {
		if item.Align.FillV {
			return FlexAlignStretch
		}
		switch item.Align.V {
		case core.AlignTop:
			return FlexAlignStart
		case core.AlignBottom:
			return FlexAlignEnd
		}
		return FlexAlignCenter
	}
	if item.Align.FillH {
		return FlexAlignStretch
	}
	// The horizontal cross axis is the one a direction turns over, so the
	// logical alignments are spent here rather than read as sides.
	switch core.ResolveHAlign(item.Align.H, core.FindTextDirection(item.Trinket), layoutDir) {
	case core.SideLeft:
		return FlexAlignStart
	case core.SideRight:
		return FlexAlignEnd
	}
	return FlexAlignCenter
}

// SizeHint returns the preferred size for the container.
func (l *FlexLayout) SizeHint(container core.Container) core.UnitSize {
	var mainTotal, crossMax core.Unit

	for _, item := range l.items {
		hint := itemSize(item.Trinket)
		main, cross := l.mainCross(hint.Width, hint.Height)

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
