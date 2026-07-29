//go:build mew

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	mew "github.com/phroun/mew"
)

// The cursor query and the mouse path must land on the SAME cell. Clicks
// arrive at the host's snapped unit rate while the terminal lays its grid out
// at the renderer's ppu; the mouse path scales by hitK to cross that gap, and
// the cursor query has to make the identical trip. When it did not, the
// mapping drifted as soon as the two rates diverged (any zoom), which wide
// targets survive and one-column ones do not — mew's reserved scrollbar column
// and its short link-button spans both stopped showing the arrow.
func TestPointerCellMatchesMousePath(t *testing.T) {
	e := NewEditor()
	defer e.Close()
	if e.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	e.SetBounds(core.UnitRect{Width: 640, Height: 384}) // 80x24 at 8x16

	cw, chh := e.cellDims()
	if cw <= 0 || chh <= 0 {
		t.Skip("no cell metrics")
	}

	for _, k := range []float64{1.0, 1.07, 0.94, 1.25} {
		e.gfx.hitKX, e.gfx.hitKY = k, k
		for _, cell := range []int{0, 1, 17, 40, 78, 79} {
			// A point in the middle of cell `cell` at the OUTER rate.
			x := core.Unit((float64(cell) + 0.5) * float64(cw) / k)
			col, _, ok := e.pointerCell(x, core.Unit(float64(chh)/2/k))
			if !ok {
				t.Fatal("pointerCell should resolve with valid metrics")
			}
			if want := cell + 1; col != want {
				t.Errorf("hitK=%.2f cell %d: pointerCell col=%d, want %d", k, cell, col, want)
			}
		}
	}
}

// With a region published, the cursor is the I-beam over text, the arrow over
// a button span inside it, and the arrow outside the region entirely — which
// is where mew's reserved scrollbar column sits, since the published width is
// the content width with that column already excluded.
func TestCursorShapeHonoursRegionAndArrows(t *testing.T) {
	e := NewEditor()
	defer e.Close()
	if e.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	e.SetBounds(core.UnitRect{Width: 640, Height: 384})
	cw, chh := e.cellDims()
	if cw <= 0 || chh <= 0 {
		t.Skip("no cell metrics")
	}

	// Text spans columns 1..78 on rows 1..20; column 79 is mew's scrollbar
	// (outside the region). A button occupies columns 10..14 of row 3.
	e.pointerRegionMu.Lock()
	e.pointerRegion = [4]int{1, 1, 78, 20}
	e.pointerArrows = []mew.PointerArrowSpan{{Row: 3, Col: 10, Width: 5}}
	e.pointerRegionMu.Unlock()

	at := func(col, row int) core.CursorShape {
		x := core.Unit((float64(col) - 0.5) * float64(cw))
		y := core.Unit((float64(row) - 0.5) * float64(chh))
		return e.CursorShapeAt(x, y)
	}
	if got := at(5, 5); got != core.CursorText {
		t.Errorf("over plain text: cursor %v, want text I-beam", got)
	}
	if got := at(12, 3); got != core.CursorDefault {
		t.Errorf("over a link button: cursor %v, want arrow", got)
	}
	if got := at(9, 3); got != core.CursorText {
		t.Errorf("just left of the button: cursor %v, want text I-beam", got)
	}
	if got := at(79, 5); got != core.CursorDefault {
		t.Errorf("over the reserved scrollbar column: cursor %v, want arrow", got)
	}
	if got := at(5, 22); got != core.CursorDefault {
		t.Errorf("below the text region: cursor %v, want arrow", got)
	}
}

// A hosted pty terminal owns the cursor over its own surface: it has its own
// scrollbars and only it knows where their lanes fall. The Editor declares
// CursorShapesSubtree, so the descent stops at the Editor and never reaches
// the child — the delegation has to be explicit or the terminal's lanes show
// mew's I-beam.
func TestHostedTerminalOwnsItsCursor(t *testing.T) {
	e := NewEditor()
	defer e.Close()
	if e.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	e.SetBounds(core.UnitRect{Width: 640, Height: 384})
	cw, chh := e.cellDims()
	if cw <= 0 || chh <= 0 {
		t.Skip("no cell metrics")
	}

	// mew's text region covers the whole grid, so without delegation every
	// point below would report the I-beam.
	e.pointerRegionMu.Lock()
	e.pointerRegion = [4]int{1, 1, 80, 24}
	e.pointerRegionMu.Unlock()

	// Place a hosted terminal over cells (col 11..50, row 3..14).
	child := NewPurfecTerm()
	defer child.Close()
	child.SetBounds(core.UnitRect{Width: 40 * cw, Height: 12 * chh})
	e.termMu.Lock()
	e.termSurfaces = map[string]*termSurface{"pty1": {
		term: child,
		col:  11, row: 3, width: 40, height: 12,
		clipCol: 11, clipRow: 3, clipWidth: 40, clipHeight: 12,
	}}
	e.termMu.Unlock()

	at := func(col, row int) core.CursorShape {
		x := core.Unit((float64(col) - 0.5) * float64(cw))
		y := core.Unit((float64(row) - 0.5) * float64(chh))
		return e.CursorShapeAt(x, y)
	}

	// Outside the hosted surface: mew answers, and its region says text.
	if got := at(5, 5); got != core.CursorText {
		t.Errorf("outside the hosted surface: cursor %v, want mew's I-beam", got)
	}
	// Inside it, the child answers. With no scrollback its lanes are absent,
	// so it reports the text I-beam over its content — the point being that
	// the CHILD decided, not mew.
	tt, lx, ly, ok := e.hostedTermAt(
		core.Unit(19.5*float64(cw)), core.Unit(7.5*float64(chh)))
	if !ok {
		t.Fatal("a point inside the hosted surface should resolve to its terminal")
	}
	if tt != child {
		t.Fatal("resolved the wrong hosted terminal")
	}
	// The child-local coordinate is the point minus the surface origin.
	if wantX := core.Unit(9.5 * float64(cw)); lx != wantX {
		t.Errorf("child-local x = %v, want %v", lx, wantX)
	}
	if wantY := core.Unit(5.5 * float64(chh)); ly != wantY {
		t.Errorf("child-local y = %v, want %v", ly, wantY)
	}
	// And a point just outside the surface must NOT resolve to it.
	if _, _, _, ok := e.hostedTermAt(core.Unit(9.5*float64(cw)), core.Unit(7.5*float64(chh))); ok {
		t.Error("a point left of the hosted surface must not resolve to it")
	}
	if _, _, _, ok := e.hostedTermAt(core.Unit(19.5*float64(cw)), core.Unit(1.5*float64(chh))); ok {
		t.Error("a point above the hosted surface must not resolve to it")
	}
	_ = at
}
