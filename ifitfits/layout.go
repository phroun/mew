package ifitfits

import "sort"

// stackReserve is the header chrome a stack eats on the height axis (one tab row).
func stackReserve(*node) float64 { return headerH }

// measure computes each node's minimum and natural bounding box, bottom-up. A
// stack reports the MAX of every child's box (both axes) plus header reserve, so
// its footprint is invariant to which tab is up — flipping tabs never relayouts.
func measure(n *node) {
	if n.kind == leafKind {
		n.mMinW, n.mMinH, n.mNatW, n.mNatH = n.minW, n.minH, n.natW, n.natH
		return
	}
	for _, c := range n.children {
		measure(c)
	}
	sum := func(f func(*node) float64) float64 {
		var s float64
		for _, c := range n.children {
			s += f(c)
		}
		return s
	}
	max := func(f func(*node) float64) float64 {
		m := 0.0
		for i, c := range n.children {
			if v := f(c); i == 0 || v > m {
				m = v
			}
		}
		return m
	}
	minW := func(c *node) float64 { return c.mMinW }
	minH := func(c *node) float64 { return c.mMinH }
	natW := func(c *node) float64 { return c.mNatW }
	natH := func(c *node) float64 { return c.mNatH }
	switch {
	case stacked(n):
		res := stackReserve(n)
		n.mMinW, n.mNatW = max(minW), max(natW)
		n.mMinH, n.mNatH = max(minH)+res, max(natH)+res
	case isH(n.orient):
		n.mMinW, n.mNatW = sum(minW), sum(natW)
		n.mMinH, n.mNatH = max(minH), max(natH)
	default:
		n.mMinH, n.mNatH = sum(minH), sum(natH)
		n.mMinW, n.mNatW = max(minW), max(natW)
	}
}

// computeEff is a node's effective overflow priority: a leaf's own, a group's the
// max of its children. (The engine does not protect a "focused" tile — the host
// owns focus; raise a tile's priority to keep it from being omitted.)
func computeEff(n *node) float64 {
	if n.kind == leafKind {
		n.eff = float64(n.priority)
		return n.eff
	}
	m := 0.0
	for i, c := range n.children {
		if e := computeEff(c); i == 0 || e > m {
			m = e
		}
	}
	n.eff = m
	return m
}

// markHidden hides a subtree from drawing. reason: "omit" (overflow), "tab"
// (inactive stack tab), "lens" (covered by a magnify lens).
func markHidden(n *node, reason string) {
	n.hidden = true
	n.why = reason
	for _, c := range n.children {
		markHidden(c, reason)
	}
}

// waterfill hands `pool` out evenly across items, each capped by capFn, until the
// pool runs dry or no item can take more. Returns the leftover.
func waterfill(items []*node, pool float64, capFn func(*node) float64) float64 {
	active := make([]*node, 0, len(items))
	for _, k := range items {
		if capFn(k)-k.main > 0.5 {
			active = append(active, k)
		}
	}
	for guard := 0; pool > 0.5 && len(active) > 0 && guard < 500; guard++ {
		share := pool / float64(len(active))
		prog := false
		for _, k := range active {
			give := share
			if room := capFn(k) - k.main; room < give {
				give = room
			}
			if give > 0 {
				k.main += give
				pool -= give
				prog = true
			}
		}
		next := active[:0]
		for _, k := range active {
			if capFn(k)-k.main > 0.5 {
				next = append(next, k)
			}
		}
		active = next
		if !prog {
			break
		}
	}
	return pool
}

