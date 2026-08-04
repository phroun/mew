package ifitfits

// ---- small tree edits ----

func axisFor(d Direction) byte {
	if d == Up || d == Down {
		return 'V'
	}
	return 'H'
}

func (v *Viewport) insertSib(g, ref, n *node, s Direction) {
	i := indexOf(g, ref)
	if s == After {
		i++
	}
	kids := make([]*node, 0, len(g.children)+1)
	kids = append(kids, g.children[:i]...)
	kids = append(kids, n)
	kids = append(kids, g.children[i:]...)
	g.children = kids
	n.parent = g
}

func (v *Viewport) replaceNode(oldN, neu *node) {
	neu.selected = oldN.selected
	oldN.selected = false
	if oldN == v.root {
		v.root = neu
		neu.parent = nil
	} else {
		p := oldN.parent
		p.children[indexOf(p, oldN)] = neu
		neu.parent = p
	}
}

func (v *Viewport) seekUpInsert(G *node, ad byte, d Direction, nl *node) {
	cur, parent := G, G.parent
	for parent != nil {
		if axOf(parent) == ad {
			v.insertSib(parent, cur, nl, side(parent.orient, d))
			return
		}
		cur, parent = parent, parent.parent
	}
	o := defOrient(ad)
	if side(o, d) == Before {
		v.replaceNode(G, v.newGroup(o, nl, G))
	} else {
		v.replaceNode(G, v.newGroup(o, G, nl))
	}
}

func contraryOrient(g *node) Orient {
	if g.parent == nil {
		return g.orient
	}
	if axisOf(g.parent.orient) == 'H' {
		return defOrient('V')
	}
	return defOrient('H')
}

func enclosingStack(n *node) *node {
	if n == nil {
		return nil
	}
	for p := n.parent; p != nil; p = p.parent {
		if stacked(p) {
			return p
		}
	}
	return nil
}

func isAncestorOf(a, b *node) bool {
	for n := b; n != nil; n = n.parent {
		if n == a {
			return true
		}
	}
	return false
}

func detach(n *node) {
	if p := n.parent; p != nil {
		p.children = removeNode(p.children, n)
	}
	n.selected = false
}

