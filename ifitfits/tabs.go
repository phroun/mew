package ifitfits

// TabNext cycles the tile's enclosing stack forward and returns the newly shown
// tile. Nested stacks read as one continuous order (step the innermost that can
// move, escalate at each boundary, and only the OUTERMOST wraps). When the tile is
// not inside any stack, it runs the loose odometer over the visible tab-groups
// instead (which does not move focus) and returns the tile unchanged.
func (v *Viewport) TabNext(tile Handle) Handle { return v.tabCycle(tile, 1) }

// TabPrior cycles backward.
func (v *Viewport) TabPrior(tile Handle) Handle { return v.tabCycle(tile, -1) }

func (v *Viewport) goTab(g *node, j int) Handle {
	selectChild(g, g.children[j])
	v.touch()
	if l := focusEntry(g.children[j]); l != nil {
		return l.handle
	}
	return 0
}

func (v *Viewport) tabCycle(tile Handle, step int) Handle {
	s := v.tile(tile)
	if s == nil {
		return 0
	}
	var stacks []*node
	for g := enclosingStack(s); g != nil; g = enclosingStack(g) {
		stacks = append(stacks, g)
	}
	for _, g := range stacks {
		i := indexOf(g, selectedChild(g))
		j := i + step
		if j >= 0 && j < len(g.children) {
			return v.goTab(g, j)
		}
	}
	if len(stacks) > 0 { // every enclosing stack at its boundary -> wrap the outermost
		top := stacks[len(stacks)-1]
		n := len(top.children)
		if n < 2 {
			return tile
		}
		i := indexOf(top, selectedChild(top))
		return v.goTab(top, ((i+step)%n+n)%n)
	}
	// not inside any stack: odometer over the visible tab-groups
	v.ensure()
	v.cycleVisibleStack(step)
	v.touch()
	return tile
}

// ---- loose odometer over visible tab-groups (no focus move) ----

func visible(n *node) bool { return !n.hidden }

func (v *Viewport) visStacks() []*node {
	var a []*node
	walk(v.root, func(n *node) {
		if n.kind == groupKind && stacked(n) && visible(n) && len(n.children) >= 2 {
			a = append(a, n)
		}
	})
	return a
}

func parentStack(g *node) *node {
	for p := g.parent; p != nil; p = p.parent {
		if stacked(p) && len(p.children) >= 2 {
			return p
		}
	}
	return nil
}

func childStacks(S *node) []*node {
	var a []*node
	var w func(n *node)
	w = func(n *node) {
		if n.kind != groupKind {
			return
		}
		if stacked(n) && len(n.children) >= 2 {
			a = append(a, n)
			return
		}
		for _, c := range n.children {
			w(c)
		}
	}
	if sc := selectedChild(S); sc != nil {
		w(sc)
	}
	return a
}

func indexOfNode(s []*node, n *node) int {
	for i, x := range s {
		if x == n {
			return i
		}
	}
	return -1
}

// cycleVisibleStack is the hierarchical odometer: a baton walks the deepest active
// stack's tabs; off its end it hands to the next peer at that level; once a level
// is spent it rises to the parent; the top level is the only ring that wraps.
func (v *Viewport) cycleVisibleStack(step int) {
	vs := v.visStacks()
	if len(vs) == 0 {
		return
	}
	dir := 1
	if step < 0 {
		dir = -1
	}
	shown := func(g *node) int { return indexOf(g, selectedChild(g)) }
	set := func(g *node, i int) { selectChild(g, g.children[i]) }
	entry := func(h *node) int {
		if step > 0 {
			return 0
		}
		return len(h.children) - 1
	}
	var descend func(S *node)
	descend = func(S *node) {
		cur := S
		for {
			ks := childStacks(cur)
			if len(ks) == 0 {
				v.visCur = cur
				return
			}
			nx := ks[0]
			if step < 0 {
				nx = ks[len(ks)-1]
			}
			set(nx, entry(nx))
			cur = nx
		}
	}
	enter := func(S *node) {
		cur, moved := S, false
		for {
			before, e := shown(cur), entry(cur)
			if before != e {
				set(cur, e)
				moved = true
			}
			ks := childStacks(cur)
			if len(ks) == 0 && !moved {
				set(cur, e+step)
				moved = true
				ks = childStacks(cur)
			}
			if len(ks) == 0 {
				v.visCur = cur
				return
			}
			if step > 0 {
				cur = ks[0]
			} else {
				cur = ks[len(ks)-1]
			}
		}
	}
	var D *node
	if v.visCur != nil {
		for _, s := range vs {
			if s == v.visCur {
				D = s
				break
			}
		}
	}
	if D == nil {
		var top []*node
		for _, s := range vs {
			if parentStack(s) == nil {
				top = append(top, s)
			}
		}
		if len(top) == 0 {
			return
		}
		if step > 0 {
			enter(top[0])
		} else {
			enter(top[len(top)-1])
		}
		return
	}
	for g := D; ; {
		j := shown(g) + step
		if j >= 0 && j < len(g.children) {
			set(g, j)
			descend(g)
			return
		}
		par := parentStack(g)
		var peers []*node
		for _, s := range vs {
			if parentStack(s) == par {
				peers = append(peers, s)
			}
		}
		i := indexOfNode(peers, g)
		if par == nil {
			m := len(peers)
			enter(peers[((i+dir)%m+m)%m])
			return
		}
		ni := i + dir
		if ni >= 0 && ni < len(peers) {
			enter(peers[ni])
			return
		}
		g = par
	}
}