// allocate places a subtree into (x,y,w,h): the two-pass waterfall of minimums →
// pins → zoom → natural → mop-up along the group's main axis.
func allocate(n *node, x, y, w, h float64) {
	n.hidden = false
	n.rect = Rect{x, y, w, h}
	n.why = ""
	if n.kind == leafKind {
		return
	}
	if stacked(n) {
		res := stackReserve(n)
		n.headerH = res
		child := selectedChild(n)
		for _, c := range n.children {
			if c != child {
				markHidden(c, "tab")
			}
		}
		if child != nil {
			bh := h - res
			if bh < 0 {
				bh = 0
			}
			allocate(child, x, y+res, w, bh)
		}
		return
	}
	horiz := isH(n.orient)
	mainAvail, crossAvail := h, w
	if horiz {
		mainAvail, crossAvail = w, h
	}
	kids := n.children
	for _, k := range kids {
		k.omit = false
	}
	// cross-axis (breadth) overflow
	for _, k := range kids {
		cmin := k.mMinW
		if horiz {
			cmin = k.mMinH
		}
		if cmin > crossAvail+0.5 {
			k.omit = true
		}
	}
	active := make([]*node, 0, len(kids))
	for _, k := range kids {
		if !k.omit {
			k.mainMin, k.mainNat, k.main = mMain(k, horiz)
			active = append(active, k)
		}
	}
	idx := func(k *node) int { return indexOf(n, k) }
	byPri := append([]*node(nil), active...)
	sort.Slice(byPri, func(a, b int) bool {
		if byPri[a].eff != byPri[b].eff {
			return byPri[a].eff > byPri[b].eff
		}
		return idx(byPri[a]) < idx(byPri[b])
	})
	// 1 · minimums by priority
	used := 0.0
	for _, k := range byPri {
		if used+k.mainMin <= mainAvail+0.5 {
			k.main = k.mainMin
			used += k.mainMin
		} else {
			k.omit = true
		}
	}
	surplus := mainAvail - used
	if surplus < 0 {
		surplus = 0
	}
	vis := make([]*node, 0, len(active))
	for _, k := range active {
		if !k.omit {
			vis = append(vis, k)
		}
	}
	// 2 · pins (reservations, by priority)
	for _, k := range byPri {
		if k.omit || !(k.pin.has && k.pin.enforced) {
			continue
		}
		want := k.pin.amount
		if k.mainMin > want {
			want = k.mainMin
		}
		give := want - k.main
		if give > surplus {
			give = surplus
		}
		if give > 0 {
			k.main += give
			surplus -= give
		}
	}
	// 3 · zoom (even split of remaining surplus)
	zoom := filterNodes(vis, func(k *node) bool { return k.mode == Zoom })
	if len(zoom) > 0 && surplus > 0.5 {
		share := surplus / float64(len(zoom))
		for _, k := range zoom {
			k.main += share
		}
		surplus = 0
	}
	// 4 · normal toward natural
	if surplus > 0.5 {
		normals := filterNodes(vis, func(k *node) bool {
			return k.mode == Normal && !(k.pin.has && k.pin.enforced)
		})
		surplus = waterfill(normals, surplus, func(k *node) float64 {
			if k.mainNat > k.main {
				return k.mainNat
			}
			return k.main
		})
	}
	// 5 · mop-up remainder -> last (in order) non-shrink, non-pinned survivor
	if surplus > 0.5 {
		cand := filterNodes(vis, func(k *node) bool {
			return k.mode != Shrink && !(k.pin.has && k.pin.enforced)
		})
		if len(cand) == 0 {
			cand = append([]*node(nil), vis...)
		}
		sort.Slice(cand, func(a, b int) bool { return idx(cand[a]) < idx(cand[b]) })
		if len(cand) > 0 {
			cand[len(cand)-1].main += surplus
		}
		surplus = 0
	}
	// position along the axis in orientation order
	ordered := append([]*node(nil), vis...)
	sort.Slice(ordered, func(a, b int) bool { return idx(ordered[a]) < idx(ordered[b]) })
	cur := 0.0
	for _, k := range ordered {
		var cx, cy, cw, ch float64
		if horiz {
			cw, ch, cy = k.main, crossAvail, y
			if n.orient == LTR {
				cx = x + cur
			} else {
				cx = x + (w - cur - cw)
			}
		} else {
			ch, cw, cx = k.main, crossAvail, x
			if n.orient == TTB {
				cy = y + cur
			} else {
				cy = y + (h - cur - ch)
			}
		}
		cur += k.main
		allocate(k, cx, cy, cw, ch)
	}
	for _, k := range kids {
		if k.omit {
			markHidden(k, "omit")
		}
	}
}

