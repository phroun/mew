package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// stripeRecorder remembers the device-pixel hairlines a tree draws for its
// column dividers, so a test can count the ones standing on a boundary.
type stripeRecorder struct {
	core.RenderBackend
	stripes []struct{ x, y, w, h int }
}

// GraphicalMode forwards the wrapped backend's answer. Embedding the
// RenderBackend interface promotes only the methods IN it, and this is not one
// of them -- so without saying so the wrapper reads as a cell surface and the
// painter never takes the pixel path at all.
func (r *stripeRecorder) GraphicalMode() bool {
	gm, ok := r.RenderBackend.(core.GraphicalModer)
	return ok && gm.GraphicalMode()
}

func (r *stripeRecorder) FillRectPxAlpha(xPx, yPx, wPx, hPx int, cr, cg, cb uint8, alpha float64) {
	r.stripes = append(r.stripes, struct{ x, y, w, h int }{xPx, yPx, wPx, hPx})
	if tf, ok := r.RenderBackend.(core.TranslucentPixelFiller); ok {
		tf.FillRectPxAlpha(xPx, yPx, wPx, hPx, cr, cg, cb, alpha)
	}
}

// tallStripeXs is where the full-height hairlines were drawn, one entry each.
// A divider runs the whole grid; the edge fades are shorter columns and the
// rest of the painting is not a hairline at all.
func (r *stripeRecorder) tallStripeXs() []int {
	tallest := 0
	for _, s := range r.stripes {
		if s.w == 1 && s.h > tallest {
			tallest = s.h
		}
	}
	var out []int
	for _, s := range r.stripes {
		if s.w == 1 && s.h == tallest {
			out = append(out, s.x)
		}
	}
	return out
}

// frozenTree pins the first column outside the scrolling region, so there is
// a boundary between what moves and what does not.
func frozenTree(t *testing.T, graphical bool) *TreeView {
	t.Helper()
	return frozenTreeReading(t, graphical, core.DirLTR)
}

func frozenTreeReading(t *testing.T, graphical bool, dir core.Direction) *TreeView {
	t.Helper()
	tv := newColumnsTree(30, 10)
	tv.SetDirection(dir)
	if graphical {
		parent := &gfxSurface{smooth: true}
		parent.Panel = *NewPanel()
		parent.Init(parent)
		tv.SetParent(parent)
		tv.SetBounds(core.UnitRect{Width: 30 * 8, Height: 10 * 16})
	}
	tv.SetFitWidth(false)
	tv.SetKeyWidth(15 * cell)
	tv.SetFixedColumns(1, 0)
	return tv
}

// The boundary between the pinned columns and the ones that scroll is drawn as
// a DOUBLE rule, so what stays put reads as separate from what moves. An
// ordinary boundary between two columns keeps its single one.
func TestAFrozenBoundaryIsADoubleRuleInTheTUI(t *testing.T) {
	tv := frozenTree(t, false)
	lay := tv.columnLayout()

	var frozen, plain core.Unit = -1, -1
	for _, sp := range lay.spans {
		if !lay.divVisible(sp) {
			continue
		}
		if sp.divFrozen {
			frozen = sp.divX
		} else {
			plain = sp.divX
		}
	}
	if frozen < 0 {
		t.Fatal("no frozen boundary in a tree with a pinned column")
	}

	ink := newInk(t)
	tv.Paint(core.NewPainter(ink))

	at := func(x core.Unit, ch rune) int {
		n := 0
		for _, c := range ink.cells {
			if c.x == x && c.ch == ch {
				n++
			}
		}
		return n
	}
	if at(frozen, '║') == 0 {
		t.Errorf("the frozen boundary at x=%d is not drawn with a double rule", frozen)
	}
	if got := at(frozen, '│'); got != 0 {
		t.Errorf("the frozen boundary at x=%d also drew %d single rules", frozen, got)
	}
	if plain >= 0 {
		if at(plain, '│') == 0 {
			t.Errorf("the boundary between two columns at x=%d lost its single rule", plain)
		}
		if got := at(plain, '║'); got != 0 {
			t.Errorf("an ordinary column boundary at x=%d drew %d double rules", plain, got)
		}
	}
}

// A pixel surface has no glyph to swap: the hairline gets a second hairline a
// hairline's width away, which says the same thing in the same ink.
//
// Where the pair LANDS is the painter's arithmetic to answer -- a unit maps to
// a device pixel through the zoom and the anchor -- so what is checked here is
// the pattern: pinning a column adds exactly one hairline, and that hairline
// stands one pixel of gap from another.
func TestAFrozenBoundaryIsTwoHairlinesOnAPixelSurface(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	stripes := func(pinned int) []int {
		px, err := raster.New(400, 200)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		rec := &stripeRecorder{RenderBackend: px}
		tv := frozenTree(t, true)
		tv.SetFixedColumns(pinned, 0)
		tv.Paint(core.NewPainter(rec))
		return rec.tallStripeXs()
	}

	loose, frozen := stripes(0), stripes(1)
	if len(loose) == 0 {
		t.Fatal("a tree with columns drew no divider hairlines at all")
	}
	if got, want := len(frozen), len(loose)+1; got != want {
		t.Errorf("pinning a column drew %d hairlines against %d loose; want one more, not %d",
			got, len(loose), want)
	}

	pairs := func(xs []int) int {
		at := map[int]bool{}
		for _, x := range xs {
			at[x] = true
		}
		n := 0
		for _, x := range xs {
			if at[x+2] {
				n++
			}
		}
		return n
	}
	if got := pairs(frozen); got != 1 {
		t.Errorf("the pinned tree has %d hairlines with a companion two pixels away, want 1", got)
	}
	if got := pairs(loose); got != 0 {
		t.Errorf("a tree with nothing pinned already has %d paired hairlines", got)
	}
}

// The companion stands one step ALONG THE RUN from the boundary, so the pair
// sits the same way about it however the columns are ordered -- and never
// steps off the tree at the edge the run ends against.
func TestTheCompanionHairlineStandsAlongTheRun(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, tc := range []struct {
		dir  core.Direction
		step int
	}{
		{core.DirLTR, 2},
		{core.DirRTL, -2},
	} {
		stripes := func(pinned int) []int {
			px, err := raster.New(400, 200)
			if err != nil {
				t.Fatal(err)
			}
			core.SetTextMeasurer(px)
			rec := &stripeRecorder{RenderBackend: px}
			tv := frozenTreeReading(t, true, tc.dir)
			tv.SetFixedColumns(pinned, 0)
			tv.Paint(core.NewPainter(rec))
			return rec.tallStripeXs()
		}

		// Pinning adds exactly the companion, so whatever is new is it,
		// and whatever it stands beside is the boundary.
		was := map[int]bool{}
		for _, x := range stripes(0) {
			was[x] = true
		}
		var added []int
		for _, x := range stripes(1) {
			if !was[x] {
				added = append(added, x)
			}
		}
		if len(added) != 1 {
			t.Fatalf("%v: pinning a column added %d hairlines, want the one companion",
				tc.dir, len(added))
		}
		if !was[added[0]-tc.step] {
			t.Errorf("%v: the companion at %d stands beside no boundary %d pixels back along the run",
				tc.dir, added[0], tc.step)
		}
	}
}
