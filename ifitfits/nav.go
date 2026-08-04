package ifitfits

// side maps a physical direction to an insertion end (Before/After) within a
// group of the given orientation.
func side(o Orient, d Direction) Direction {
	if d == Before || d == After {
		return d
	}
	switch o {
	case LTR:
		if d == Left {
			return Before
		}
	case RTL:
		if d == Left {
			return After
		}
		return Before
	case TTB:
		if d == Up {
			return Before
		}
	case BTT:
		if d == Up {
			return After
		}
		return Before
	}
	return After
}

// nav geometry reads the TRUE, pre-lens boxes so a direction press finds the real
// neighbor (and thus breaks out of a lens).
func navRect(n *node) Rect   { return n.navRect }
func navHidden(n *node) bool { return n.navHidden }

// landingLeaf descends a neighbor subtree to the leaf a directional move lands on:
// at a level whose axis matches travel it takes the entered edge; at a
// perpendicular level it takes the child under the caret's goal coordinate.
func (v *Viewport) landingLeaf(cur *node, d Direction) *node {
	navH := d == Left || d == Right
	for cur != nil && cur.kind == groupKind {
		kids := filterNodes(cur.children, func(c *node) bool { return !navHidden(c) })
		if len(kids) == 0 {
			return nil
		}
		if (axisOf(cur.orient) == 'H') == navH {
			pick := kids[0]
			for _, c := range kids[1:] {
				switch d {
				case Right:
					if navRect(c).X < navRect(pick).X {
						pick = c
					}
				case Left:
					if navRect(c).X+navRect(c).W > navRect(pick).X+navRect(pick).W {
						pick = c
					}
				case Down:
					if navRect(c).Y < navRect(pick).Y {
						pick = c
					}
				case Up:
					if navRect(c).Y+navRect(c).H > navRect(pick).Y+navRect(pick).H {
						pick = c
					}
				}
			}
			cur = pick
		} else {
			g := v.caretX
			lo := func(c *node) float64 { return navRect(c).X }
			hi := func(c *node) float64 { return navRect(c).X + navRect(c).W }
			if navH {
				g = v.caretY
				lo = func(c *node) float64 { return navRect(c).Y }
				hi = func(c *node) float64 { return navRect(c).Y + navRect(c).H }
			}
			var pick *node
			for _, c := range kids {
				if g >= lo(c)-0.5 && g <= hi(c)+0.5 {
					pick = c
					break
				}
			}
			if pick == nil {
				pick = kids[0]
				bd := gap(lo(pick), hi(pick), g)
				for _, c := range kids[1:] {
					if gp := gap(lo(c), hi(c), g); gp < bd {
						bd, pick = gp, c
					}
				}
			}
			cur = pick
		}
	}
	return cur
}

func gap(lo, hi, g float64) float64 {
	d := 0.0
	if lo-g > d {
		d = lo - g
	}
	if g-hi > d {
		d = g - hi
	}
	return d
}

// navTarget finds the leaf a directional move from `from` lands on: walk up to the
// nearest ancestor whose axis matches travel, take its sibling in that direction,
// then descend under the caret. Stacked ancestors are transparent (axis Z).
func (v *Viewport) navTarget(from *node, d Direction) *node {
	navH := d == Left || d == Right
	want := byte('V')
	if navH {
		want = 'H'
	}
	node, parent := from, from.parent
	for parent != nil {
		if axOf(parent) == want {
			i := indexOf(parent, node)
			step := 1
			if side(parent.orient, d) == Before {
				step = -1
			}
			for ni := i + step; ni >= 0 && ni < len(parent.children); ni += step {
				if cand := v.landingLeaf(parent.children[ni], d); cand != nil {
					return cand
				}
			}
		}
		node, parent = parent, parent.parent
	}
	return nil
}

