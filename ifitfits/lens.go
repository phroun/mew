package ifitfits

// lensTarget resolves the node a lens magnifies: monocle (group=false) magnifies
// the tile itself (climb past inert levels); spectacle (group=true) magnifies its
// enclosing group (climb one more tiling level).
func (v *Viewport) lensTarget(tile Handle, group bool) *node {
	t := v.negFrom(tile)
	if t == nil {
		return nil
	}
	if group && t.parent != nil {
		return climbTiling(t.parent)
	}
	return t
}

// Monocle magnifies the tile to fill the whole screen.
func (v *Viewport) Monocle(tile Handle, state State) { v.setLens(tile, false, "screen", state) }

// LocalMonocle magnifies the tile to fill its own group box.
func (v *Viewport) LocalMonocle(tile Handle, state State) { v.setLens(tile, false, "group", state) }

// Spectacle magnifies the tile's enclosing group to fill the whole screen.
func (v *Viewport) Spectacle(tile Handle, state State) { v.setLens(tile, true, "screen", state) }

// LocalSpectacle magnifies the tile's enclosing group to fill its own group box.
func (v *Viewport) LocalSpectacle(tile Handle, state State) { v.setLens(tile, true, "group", state) }

func (v *Viewport) setLens(tile Handle, group bool, scope string, state State) {
	same := v.lens != nil && v.lens.tile == tile && v.lens.group == group && v.lens.scope == scope
	switch state {
	case On:
		v.lens = &lensState{tile: tile, group: group, scope: scope}
	case Off:
		if same {
			v.lens = nil
		}
	default: // Toggle
		if same {
			v.lens = nil
		} else {
			v.lens = &lensState{tile: tile, group: group, scope: scope}
		}
	}
	v.touch()
}

// SetFocus tells the engine the host moved focus to `tile`. Its only effect is to
// dismiss an active lens when focus lands outside the magnified subtree.
func (v *Viewport) SetFocus(tile Handle) {
	n := v.tile(tile)
	if n == nil {
		return
	}
	v.focus = n
	if v.lens != nil {
		X := v.lensTarget(v.lens.tile, v.lens.group)
		if X == nil || !isAncestorOf(X, n) {
			v.lens = nil
			v.touch()
		}
	}
}

// GetFocus returns the last tile the host reported via SetFocus (0 if none or if
// it has since been closed). The engine does not otherwise track focus.
func (v *Viewport) GetFocus() Handle {
	if v.focus != nil {
		if _, ok := v.handles[v.focus.handle]; ok {
			return v.focus.handle
		}
	}
	return 0
}