func removeNode(s []*node, n *node) []*node {
	for i, x := range s {
		if x == n {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func swapNodes(a, b *node) {
	pa, pb := a.parent, b.parent
	ia, ib := indexOf(pa, a), indexOf(pb, b)
	pa.children[ia] = b
	pb.children[ib] = a
	a.parent, b.parent = pb, pa
}

func focusEntry(n *node) *node {
	for n != nil && n.kind == groupKind {
		if len(n.children) == 0 {
			return nil
		}
		if stacked(n) {
			n = selectedChild(n)
		} else {
			n = n.children[0]
		}
	}
	return n
}

// nextFocusAfterDismiss picks the tile focus should move to before `t` is closed:
// next sibling in reading order, else previous, else recurse up when t is the
// only child (its group dissolves).
func nextFocusAfterDismiss(t *node) *node {
	for cur := t; cur.parent != nil; cur = cur.parent {
		kids := cur.parent.children
		i := indexOf(cur.parent, cur)
		if i+1 < len(kids) {
			return firstLeaf(kids[i+1])
		}
		if i-1 >= 0 {
			return lastLeaf(kids[i-1])
		}
	}
	return nil
}

// settle re-resolves and re-homes the caret on `focus` (per-axis), the common tail
// of a structural edit.
func (v *Viewport) settle(focus *node) {
	v.touch()
	v.ensure()
	v.rehomeCaret(focus)
}

var flipMap = map[Orient]Orient{LTR: RTL, RTL: LTR, TTB: BTT, BTT: TTB}

// ---- structural commands ----

// New inserts a new tile relative to `tile`. A directional new busts out to the
// nearest matching-axis level. The new tile clones `tile`'s ref unless a ref is
// given. Returns the new tile.
func (v *Viewport) New(tile Handle, d Direction, ref ...string) Handle {
	s := v.tile(tile)
	if s == nil || s.parent == nil {
		return 0
	}
	G := s.parent
	r := s.ref
	if len(ref) > 0 {
		r = ref[0]
	}
	nl := v.newLeaf(r)
	if d == Before || d == After {
		v.insertSib(G, s, nl, d)
		v.settle(s)
		return nl.handle
	}
	ad := axisFor(d)
	if axOf(G) == ad {
		sd := side(G.orient, d)
		i := indexOf(G, s)
		atEnd := (sd == Before && i == 0) || (sd == After && i == len(G.children)-1)
		if !atEnd {
			v.insertSib(G, s, nl, sd)
			v.settle(s)
			return nl.handle
		}
	}
	v.seekUpInsert(G, ad, d, nl)
	v.settle(s)
	return nl.handle
}

// Split wraps `tile` in a new nested group in place; the new tile clones `tile`'s
// ref unless a ref is given. Returns the new tile.
func (v *Viewport) Split(tile Handle, d Direction, ref ...string) Handle {
	s := v.tile(tile)
	if s == nil || s.parent == nil {
		return 0
	}
	G := s.parent
	var o Orient
	var s2 Direction
	if d == Before || d == After {
		if axisOf(G.orient) == 'H' {
			o = defOrient('V')
		} else {
			o = defOrient('H')
		}
		s2 = d
	} else {
		ax := axisFor(d)
		if ax == axisOf(G.orient) {
			o = G.orient
		} else {
			o = defOrient(ax)
		}
		s2 = side(o, d)
	}
	r := s.ref
	if len(ref) > 0 {
		r = ref[0]
	}
	nl := v.newLeaf(r)
	if s2 == Before {
		v.replaceNode(s, v.newGroup(o, nl, s))
	} else {
		v.replaceNode(s, v.newGroup(o, s, nl))
	}
	v.settle(s)
	return nl.handle
}

// Close removes `tile` and returns the tile focus should move to (0 if the tree
// emptied — a fresh tile is created).
func (v *Viewport) Close(tile Handle) Handle {
	s := v.tile(tile)
	if s == nil {
		return 0
	}
	next := nextFocusAfterDismiss(s)
	if p := s.parent; p != nil {
		p.children = removeNode(p.children, s)
	}
	delete(v.handles, s.handle)
	if next != nil {
		v.settle(next)
		return next.handle
	}
	v.touch()
	v.ensure()
	return 0
}

// Flip flips the orientation polarity of `tile`'s group.
func (v *Viewport) Flip(tile Handle) {
	if s := v.tile(tile); s != nil && s.parent != nil {
		s.parent.orient = flipMap[s.parent.orient]
		v.touch()
	}
}

// FlipParent flips the orientation one level up.
func (v *Viewport) FlipParent(tile Handle) {
	if s := v.tile(tile); s != nil && s.parent != nil && s.parent.parent != nil {
		s.parent.parent.orient = flipMap[s.parent.parent.orient]
		v.touch()
	}
}

// Reverse reverses the child order of `tile`'s group.
func (v *Viewport) Reverse(tile Handle) {
	if s := v.tile(tile); s != nil && s.parent != nil {
		reverse(s.parent.children)
		v.touch()
	}
}

// ReverseParent reverses the grandparent's child order.
func (v *Viewport) ReverseParent(tile Handle) {
	if s := v.tile(tile); s != nil && s.parent != nil && s.parent.parent != nil {
		reverse(s.parent.parent.children)
		v.touch()
	}
}

func reverse(s []*node) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// Stack folds `tile`'s group into a tabbed stack or unfolds it. state On stacks,
// Off unstacks, Toggle flips.
func (v *Viewport) Stack(tile Handle, state State) {
	s := v.tile(tile)
	if s == nil || s.parent == nil {
		return
	}
	g := s.parent
	is := stacked(g)
	want := !is
	switch state {
	case On:
		want = true
	case Off:
		want = false
	}
	if want && !is {
		for _, c := range g.children {
			c.selected = c == s
		}
		v.touch()
	} else if !want && is {
		for _, c := range g.children {
			c.selected = false
		}
		g.orient = contraryOrient(g)
		v.touch()
	}
}

// Swap exchanges `tile` (its whole box, if it is a stacked tab) with the neighbor
// in `dir`; at a workspace edge it slides the caret instead (a non-swap).
func (v *Viewport) Swap(tile Handle, d Direction) {
	src := opTargetOf(v.tile(tile))
	if src == nil {
		return
	}
	v.ensure()
	t := v.navTarget(src, d)
	if t == nil || t == src {
		v.caretToEdge(src, d)
		return
	}
	vert := d == Up || d == Down
	keep := v.caretY
	if vert {
		keep = v.caretX
	}
	swapNodes(src, t)
	src.pin, t.pin = t.pin, src.pin
	src.mode, t.mode = t.mode, src.mode
	src.selected, t.selected = t.selected, src.selected
	v.touch()
	v.ensure()
	r := src.rect
	const ins = 8
	if vert {
		v.caretX = clamp(keep, r.X+ins, r.X+r.W-ins)
		v.caretY = r.Y + r.H/2
	} else {
		v.caretY = clamp(keep, r.Y+ins, r.Y+r.H-ins)
		v.caretX = r.X + r.W/2
	}
}

// SwapUp/Down/Left/Right are direction aliases for Swap.
func (v *Viewport) SwapUp(t Handle)    { v.Swap(t, Up) }
func (v *Viewport) SwapDown(t Handle)  { v.Swap(t, Down) }
func (v *Viewport) SwapLeft(t Handle)  { v.Swap(t, Left) }
func (v *Viewport) SwapRight(t Handle) { v.Swap(t, Right) }

// Merge moves the single tile `tile` into the adjacent group in `dir` — a lone tab
// can be extracted from its stack without dragging the rest along.
func (v *Viewport) Merge(tile Handle, d Direction) {
	src := v.tile(tile)
	if src == nil {
		return
	}
	v.ensure()
	ad := axisFor(d)
	pd := byte('V')
	if ad == 'V' {
		pd = 'H'
	}
	D := v.navTarget(src, d)
	if D == src {
		return
	}
	// keep the source stack alive: activate a neighbor tab before extracting.
	if g := src.parent; g != nil && stacked(g) && src.selected {
		i := indexOf(g, src)
		var nb *node
		if i+1 < len(g.children) {
			nb = g.children[i+1]
		} else if i-1 >= 0 {
			nb = g.children[i-1]
		}
		if nb != nil {
			selectChild(g, nb)
		}
	}
	if D == nil {
		v.mergeToEdge(src, d, ad)
		return
	}
	dst := destStack(D, src)
	detach(src)
	if dst != nil {
		dst.children = append(dst.children, src)
		src.parent = dst
	} else {
		var phys Direction
		if pd == 'H' {
			if v.caretX <= D.rect.X+D.rect.W/2 {
				phys = Left
			} else {
				phys = Right
			}
		} else {
			if v.caretY <= D.rect.Y+D.rect.H/2 {
				phys = Up
			} else {
				phys = Down
			}
		}
		p := D.parent
		if p != nil && !stacked(p) && axisOf(p.orient) == pd {
			v.insertSib(p, D, src, side(p.orient, phys))
		} else {
			o := defOrient(pd)
			if side(o, phys) == Before {
				v.replaceNode(D, v.newGroup(o, src, D))
			} else {
				v.replaceNode(D, v.newGroup(o, D, src))
			}
		}
	}
	v.settle(src)
}

// MergeUp/Down/Left/Right are direction aliases for Merge.
func (v *Viewport) MergeUp(t Handle)    { v.Merge(t, Up) }
func (v *Viewport) MergeDown(t Handle)  { v.Merge(t, Down) }
func (v *Viewport) MergeLeft(t Handle)  { v.Merge(t, Left) }
func (v *Viewport) MergeRight(t Handle) { v.Merge(t, Right) }

func destStack(D, src *node) *node {
	var st *node
	for n := D; n != nil; n = n.parent {
		if stacked(n) && !isAncestorOf(n, src) {
			st = n
		}
	}
	return st
}

func (v *Viewport) mergeToEdge(s *node, d Direction, ad byte) {
	if len(leavesOf(v.root)) < 2 {
		return
	}
	detach(s)
	if axisOf(v.root.orient) == ad {
		if side(v.root.orient, d) == Before {
			v.root.children = append([]*node{s}, v.root.children...)
		} else {
			v.root.children = append(v.root.children, s)
		}
		s.parent = v.root
	} else {
		o := defOrient(ad)
		if side(o, d) == Before {
			v.root = v.newGroup(o, s, v.root)
		} else {
			v.root = v.newGroup(o, v.root, s)
		}
	}
	v.settle(s)
}

// MoveTabNext/MoveTabPrior reorder the active tab within its stack (no wrap).
func (v *Viewport) MoveTabNext(tile Handle)  { v.moveTab(tile, 1) }
func (v *Viewport) MoveTabPrior(tile Handle) { v.moveTab(tile, -1) }

func (v *Viewport) moveTab(tile Handle, step int) {
	g := enclosingStack(v.tile(tile))
	if g == nil {
		return
	}
	i := indexOf(g, selectedChild(g))
	j := i + step
	if j < 0 || j >= len(g.children) {
		return
	}
	g.children[i], g.children[j] = g.children[j], g.children[i]
	v.touch()
}

// opTargetOf returns the box a whole-tile op acts on: the stack when `n` is a
// direct tab of one (the stack shadows its tabs), else `n`.
func opTargetOf(n *node) *node {
	if n == nil {
		return nil
	}
	if n.parent != nil && stacked(n.parent) {
		return n.parent
	}
	return n
}