// caretToEdge slides the caret goal within `cur` along the pressed axis: to the
// pressed-direction edge, unless the caret already sits in the opposite third, in
// which case it goes to center — so repeats step opposite-edge → center → edge.
func (v *Viewport) caretToEdge(cur *node, d Direction) {
	b := navRect(cur)
	horiz := d == Left || d == Right
	lo, size := b.Y, b.H
	if horiz {
		lo, size = b.X, b.W
	}
	hi, mid, third := lo+size, lo+size/2, size/3
	pos := v.caretY
	if horiz {
		pos = v.caretX
	}
	var np float64
	if d == Right || d == Down {
		if pos <= lo+third {
			np = mid
		} else {
			np = hi
		}
	} else {
		if pos >= hi-third {
			np = mid
		} else {
			np = lo
		}
	}
	if horiz {
		v.caretX = np
	} else {
		v.caretY = np
	}
}

// rehomeCaret re-homes the caret on a tile PER AXIS: keep whichever axis still
// falls inside the tile, re-center only the axis that no longer does. So an
// insert/split/close that renegotiates one axis leaves the other axis's lane.
func (v *Viewport) rehomeCaret(n *node) {
	if n == nil {
		return
	}
	r := n.rect
	if v.caretX < r.X || v.caretX > r.X+r.W {
		v.caretX = r.X + r.W/2
	}
	if v.caretY < r.Y || v.caretY > r.Y+r.H {
		v.caretY = r.Y + r.H/2
	}
}

// Go moves in a direction and returns the destination tile, updating the caret as
// a side effect. Spatial directions (Up/Down/Left/Right) navigate by geometry and,
// at a workspace edge, slide the caret instead of moving. Prior/Next cycle the
// whole tree in reading order (wrapping).
func (v *Viewport) Go(tile Handle, d Direction) Handle {
	from := v.tile(tile)
	if from == nil {
		return 0
	}
	v.ensure()
	if d == Prior || d == Next {
		dest := v.readingCycle(from, d)
		if dest != nil {
			v.rehomeCaret(dest)
			return dest.handle
		}
		return tile
	}
	t := v.navTarget(from, d)
	if t == nil {
		v.caretToEdge(from, d)
		return tile
	}
	b := navRect(t)
	if d == Left || d == Right {
		v.caretX = b.X + b.W/2
	} else {
		v.caretY = b.Y + b.H/2
	}
	return t.handle
}

// readingCycle steps the focus through all leaves in reading order, wrapping.
func (v *Viewport) readingCycle(from *node, d Direction) *node {
	ls := leavesOf(v.root)
	if len(ls) == 0 {
		return nil
	}
	i := 0
	for k, l := range ls {
		if l == from {
			i = k
			break
		}
	}
	step := 1
	if d == Prior {
		step = -1
	}
	return ls[((i+step)%len(ls)+len(ls))%len(ls)]
}

// direction aliases
func (v *Viewport) Up(t Handle) Handle    { return v.Go(t, Up) }
func (v *Viewport) Down(t Handle) Handle  { return v.Go(t, Down) }
func (v *Viewport) Left(t Handle) Handle  { return v.Go(t, Left) }
func (v *Viewport) Right(t Handle) Handle { return v.Go(t, Right) }
func (v *Viewport) Prior(t Handle) Handle { return v.Go(t, Prior) }
func (v *Viewport) Next(t Handle) Handle  { return v.Go(t, Next) }

// SetCaret sets the caret goal from coordinates LOCAL to a tile (0,0 = the tile's
// top-left), clamped to the tile's size, then mapped into the workspace.
func (v *Viewport) SetCaret(tile Handle, x, y float64) {
	n := v.tile(tile)
	if n == nil {
		return
	}
	v.ensure()
	r := n.navRect
	x = clamp(x, 0, r.W)
	y = clamp(y, 0, r.H)
	v.caretX, v.caretY = r.X+x, r.Y+y
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// GetTile returns the tile at absolute workspace coordinates, hit-testing the
// fully resolved (on-screen) layout. Returns 0 if none.
func (v *Viewport) GetTile(x, y float64) Handle {
	v.ensure()
	var hit *node
	walk(v.root, func(n *node) {
		if n.kind != leafKind || n.hidden {
			return
		}
		r := n.rect
		if x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H {
			hit = n
		}
	})
	if hit != nil {
		return hit.handle
	}
	return 0
}