func mMain(k *node, horiz bool) (min, nat, main float64) {
	if horiz {
		return k.mMinW, k.mNatW, 0
	}
	return k.mMinH, k.mNatH, 0
}

func filterNodes(in []*node, keep func(*node) bool) []*node {
	out := make([]*node, 0, len(in))
	for _, k := range in {
		if keep(k) {
			out = append(out, k)
		}
	}
	return out
}

// normalize keeps the tree canonical: drop empty groups, flatten same-orientation
// groups (but a stacked group is a hard boundary), and collapse single-child
// groups (the promoted node inherits the slot's tab-state).
func normalize(n *node) {
	if n.kind != groupKind {
		return
	}
	for _, c := range n.children {
		normalize(c)
	}
	for changed, guard := true, 0; changed && guard < 2000; guard++ {
		changed = false
		// drop empty groups
		for i := len(n.children) - 1; i >= 0; i-- {
			c := n.children[i]
			if c.kind == groupKind && len(c.children) == 0 {
				n.children = append(n.children[:i], n.children[i+1:]...)
				changed = true
			}
		}
		// same-orientation flatten (stacks are hard boundaries)
		for i := 0; i < len(n.children); i++ {
			c := n.children[i]
			if c.kind == groupKind && !stacked(c) && !stacked(n) && c.orient == n.orient {
				rep := append([]*node(nil), n.children[:i]...)
				rep = append(rep, c.children...)
				rep = append(rep, n.children[i+1:]...)
				n.children = rep
				changed = true
				i--
			}
		}
		// single-child collapse: promote the only child into this slot
		for i := 0; i < len(n.children); i++ {
			c := n.children[i]
			if c.kind == groupKind && len(c.children) == 1 {
				only := c.children[0]
				only.selected = c.selected
				n.children[i] = only
				changed = true
			}
		}
	}
}

// ---- resolve pipeline ----

// ensure recomputes the layout if the tree changed since the last resolve.
func (v *Viewport) ensure() {
	if v.dirty {
		v.resolve()
	}
}

func (v *Viewport) resolve() {
	normalize(v.root)
	// the root is never a tab, and a bare-leaf root is wrapped so children can tile
	for v.root.kind == groupKind && len(v.root.children) == 1 && v.root.children[0].kind == groupKind {
		v.root = v.root.children[0]
	}
	if v.root.kind == leafKind {
		v.root = v.newGroup(LTR, v.root)
	}
	if v.root.kind == groupKind && len(v.root.children) == 0 {
		v.root.children = []*node{v.newLeaf("")}
	}
	v.root.selected = false
	rebuildParents(v.root)

	measure(v.root)
	computeEff(v.root)
	allocate(v.root, 0, 0, v.w, v.h)

	// snapshot TRUE geometry for navigation before any lens rewrites it
	walk(v.root, func(n *node) { n.navRect, n.navHidden = n.rect, n.hidden })

	// apply an active lens: re-expand the magnified subtree over its scope
	if v.lens != nil {
		X := v.lensTarget(v.lens.tile, v.lens.group)
		if X == nil {
			v.lens = nil
		} else if v.lens.scope == "screen" {
			markHidden(v.root, "lens")
			allocate(X, 0, 0, v.w, v.h)
		} else {
			a := X.parent
			if a == nil {
				a = v.root
			}
			r := a.rect
			if X.parent != nil {
				for _, c := range X.parent.children {
					if c != X {
						markHidden(c, "lens")
					}
				}
			}
			allocate(X, r.X, r.Y, r.W, r.H)
		}
	}
	v.dirty = false
}
