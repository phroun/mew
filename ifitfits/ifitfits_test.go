package ifitfits

import (
	"math"
	"testing"
)

// ---- helpers (in-package: tests may touch the tree directly) ----

func near(a, b float64) bool { return math.Abs(a-b) < 0.6 }

func newVP(w, h float64) *Viewport {
	return &Viewport{w: w, h: h, handles: map[Handle]*node{}, dirty: true}
}

func (v *Viewport) grp(o Orient, kids ...*node) *node { return v.newGroup(o, kids...) }

// setRoot installs a hand-built tree and re-parents it.
func (v *Viewport) setRoot(r *node) {
	v.root = r
	rebuildParents(r)
	v.touch()
}

func (v *Viewport) rectOf(h Handle) Rect { v.ensure(); return v.handles[h].rect }
func refRect(n *node) Rect               { return n.rect }

// ---- layout ----

func TestBasicLayout(t *testing.T) {
	v := newVP(300, 100)
	a, b, c := v.newLeaf("a"), v.newLeaf("b"), v.newLeaf("c")
	for _, l := range []*node{a, b, c} {
		l.minW, l.minH, l.natW, l.natH = 20, 20, 100, 100
	}
	v.setRoot(v.grp(LTR, a, b, c))
	if r := v.rectOf(a.handle); !near(r.X, 0) || !near(r.W, 100) {
		t.Fatalf("a = %+v, want x0 w100", r)
	}
	if r := v.rectOf(b.handle); !near(r.X, 100) || !near(r.W, 100) {
		t.Fatalf("b = %+v, want x100 w100", r)
	}
	if r := v.rectOf(c.handle); !near(r.X, 200) || !near(r.W, 100) {
		t.Fatalf("c = %+v, want x200 w100", r)
	}
}

// ---- sizing climbs past only-child / stack-tab levels ----

func TestZoomClimbsToStackBox(t *testing.T) {
	// zooming a tab climbs past the stack's tab-boundary to the stack BOX, which
	// then competes with its sibling. (A sole-child group would be dissolved by
	// normalize, so the meaningful, persistent climb is the tab -> stack one.)
	v := newVP(300, 100)
	t1, t2 := v.newLeaf("t1"), v.newLeaf("t2")
	for _, l := range []*node{t1, t2} {
		l.minW, l.minH, l.natW, l.natH = 20, 20, 100, 100
	}
	s := v.grp(LTR, t1, t2)
	selectChild(s, t1) // s is a 2-tab stack (survives normalize)
	q := v.newLeaf("q")
	q.minW, q.minH, q.natW, q.natH = 20, 20, 100, 100
	v.setRoot(v.grp(LTR, s, q))
	v.Zoom(t1.handle, On) // climbs t1 -> s (the stack box)
	v.ensure()
	if s.mode != Zoom {
		t.Fatalf("zoom landed on the tab, not the stack box (s.mode=%v)", s.mode)
	}
	if t1.mode == Zoom {
		t.Fatalf("zoom should have climbed off the tab t1")
	}
	if refRect(s).W <= refRect(q).W {
		t.Fatalf("zoom did not take effect: s.W=%v q.W=%v", refRect(s).W, refRect(q).W)
	}
}

func TestEqualizeThenBalance(t *testing.T) {
	v := newVP(360, 100)
	a, b, c := v.newLeaf("a"), v.newLeaf("b"), v.newLeaf("c")
	a.minW, a.natW = 20, 50
	b.minW, b.natW = 20, 250
	c.minW, c.natW = 20, 120
	for _, l := range []*node{a, b, c} {
		l.minH, l.natH = 20, 100
	}
	v.setRoot(v.grp(LTR, a, b, c))
	v.Resize(a.handle, 200) // ragged pin
	v.Equalize(a.handle, false)
	v.ensure()
	for _, l := range []*node{a, b, c} {
		if !near(refRect(l).W, 120) {
			t.Fatalf("equalize: %s.W=%v want 120", l.ref, refRect(l).W)
		}
	}
	v.Balance(a.handle, false)
	v.ensure()
	if a.pin.has {
		t.Fatalf("balance should have cleared a's pin")
	}
	if !near(refRect(a).W, 50) || !near(refRect(c).W, 120) {
		t.Fatalf("balance: a.W=%v (want 50) c.W=%v (want 120)", refRect(a).W, refRect(c).W)
	}
}

// ---- lens remap: m=tile/screen, M=tile/group, p=group/screen, P=group/group ----

