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
	Basis    core.Unit // Base size (core.BasisAuto = take it from the child)
	Align    core.Alignment
	AlignSet bool
}

// FlexLayout arranges trinkets using flexbox-like semantics.
// This is similar to CSS Flexbox.
type FlexLayout struct {
	BaseLayout
	direction     FlexDirection
	wrap          FlexWrap
	justify       FlexJustify
	alignItems    FlexAlign
	items         []*FlexItem
	metricsSource core.Trinket // container whose effective grid metrics apply

	// gap is what a boundary costs along the run, crossGap what one costs
	// between lines, and mainQ and crossQ the size a track on each axis has to
	// be a whole number of -- see resolveGap. All four are set at the top of
	// every entry point and read by everything below.
	gap      core.Unit
	crossGap core.Unit
	mainQ    core.Unit
	crossQ   core.Unit
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

// SetMetricsSource sets the trinket whose effective grid metrics this layout
// uses (normally the container; wired by Panel). Layouts are not trinkets, so
// they cannot walk the inheritance chain themselves.
func (l *FlexLayout) SetMetricsSource(w core.Trinket) {
	l.metricsSource = w
}

// effectiveMetrics resolves grid metrics from the given container if it is a
// trinket, else from the stored metrics source, else the defaults.
func (l *FlexLayout) effectiveMetrics(container core.Container) core.CellMetrics {
	if w, ok := container.(core.Trinket); ok && w != nil {
		return core.FindEffectiveCellMetrics(w)
	}
	if l.metricsSource != nil {
		return core.FindEffectiveCellMetrics(l.metricsSource)
	}
	return core.DefaultCellMetrics()
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
	item := &FlexItem{Trinket: trinket}
	d := core.DefaultFlexHints()
	f := flexFor(trinket, d)
	item.Grow, item.Shrink, item.Basis = f.Grow, f.Shrink, f.Basis
	if h, ok := trinket.(interface {
		LayoutAlignment() (core.Alignment, bool)
	}); ok {
		item.Align, item.AlignSet = h.LayoutAlignment()
	}
	l.items = append(l.items, item)
}

// resolveGap settles the grid this pass works on: the size a track has to be a
// whole number of along the run and between lines, and what a boundary costs
// on each. A gap of half a cell cannot be drawn and puts everything after it
// between cells, so a gap rounds down to whole cells.
//
// The two axes are asked separately because a cell is taller than it is wide,
// so the same spacing is a whole number of one and a fraction of the other.
//
// All four are settled once at the top of a pass and held on the layout,
// rather than threaded through every function that measures, breaks or places.
func (l *FlexLayout) resolveGap(container core.Container, metrics core.CellMetrics) {
	if container == nil {
		// Measuring without one: the metrics source is the container this
		// layout belongs to, and it answers the same question.
		container, _ = l.metricsSource.(core.Container)
	}
	main, cross := metrics.UnitsPerCellHeight, metrics.UnitsPerCellWidth
	if l.isMainHorizontal() {
		main, cross = cross, main
	}
	l.mainQ = cellQuantum(container, main)
	l.crossQ = cellQuantum(container, cross)
	l.gap = l.cellSpacing(l.mainQ)
	l.crossGap = l.cellSpacing(l.crossQ)
}

// refreshHints re-reads the flex hints off every child, so a grow, shrink or
// basis set on a child that is already in this layout reaches it. See flexFor.
func (l *FlexLayout) refreshHints() {
	for _, item := range l.items {
		f := flexFor(item.Trinket, core.FlexHints{
			Grow: item.Grow, Shrink: item.Shrink, Basis: item.Basis,
		})
		item.Grow, item.Shrink, item.Basis = f.Grow, f.Shrink, f.Basis
	}
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
	if item.Basis >= 0 {
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
func (l *FlexLayout) breakIntoLines(base []core.Unit, mainSize core.Unit, metrics core.CellMetrics) []flexLine {
	if l.wrap == FlexNoWrap {
		return []flexLine{{first: 0, last: len(l.items)}}
	}

	var lines []flexLine
	first := 0
	used := core.Unit(0)
	cell := core.Unit(0)
	if l.isMainHorizontal() {
		cell = metrics.UnitsPerCellWidth
	}
	for i := range l.items {
		next := base[i]
		if isInlineTrinket(l.items[i].Trinket) {
			// Its trailing bearing, and its leading one where the neighbour
			// before it did not already open one.
			next += cell
			if i == first || !isInlineTrinket(l.items[i-1].Trinket) {
				next += cell
			}
		}
		if i > first {
			next += l.gap
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

// lineBearings is the room a line's items keep for their side-bearings, along
// the main axis: a column before the first inline item, one between any two
// where either is inline, one after the last. Two that meet collapse into one,
// as they do in a box.
//
// Only where the main axis is horizontal. A column-direction run's bearings sit
// across it and are taken out of each item's own width instead.
func (l *FlexLayout) lineBearings(line flexLine, metrics core.CellMetrics) core.Unit {
	if !l.isMainHorizontal() || line.last <= line.first {
		return 0
	}
	cell := metrics.UnitsPerCellWidth
	total := core.Unit(0)
	if isInlineTrinket(l.items[line.first].Trinket) {
		total += cell
	}
	for i := line.first; i < line.last-1; i++ {
		if isInlineTrinket(l.items[i].Trinket) || isInlineTrinket(l.items[i+1].Trinket) {
			total += cell
		}
	}
	if isInlineTrinket(l.items[line.last-1].Trinket) {
		total += cell
	}
	return total
}

// resolveMain divides the line's main axis: grow shares out what is left over,
// shrink shares out what is missing, and neither takes an item below its own
// minimum.
func (l *FlexLayout) resolveMain(line *flexLine, base []core.Unit, mainSize core.Unit, metrics core.CellMetrics, q core.Unit) {
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
		total += l.gap * core.Unit(n-1)
	}
	total += l.lineBearings(*line, metrics)

	free := mainSize - total
	switch {
	case free > 0 && totalGrow > 0:
		l.growLine(line, free)
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

	// Whole cells where the surface has a grid: a line laid out in fractions
	// of a cell puts every item after the first between cells (see
	// quantizeSizes). The room the sizes have to fit is the line's, less what
	// the boundaries and the bearings take.
	room := mainSize - l.lineBearings(*line, metrics)
	if n > 1 {
		room -= l.gap * core.Unit(n-1)
	}
	quantizeSizes(line.sizes, room, q)
}

// growLine hands out a line's spare room among the items that grow, in
// proportion. An item that reaches its maximum stops there and the rest share
// what it turned down, which takes going round again: each item's share
// depends on who is still growing, and that is only known once the ones that
// stopped have stopped.
func (l *FlexLayout) growLine(line *flexLine, free core.Unit) {
	stopped := make([]bool, line.last-line.first)
	for free > 0 {
		total := 0.0
		for i := line.first; i < line.last; i++ {
			if g := l.items[i].Grow; g > 0 && !stopped[i-line.first] {
				total += g
			}
		}
		if total == 0 {
			return
		}

		given := core.Unit(0)
		anyStopped := false
		for i := line.first; i < line.last; i++ {
			k := i - line.first
			g := l.items[i].Grow
			if g <= 0 || stopped[k] {
				continue
			}
			portion := core.Unit(float64(free) * g / total)
			if max := l.maxMain(l.items[i]); max >= 0 {
				room := max - line.sizes[k]
				if room < 0 {
					room = 0
				}
				if portion >= room {
					portion = room
					stopped[k] = true
					anyStopped = true
				}
			}
			line.sizes[k] += portion
			given += portion
		}
		free -= given
		if !anyStopped {
			return
		}
	}
}

// maxMain and maxCross are the largest an item may be along each axis, and
// minCross the smallest across. A maximum of core.Unbounded does not bound.
func (l *FlexLayout) maxMain(item *FlexItem) core.Unit {
	max := item.Trinket.MaximumSize()
	main, _ := l.mainCross(max.Width, max.Height)
	return main
}

// maxCross is maxMain across the line.
func (l *FlexLayout) maxCross(item *FlexItem) core.Unit {
	max := item.Trinket.MaximumSize()
	_, cross := l.mainCross(max.Width, max.Height)
	return cross
}

// minCross is the smallest an item may be made across its line.
func (l *FlexLayout) minCross(item *FlexItem) core.Unit {
	min := item.Trinket.MinimumSize()
	_, cross := l.mainCross(min.Width, min.Height)
	return cross
}

// lineCross is how deep a line is: the deepest thing in it, taken out to the
// whole cell it needs where the surface has a grid, so the line below it
// starts on one.
func (l *FlexLayout) lineCross(line flexLine) core.Unit {
	deepest := core.Unit(0)
	for i := line.first; i < line.last; i++ {
		hint := itemSize(l.items[i].Trinket)
		_, cross := l.mainCross(hint.Width, hint.Height)
		if cross > deepest {
			deepest = cross
		}
	}
	return ceilToQuantum(deepest, l.crossQ)
}

// Layout arranges children within the given bounds.
func (l *FlexLayout) Layout(container core.Container, bounds core.UnitRect) {
	if len(l.items) == 0 {
		return
	}
	l.refreshHints()
	l.resolveGap(container, l.effectiveMetrics(container))

	rect := l.effectiveBounds(bounds)
	mainSize, crossSize := l.mainCross(rect.Width, rect.Height)

	layoutDir := core.DirLTR
	if w, ok := container.(core.Trinket); ok && w != nil {
		layoutDir = core.FindEffectiveDirection(w)
	}
	metrics := l.effectiveMetrics(container)

	base := make([]core.Unit, len(l.items))
	for i, item := range l.items {
		base[i] = l.baseSize(item)
	}

	lines := l.breakIntoLines(base, mainSize, metrics)
	for i := range lines {
		l.resolveMain(&lines[i], base, mainSize, metrics, l.mainQ)
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
		used := l.crossGap * core.Unit(len(lines)-1)
		for _, line := range lines {
			used += line.cross
		}
		if crossPos = crossSize - used; crossPos < 0 {
			crossPos = 0
		}
	}
	for i := range lines {
		lines[i].crossPos = crossPos
		crossPos += lines[i].cross + l.crossGap
	}

	for _, line := range lines {
		n := line.last - line.first
		spacing := core.Unit(0)
		if n > 1 {
			spacing = l.gap * core.Unit(n-1)
		}
		spacing += l.lineBearings(line, metrics)
		positions := l.calculatePositions(mainSize, line.sizes, spacing)

		// Bearings are added as the line is walked, so each item carries the
		// ones opened before it. The first item's own leading bearing comes
		// first, if it wants one.
		bearingBefore := core.Unit(0)
		if l.isMainHorizontal() && isInlineTrinket(l.items[line.first].Trinket) {
			bearingBefore = metrics.UnitsPerCellWidth
		}
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
					X: rect.X + positions[k] + bearingBefore, Y: rect.Y + line.crossPos,
					Width: size, Height: line.cross,
				}
				// This item's trailing bearing, and the next one's leading
				// bearing where they do not collapse into it.
				if isInlineTrinket(item.Trinket) {
					bearingBefore += metrics.UnitsPerCellWidth
				} else if k+1 < n && isInlineTrinket(l.items[line.first+k+1].Trinket) {
					bearingBefore += metrics.UnitsPerCellWidth
				}
			} else {
				itemBounds = core.UnitRect{
					X: rect.X + line.crossPos, Y: rect.Y + positions[k],
					Width: line.cross, Height: size,
				}
			}
			placed := l.alignCross(item, itemBounds, layoutDir)
			if !l.isMainHorizontal() {
				placed = insetForBearing(item.Trinket, metrics, placed)
			}
			placeChild(container, item.Trinket, placed, metrics)
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
			pos += sizes[i] + l.gap
		}

	case FlexJustifyEnd:
		pos := freeSpace
		for i := range sizes {
			positions[i] = pos
			pos += sizes[i] + l.gap
		}

	case FlexJustifyCenter:
		pos := freeSpace / 2
		for i := range sizes {
			positions[i] = pos
			pos += sizes[i] + l.gap
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
	if stated, set := statedAlignment(item.Trinket); set {
		align = l.alignFromChild(stated, item.Trinket, layoutDir)
	} else if item.AlignSet {
		align = l.alignFromChild(item.Align, item.Trinket, layoutDir)
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
	// Stretch and baseline take the line's whole depth, unless a maximum stops
	// them short -- and an item stopped short is placed in what is left over
	// exactly as one that asked not to fill. A baseline pass would need a
	// shared baseline to align to, which nothing reports yet.
	size := boundsCross
	if max := l.maxCross(item); max >= 0 && max < size {
		size = max
	}
	if m := l.minCross(item); size < m {
		size = m
	}
	if size >= boundsCross {
		return bounds
	}
	switch l.crossSide(alignmentFor(item.Trinket, item.Align), item.Trinket, layoutDir) {
	case FlexAlignStart:
		return set(origin, size)
	case FlexAlignEnd:
		return set(origin+boundsCross-size, size)
	}
	return set(origin+(boundsCross-size)/2, size)
}

// alignFromChild reads the child's own alignment as a cross-axis placement.
// Filling that axis is stretch; anything else is where it sits.
func (l *FlexLayout) alignFromChild(a core.Alignment, w core.Trinket, layoutDir core.Direction) FlexAlign {
	if l.isMainHorizontal() && a.FillV {
		return FlexAlignStretch
	}
	if !l.isMainHorizontal() && a.FillH {
		return FlexAlignStretch
	}
	return l.crossSide(a, w, layoutDir)
}

// crossSide is where a child says it sits across its line, filling aside. It
// is what places one that asked to fill and was stopped by a maximum.
func (l *FlexLayout) crossSide(a core.Alignment, w core.Trinket, layoutDir core.Direction) FlexAlign {
	if l.isMainHorizontal() {
		switch a.V {
		case core.AlignTop:
			return FlexAlignStart
		case core.AlignBottom:
			return FlexAlignEnd
		}
		return FlexAlignCenter
	}
	// The horizontal cross axis is the one a direction turns over, so the
	// logical alignments are spent here rather than read as sides.
	switch core.ResolveHAlign(a.H, core.FindTextDirection(w), layoutDir) {
	case core.SideLeft:
		return FlexAlignStart
	case core.SideRight:
		return FlexAlignEnd
	}
	return FlexAlignCenter
}

// HasHeightForWidth reports whether this layout's height depends on the width
// it is given, which a wrapping run's does: how many lines it takes is not
// known until the width is.
//
// Only along a horizontal main axis. A column that wraps would need width for
// height, and nothing in the toolkit asks a question that way round.
func (l *FlexLayout) HasHeightForWidth() bool {
	return l.wrap != FlexNoWrap && l.isMainHorizontal() && len(l.items) > 0
}

// HeightForWidth is the height the wrapped run needs at the given width: the
// lines it breaks into, stacked.
func (l *FlexLayout) HeightForWidth(width core.Unit) core.Unit {
	l.refreshHints()
	l.resolveGap(nil, l.effectiveMetrics(nil))
	if !l.HasHeightForWidth() {
		return l.SizeHint(nil).Height
	}
	mainSize := width - l.margins.Horizontal()
	base := make([]core.Unit, len(l.items))
	for i, item := range l.items {
		base[i] = l.baseSize(item)
	}

	lines := l.breakIntoLines(base, mainSize, l.effectiveMetrics(nil))
	total := l.crossGap * core.Unit(len(lines)-1)
	for _, line := range lines {
		total += l.lineCross(line)
	}
	return total + l.margins.Vertical()
}

// SizeHint returns the preferred size for the container.
func (l *FlexLayout) SizeHint(container core.Container) core.UnitSize {
	l.refreshHints()
	l.resolveGap(container, l.effectiveMetrics(container))
	var mainTotal, crossMax core.Unit

	for _, item := range l.items {
		hint := itemSize(item.Trinket)
		main, cross := l.mainCross(hint.Width, hint.Height)

		if item.Basis >= 0 {
			main = item.Basis
		}

		mainTotal += main
		if cross > crossMax {
			crossMax = cross
		}
	}

	// Add spacing
	if len(l.items) > 1 {
		mainTotal += l.gap * core.Unit(len(l.items)-1)
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
//
// A run that wraps is as narrow as its widest item, since it can always break;
// one that does not is as wide as all of them together.
func (l *FlexLayout) MinimumSize(container core.Container) core.UnitSize {
	l.refreshHints()
	l.resolveGap(container, l.effectiveMetrics(container))
	var mainTotal, crossMax core.Unit

	for _, item := range l.items {
		minSize := item.Trinket.MinimumSize()
		main, cross := l.mainCross(minSize.Width, minSize.Height)

		if l.wrap != FlexNoWrap {
			if main > mainTotal {
				mainTotal = main
			}
		} else {
			mainTotal += main
		}
		if cross > crossMax {
			crossMax = cross
		}
	}

	// Add spacing
	if len(l.items) > 1 {
		mainTotal += l.gap * core.Unit(len(l.items)-1)
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
