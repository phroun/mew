package ifitfits

// climbTiling rises past levels where a size/mode op would be inert — a tab inside
// a stack (tabs share the body) or a sole child (no siblings to negotiate against)
// — to the first ancestor that actually tiles.
func climbTiling(n *node) *node {
	for n != nil && n.parent != nil && (stacked(n.parent) || len(n.parent.children) == 1) {
		n = n.parent
	}
	return n
}

func (v *Viewport) negFrom(tile Handle) *node {
	n := v.tile(tile)
	if n == nil {
		return nil
	}
	return climbTiling(n)
}

// ---- mode / pin state machine ----

func groupHasZoom(g *node) bool {
	if g == nil {
		return false
	}
	for _, c := range g.children {
		if c.mode == Zoom {
			return true
		}
	}
	return false
}

func enterZoom(n *node) {
	if g := n.parent; g != nil {
		for _, c := range g.children {
			if c.pin.has && c.pin.enforced {
				c.pin.enforced = false
			}
		}
	}
	n.mode = Zoom
}

func restoreInactive(g *node) {
	for _, c := range g.children {
		if c.pin.has && !c.pin.enforced && c.mode == Normal {
			c.pin.enforced = true
		}
	}
}

func setMode(n *node, m Mode) {
	if m == Zoom {
		enterZoom(n)
		return
	}
	wasZoom := n.mode == Zoom
	n.mode = m
	if m == Shrink && n.pin.has {
		n.pin.enforced = false
	}
	if m == Normal && n.pin.has && !groupHasZoom(n.parent) {
		n.pin.enforced = true
	}
	if wasZoom && n.parent != nil && !groupHasZoom(n.parent) {
		restoreInactive(n.parent)
	}
}

func cycleModeVal(cur Mode, dir int) Mode {
	return Mode((int(cur) + dir + 3) % 3)
}

func (v *Viewport) manualPin(n *node, size float64) {
	wasZoom := n.mode == Zoom
	minMain := n.mMinH
	if n.parent != nil && isH(n.parent.orient) {
		minMain = n.mMinW
	}
	if minMain <= 0 {
		minMain = 10
	}
	if minMain > size {
		size = minMain
	}
	n.pin = pinState{amount: size, has: true, enforced: true}
	n.mode = Normal
	if wasZoom && n.parent != nil && !groupHasZoom(n.parent) {
		restoreInactive(n.parent)
	}
	v.touch()
}

func clearPin(n *node) { n.pin = pinState{} }

// ---- commands ----

// Zoom sets or toggles zoom mode on the negotiation target.
func (v *Viewport) Zoom(tile Handle, state State) { v.modeToggle(tile, Zoom, state) }

// Shrink sets or toggles shrink mode.
func (v *Viewport) Shrink(tile Handle, state State) { v.modeToggle(tile, Shrink, state) }

// Normal clears zoom/shrink.
func (v *Viewport) Normal(tile Handle) {
	if t := v.negFrom(tile); t != nil {
		setMode(t, Normal)
		v.touch()
	}
}

// ModeNext cycles normal → zoom → shrink.
func (v *Viewport) ModeNext(tile Handle) {
	if t := v.negFrom(tile); t != nil {
		setMode(t, cycleModeVal(t.mode, 1))
		v.touch()
	}
}

// ModePrior cycles normal → shrink → zoom.
func (v *Viewport) ModePrior(tile Handle) {
	if t := v.negFrom(tile); t != nil {
		setMode(t, cycleModeVal(t.mode, -1))
		v.touch()
	}
}

func (v *Viewport) modeToggle(tile Handle, m Mode, state State) {
	t := v.negFrom(tile)
	if t == nil {
		return
	}
	target := Normal
	switch state {
	case On:
		target = m
	case Off:
		target = Normal
	default:
		if t.mode != m {
			target = m
		}
	}
	setMode(t, target)
	v.touch()
}

// Expand grows the resolved main-axis size by delta (via a pin).
func (v *Viewport) Expand(tile Handle, delta float64) { v.resizeBy(tile, delta) }

// Contract shrinks by delta (= negative expand).
func (v *Viewport) Contract(tile Handle, delta float64) { v.resizeBy(tile, -delta) }

func (v *Viewport) resizeBy(tile Handle, delta float64) {
	t := v.negFrom(tile)
	if t == nil {
		return
	}
	v.ensure()
	if t.hidden {
		return
	}
	cur := t.rect.H
	if t.parent != nil && isH(t.parent.orient) {
		cur = t.rect.W
	}
	v.manualPin(t, cur+delta)
}

// Resize pins the negotiation target at exactly size; size <= 0 unpins.
func (v *Viewport) Resize(tile Handle, size float64) {
	t := v.negFrom(tile)
	if t == nil {
		return
	}
	if size <= 0 {
		clearPin(t)
		v.touch()
		return
	}
	v.ensure()
	v.manualPin(t, size)
}

// Equalize throws out the group's pins and modes and pins each child to an equal
// share of the axis (ignoring naturals). recursive descends the whole subtree.
func (v *Viewport) Equalize(tile Handle, recursive bool) {
	t := v.negFrom(tile)
	if t == nil {
		return
	}
	v.ensure()
	v.eachTiling(baseGroup(t), recursive, v.equalizeGroup)
	v.touch()
}

// Balance clears the group's pins only; naturals + priority re-derive the sizes.
func (v *Viewport) Balance(tile Handle, recursive bool) {
	t := v.negFrom(tile)
	if t == nil {
		return
	}
	v.eachTiling(baseGroup(t), recursive, balanceGroup)
	v.touch()
}

func baseGroup(t *node) *node {
	if t.parent != nil {
		return t.parent
	}
	return t
}

func (v *Viewport) equalizeGroup(g *node) {
	if g.kind != groupKind || stacked(g) || len(g.children) == 0 {
		return
	}
	main := g.rect.H
	if isH(g.orient) {
		main = g.rect.W
	}
	share := main / float64(len(g.children))
	for _, c := range g.children {
		c.mode = Normal
		c.pin = pinState{amount: share, has: true, enforced: true}
	}
}

func balanceGroup(g *node) {
	if g.kind != groupKind || stacked(g) {
		return
	}
	for _, c := range g.children {
		c.pin = pinState{}
	}
}

func (v *Viewport) eachTiling(g *node, recursive bool, fn func(*node)) {
	fn(g)
	if !recursive {
		return
	}
	var kids []*node
	if stacked(g) {
		if sc := selectedChild(g); sc != nil {
			kids = []*node{sc}
		}
	} else {
		kids = g.children
	}
	for _, c := range kids {
		if c.kind == groupKind {
			v.eachTiling(c, recursive, fn)
		}
	}
}