func TestLensTargets(t *testing.T) {
	v := newVP(400, 200)
	a1, a2 := v.newLeaf("a1"), v.newLeaf("a2")
	colA := v.grp(TTB, a1, a2)
	b := v.newLeaf("b")
	v.setRoot(v.grp(LTR, colA, b))

	// monocle: magnify the tile
	v.Monocle(a1.handle, On)
	if x := v.lensTarget(v.lens.tile, v.lens.group); x != a1 {
		t.Fatalf("monocle target = %v, want a1", x)
	}
	if v.lens.scope != "screen" {
		t.Fatalf("monocle scope=%s want screen", v.lens.scope)
	}
	v.Monocle(a1.handle, Off)

	// spectacle: magnify the enclosing group (negTarget twice)
	v.Spectacle(a1.handle, On)
	if x := v.lensTarget(v.lens.tile, v.lens.group); x != colA {
		t.Fatalf("spectacle target = %v, want colA", x)
	}
	// local_spectacle fills the group box
	v.LocalSpectacle(a1.handle, On)
	if v.lens.scope != "group" {
		t.Fatalf("local_spectacle scope=%s want group", v.lens.scope)
	}
}

func TestMonocleFillsScreenAndDismisses(t *testing.T) {
	v := newVP(400, 200)
	a, b := v.newLeaf("a"), v.newLeaf("b")
	for _, l := range []*node{a, b} {
		l.minW, l.minH = 20, 20
	}
	v.setRoot(v.grp(LTR, a, b))
	v.Monocle(a.handle, On)
	v.ensure()
	if r := refRect(a); !near(r.W, 400) || !near(r.H, 200) {
		t.Fatalf("monocle a = %+v, want full screen", r)
	}
	if !b.hidden {
		t.Fatalf("monocle should hide b")
	}
	// b is still navigable underneath (true geometry preserved)
	if b.navHidden {
		t.Fatalf("b.navHidden should be false (nav still sees it)")
	}
	// focusing outside the lens dismisses it
	v.SetFocus(b.handle)
	if v.lens != nil {
		t.Fatalf("lens should dismiss when focus leaves the magnified subtree")
	}
}

// ---- edge slide: opposite-edge -> center -> pressed-edge ----

func TestEdgeSlideFlip(t *testing.T) {
	v := newVP(300, 100)
	band := v.newLeaf("band")
	band.minW, band.minH = 20, 20
	v.setRoot(v.grp(LTR, band))
	v.ensure()
	seq := []float64{}
	v.caretX = 0
	for i := 0; i < 3; i++ {
		v.Go(band.handle, Right)
		seq = append(seq, v.caretX)
	}
	want := []float64{150, 300, 300}
	for i := range want {
		if !near(seq[i], want[i]) {
			t.Fatalf("edge-slide right = %v, want %v", seq, want)
		}
	}
	// mirror
	v.caretX = 300
	for i, w := range []float64{150, 0, 0} {
		v.Go(band.handle, Left)
		if !near(v.caretX, w) {
			t.Fatalf("edge-slide left step %d: caretX=%v want %v", i, v.caretX, w)
		}
	}
}

// the band example: press right (edge) then down exits on the right side.
func TestEdgeSlideRoute(t *testing.T) {
	v := newVP(300, 200)
	band := v.newLeaf("band")
	p, q := v.newLeaf("p"), v.newLeaf("q")
	for _, l := range []*node{band, p, q} {
		l.minW, l.minH = 20, 20
	}
	v.setRoot(v.grp(TTB, band, v.grp(LTR, p, q)))
	v.ensure()
	v.caretX, v.caretY = 150, 40
	v.Go(band.handle, Right) // at edge -> caret to right edge
	dest := v.Go(band.handle, Down)
	if dest != q.handle {
		t.Fatalf("right-edge then down landed on %v, want q", dest)
	}
	v.caretX, v.caretY = 150, 40
	v.Go(band.handle, Left)
	if dest := v.Go(band.handle, Down); dest != p.handle {
		t.Fatalf("left-edge then down landed on %v, want p", dest)
	}
}

func TestNavBreaksOutOfMonocle(t *testing.T) {
	v := newVP(400, 200)
	band := v.newLeaf("band")
	p, q := v.newLeaf("p"), v.newLeaf("q")
	for _, l := range []*node{band, p, q} {
		l.minW, l.minH = 20, 20
	}
	v.setRoot(v.grp(TTB, band, v.grp(LTR, p, q)))
	v.Monocle(band.handle, On)
	v.ensure()
	dest := v.Go(band.handle, Down)
	if dest != p.handle && dest != q.handle {
		t.Fatalf("nav out of monocle landed on %v, want p or q", dest)
	}
}

// ---- refs: clone on new/split; get/content/set ----

func TestRefCloneAndLookup(t *testing.T) {
	v, first := NewViewport(400, 200)
	v.Set(first, "doc")
	// new clones the origin's ref
	n2 := v.New(first, Right)
	if v.Content(n2) != "doc" {
		t.Fatalf("new did not clone ref: got %q want doc", v.Content(n2))
	}
	// explicit ref overrides
	n3 := v.New(first, Right, "other")
	if v.Content(n3) != "other" {
		t.Fatalf("new with ref: got %q want other", v.Content(n3))
	}
	// Get finds every tile carrying a ref
	got := v.Get("doc", true)
	if len(got) != 2 {
		t.Fatalf("Get(doc) = %v, want 2 tiles (first + n2)", got)
	}
	v.Set(n2, "doc2")
	if len(v.Get("doc", true)) != 1 {
		t.Fatalf("after Set, Get(doc) should be 1")
	}
}

// ---- per-axis caret re-home on insert/split/close ----

func TestCaretPerAxisRehome(t *testing.T) {
	v := newVP(300, 200)
	a, b, c := v.newLeaf("a"), v.newLeaf("b"), v.newLeaf("c")
	for _, l := range []*node{a, b, c} {
		l.minW, l.minH = 20, 20
	}
	// vertical column: closing renegotiates Y, must keep caret.x
	v.setRoot(v.grp(TTB, a, b, c))
	v.ensure()
	v.caretX = 37
	v.caretY = b.rect.Y + b.rect.H/2
	v.Close(b.handle) // focus moves to c; y renegotiated, x kept
	if !near(v.caretX, 37) {
		t.Fatalf("close reset caret.x to %v, want 37 kept", v.caretX)
	}
}

// ---- nested tab odometer: fully-right then walk back cycles the inner ----

func TestTabOdometerWalkBack(t *testing.T) {
	v := newVP(400, 200)
	// O is a stack whose tab0 is a split(SB, w18) and tab1 is a plain leaf w8
	sb := v.grp(LTR, v.newLeaf("s1"), v.newLeaf("s2"), v.newLeaf("s3"))
	selectChild(sb, sb.children[0])
	w18 := v.newLeaf("w18")
	split := v.grp(TTB, sb, w18)
	w8 := v.newLeaf("w8")
	o := v.grp(LTR, split, w8)
	selectChild(o, split) // O is a stack showing the split (SB visible)
	focus := v.newLeaf("F")
	v.setRoot(v.grp(LTR, o, focus))

	// walk fully right: O to its last tab (w8), SB to its last
	selectChild(o, w8)
	selectChild(sb, sb.children[2])
	v.visCur = nil
	v.touch()
	v.ensure()
	// walk back through the loose odometer (focus F is not in any stack): the
	// baton enters O (at w8), then must descend and cycle SB rather than getting
	// stuck on the container.
	v.TabPrior(focus.handle) // enters O -> reveals split, SB at its far tab
	first := selectedChild(sb).ref
	v.TabPrior(focus.handle) // must step SB (the inner), not the outer
	second := selectedChild(sb).ref
	if first == second {
		t.Fatalf("odometer stuck on the outer: SB stayed on %q", first)
	}
}

// ---- focus-inside tab cycle wraps the outermost, not the innermost ----

func TestTabCycleWrapsOutermost(t *testing.T) {
	v := newVP(400, 200)
	inner := v.grp(LTR, v.newLeaf("i0"), v.newLeaf("i1"))
	selectChild(inner, inner.children[1]) // inner at its last tab
	outer := v.grp(LTR, v.newLeaf("o0"), inner)
	selectChild(outer, inner) // outer at its last tab (the inner)
	v.setRoot(v.grp(LTR, outer, v.newLeaf("side")))
	// focus on i1, everything at its last tab; forward should wrap the OUTER to o0
	dest := v.TabNext(inner.children[1].handle)
	if v.tile(dest) == nil || v.tile(dest).ref != "o0" {
		got := ""
		if n := v.tile(dest); n != nil {
			got = n.ref
		}
		t.Fatalf("wrap landed on %q, want o0 (outermost wraps, not inner)", got)
	}
}

// TestSplitAddsTileAndSubdivides guards a regression where newGroup eagerly
// reparented its kids, so replaceNode(s, newGroup(o, s, nl)) read s's parent
// AFTER it had been pointed at the new group — building a self-cycle that
// dropped the new tile and later hung the parent-walk in New. Split must add a
// real tile, and a follow-on directional New must subdivide without hanging.
func TestSplitAddsTileAndSubdivides(t *testing.T) {
	v, first := NewViewport(720, 420)
	v.Set(first, "win1")
	right := v.Split(first, Right)
	if got := len(v.Tiles()); got != 2 {
		t.Fatalf("Split(Right) should yield 2 tiles, got %d", got)
	}
	if v.Content(right) != "win1" {
		t.Fatalf("Split should clone the origin ref, got %q", v.Content(right))
	}
	down := v.New(right, Down) // used to hang on the self-cycle
	if got := len(v.Tiles()); got != 3 {
		t.Fatalf("New(Down) after Split should yield 3 tiles, got %d", got)
	}
	if down == 0 || v.Content(down) != "win1" {
		t.Fatalf("New should clone the origin ref, got handle=%d ref=%q", down, v.Content(down))
	}
}
