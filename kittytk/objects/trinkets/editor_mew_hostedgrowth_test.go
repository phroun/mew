//go:build mew

package trinkets

import (
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/mew"
)

// hostedGrowthProbe drives the hosted-terminal loop the way the SDL build
// does — feed the child bytes, paint it through a REAL graphical painter,
// repeat — and reports how much heap the run retains per iteration.
//
// parented selects the one variable under test: with a parent chain up to a
// GraphicalFrameProvider, FindGraphicalFrames(child) is true and the child
// takes the pixel path (paintGraphical, the gfx input surface, the early
// return in updateTerminalSize). Orphaned, it takes the cell path.
func hostedGrowthProbe(t *testing.T, parented bool, iters int) (perIter int64, cols, rows int) {
	return hostedGrowthProbeWidth(t, parented, iters, 78)
}

func hostedGrowthProbeWidth(t *testing.T, parented bool, iters, guestCols int) (perIter int64, cols, rows int) {
	t.Helper()

	b, err := raster.NewScaled(800, 600, 2)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	if tm, ok := interface{}(b).(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
		defer core.SetTextMeasurer(nil)
	}
	b.SetFontSize(12)
	p := core.NewPainter(b)
	if !p.Graphical() {
		t.Skip("painter not graphical")
	}

	e := NewEditor()
	if parented {
		gp := &gfxFrameParent{}
		gp.TrinketBase = *core.NewTrinketBase()
		gp.Init(gp)
		e.SetParent(gp)
	}
	e.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 50})

	e.terminalOpen("pty1", 80, 24)
	e.terminalPlace([]mew.TerminalSurface{{
		ID: "pty1", Col: 1, Row: 1, Width: 80, Height: 24,
		ClipCol: 1, ClipRow: 1, ClipWidth: 80, ClipHeight: 24,
	}})

	// A guest's full-screen repaint: home, then 24 rows of text with an SGR
	// change per row. This is what an inner mew emits on every keystroke.
	frame := []byte("\x1b[H")
	for r := 0; r < 24; r++ {
		frame = append(frame, []byte(fmt.Sprintf("\x1b[%d;1H\x1b[3%dm", r+1, r%8))...)
		for c := 0; c < guestCols; c++ {
			frame = append(frame, byte('a'+(r+c)%26))
		}
	}

	// Warm up: first paints allocate caches that are not growth.
	for i := 0; i < 20; i++ {
		e.terminalFeed("pty1", frame)
		e.paintTerminalSurfaces(p)
	}
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < iters; i++ {
		e.terminalFeed("pty1", frame)
		e.paintTerminalSurfaces(p)
	}
	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	e.termMu.Lock()
	s := e.termSurfaces["pty1"]
	cols, rows = s.term.cols, s.term.rows
	e.termMu.Unlock()

	// Keep e alive across the measurement.
	runtime.KeepAlive(e)
	return (int64(after.HeapAlloc) - int64(before.HeapAlloc)) / int64(iters), cols, rows
}

// The hosted child must not retain memory per frame. A terminal that has
// settled at a fixed size, fed a fixed frame and painted, should hold steady:
// anything that grows without bound here is the runaway.
func TestHostedTerminalDoesNotGrowPerFrame(t *testing.T) {
	for _, parented := range []bool{false, true} {
		name := "orphaned"
		if parented {
			name = "parented"
		}
		t.Run(name, func(t *testing.T) {
			per, cols, rows := hostedGrowthProbe(t, parented, 200)
			t.Logf("%s: %+d bytes retained per frame, grid settled at %dx%d", name, per, cols, rows)
			if per > 4096 {
				t.Errorf("%s: retains %d bytes per frame -- unbounded growth", name, per)
			}
		})
	}
}

// THE RUNAWAY, measured. mew opens a session at the viewport's cell width and
// resizes the PTY to it (ptyPlace -> sess.Resize(s.Width)), so a guest placed
// in an 80-cell rectangle is told it has 80 columns. On the graphical path the
// child spends gfxScrollbarLane of that rectangle on its scrollbar and renders
// 78. Every full-width line the guest draws then WRAPS, a wrapped line
// SCROLLS, and a full-screen repaint that should have overwritten the screen
// in place pushes a screenful into scrollback instead — every frame, without
// bound.
//
// Here the guest writes the width it was told into a narrower grid, and the
// heap climbs per frame. The fix is not to make this case cheap; it is to stop
// lying to the guest (see TestHostedTerminalDeclaresItsSettledGrid).
func TestGuestWiderThanItsGridGrowsWithoutBound(t *testing.T) {
	per, cols, _ := hostedGrowthProbeWidth(t, true, 200, 80)
	t.Logf("a guest writing 80 columns into a %d-column grid retains %+d bytes per frame", cols, per)
	if cols >= 80 {
		t.Skip("the grid is not narrower than the guest here; nothing wraps")
	}
	if per < 4096 {
		t.Errorf("expected unbounded growth from the width mismatch, got %d bytes per frame", per)
	}
}

// So the display DECLARES the grid it settled on, and mew resizes the child
// process to that rather than to the rectangle. Nothing else can work the
// number out: it follows from this terminal's own font metrics and its own
// chrome, neither of which mew can see.
func TestHostedTerminalDeclaresItsSettledGrid(t *testing.T) {
	b, err := raster.NewScaled(800, 600, 2)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	if tm, ok := interface{}(b).(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
		defer core.SetTextMeasurer(nil)
	}
	b.SetFontSize(12)
	p := core.NewPainter(b)
	if !p.Graphical() {
		t.Skip("painter not graphical")
	}

	gp := &gfxFrameParent{}
	gp.TrinketBase = *core.NewTrinketBase()
	gp.Init(gp)
	e := NewEditor()
	e.SetParent(gp)
	e.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 50})

	e.terminalOpen("pty1", 80, 24)
	e.terminalPlace([]mew.TerminalSurface{{
		ID: "pty1", Col: 1, Row: 1, Width: 80, Height: 24,
		ClipCol: 1, ClipRow: 1, ClipWidth: 80, ClipHeight: 24,
	}})
	e.paintTerminalSurfaces(p)

	e.termMu.Lock()
	s := e.termSurfaces["pty1"]
	cols, rows := s.term.cols, s.term.rows
	e.termMu.Unlock()

	got := e.takeTermGrids()
	want := fmt.Sprintf("terminal_grid %q, %d, %d", "pty1", cols, rows)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("declared %v, want [%s] — the grid the child actually renders", got, want)
	}

	// An unchanged grid is not re-declared: every declaration resizes the
	// child process, and resizing it once per frame is its own runaway.
	e.paintTerminalSurfaces(p)
	if again := e.takeTermGrids(); len(again) != 0 {
		t.Errorf("re-declared an unchanged grid: %v", again)
	}
}

// The hosted child's origin must land where THIS EDITOR puts the column it
// covers, not where the host's cell grid happens to snap.
//
// A unit offset resolves through the backend's cell-snapped UnitToPxX; this
// editor lays its own text out at the terminal font's pitch, pixel-precise
// from its own origin. Where the two rates disagree — the ordinary case, a
// terminal font on a host grid — a child placed by unit offset alone sits a
// fraction of a cell off the text it covers, and the error grows across the
// row. paintTerminalSurfaces measures both and corrects the difference.
func TestHostedSurfaceLandsOnItsColumn(t *testing.T) {
	b, err := raster.NewScaled(800, 400, 2)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	if tm, ok := interface{}(b).(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
		defer core.SetTextMeasurer(nil)
	}
	b.SetFontSize(13) // a size whose cell need not divide the host grid
	p := core.NewPainter(b)
	if !p.Graphical() {
		t.Skip("painter not graphical")
	}

	gp := &gfxFrameParent{}
	gp.TrinketBase = *core.NewTrinketBase()
	gp.Init(gp)
	e := NewEditor()
	e.SetParent(gp)
	e.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 80})

	cw, ch := e.cellDims()
	ppu := p.PxPerUnitF()
	if cw <= 0 || ch <= 0 || ppu <= 0 {
		t.Skip("no usable cell metrics")
	}

	// Place a surface well along the row and down the screen, where any
	// per-cell drift has accumulated into something visible.
	const col, row = 37, 11
	e.terminalOpen("pty1", 20, 5)
	e.terminalPlace([]mew.TerminalSurface{{
		ID: "pty1", Col: col, Row: row, Width: 20, Height: 5,
		ClipCol: col, ClipRow: row, ClipWidth: 20, ClipHeight: 5,
	}})
	e.paintTerminalSurfaces(p)

	// Where this editor's own arithmetic puts that column/row...
	wantX := int(math.Round(float64(col-1) * float64(cw) * ppu))
	wantY := int(math.Round(float64(row-1) * float64(ch) * ppu))
	// ...versus where the child's painter actually anchors: the snapped unit
	// offset plus the residual paintTerminalSurfaces computed.
	unitX := core.Unit(col-1) * cw
	unitY := core.Unit(row-1) * ch
	gotX := p.UnitSpanPxX(0, unitX) + e.lastSurfaceOffsetX
	gotY := p.UnitSpanPxY(0, unitY) + e.lastSurfaceOffsetY

	if gotX != wantX {
		t.Errorf("column %d anchors at %d px, want %d (off by %d)", col, gotX, wantX, gotX-wantX)
	}
	if gotY != wantY {
		t.Errorf("row %d anchors at %d px, want %d (off by %d)", row, gotY, wantY, gotY-wantY)
	}
}

// THE CLIP INVARIANT: no edge of the clip may cut the content, at any zoom.
//
// The clip snaps to the host's cell grid; the content runs at the terminal
// pitch, origin-corrected. Wherever the snapped rate exceeds the terminal
// rate the content sits LEFT of and ABOVE the snapped unit position — so a
// clip anchored there shaves the first column and row ("-rw-r--" rendering
// as "rw-r--"), and one whose extent merely matches in units shaves the
// last. Every edge must be chosen for where it snaps: left/top at or before
// the content's pixel edge, right/bottom at or after.
//
// Checked across the zoom range because the two rates agree only at the
// default font size — the one place this bug cannot exist.
func TestHostedSurfaceClipNeverCutsContent(t *testing.T) {
	b, err := raster.NewScaled(1600, 800, 2)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	if tm, ok := interface{}(b).(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
		defer core.SetTextMeasurer(nil)
	}
	p := core.NewPainter(b)
	if !p.Graphical() {
		t.Skip("painter not graphical")
	}
	ppu0 := p.PxPerUnitF()
	if ppu0 <= 0 {
		t.Skip("no usable pixel rate")
	}

	for _, size := range []int{10, 11, 12, 13, 14, 16, 20} {
		b.SetFontSize(size)
		ppu := p.PxPerUnitF()

		gp := &gfxFrameParent{}
		gp.TrinketBase = *core.NewTrinketBase()
		gp.Init(gp)
		e := NewEditor()
		e.SetParent(gp)
		e.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 700, Height: 300})
		cw, ch := e.cellDims()
		if cw <= 0 || ch <= 0 {
			continue
		}
		pxX := func(cells int) int { return int(math.Round(float64(cells) * float64(cw) * ppu)) }
		pxY := func(cells int) int { return int(math.Round(float64(cells) * float64(ch) * ppu)) }

		for _, place := range []struct{ col, row, w, h int }{
			{1, 1, 80, 24},  // at the origin: zero residual, nothing may move
			{4, 2, 40, 10},  // the screenshot's shape: a gutter to the left
			{37, 11, 20, 5}, // far out, where the drift has accumulated
		} {
			id := fmt.Sprintf("pty-%d-%d", place.col, place.row)
			e.terminalOpen(id, place.w, place.h)
			e.terminalPlace([]mew.TerminalSurface{{
				ID: id, Col: place.col, Row: place.row, Width: place.w, Height: place.h,
				ClipCol: place.col, ClipRow: place.row, ClipWidth: place.w, ClipHeight: place.h,
			}})
			e.paintTerminalSurfaces(p)
			clip := e.lastClipFrame

			// Where the content's pixel edges are (the same arithmetic the
			// child paints by, from its corrected origin).
			cLeft, cRight := pxX(place.col-1), pxX(place.col-1+place.w)
			cTop, cBottom := pxY(place.row-1), pxY(place.row-1+place.h)
			// Where the clip's edges SNAP.
			kLeft := p.UnitSpanPxX(0, clip.X)
			kRight := p.UnitSpanPxX(0, clip.X+clip.Width)
			kTop := p.UnitSpanPxY(0, clip.Y)
			kBottom := p.UnitSpanPxY(0, clip.Y+clip.Height)

			if kLeft > cLeft {
				t.Errorf("size %d %v: clip left %d px cuts content at %d — first column shaved", size, place, kLeft, cLeft)
			}
			if kTop > cTop {
				t.Errorf("size %d %v: clip top %d px cuts content at %d — first row shaved", size, place, kTop, cTop)
			}
			if kRight < cRight {
				t.Errorf("size %d %v: clip right %d px cuts content at %d — last column shaved", size, place, kRight, cRight)
			}
			if kBottom < cBottom {
				t.Errorf("size %d %v: clip bottom %d px cuts content at %d — last row shaved", size, place, kBottom, cBottom)
			}

			// Over-reveal stays bounded: within one host cell + the pad on
			// each side, not swallowing the neighbours.
			cellPxX := p.UnitSpanPxX(0, core.Unit(core.DefaultCellMetrics().CellWidth))
			slack := cellPxX + 4
			if cLeft-kLeft > slack || kRight-cRight > slack {
				t.Errorf("size %d %v: clip [%d,%d] overshoots content [%d,%d] by more than a host cell",
					size, place, kLeft, kRight, cLeft, cRight)
			}
		}
	}
}

// snapCover's contract, both directions, on the real snapping.
func TestSnapCoverFindsCoveringEdges(t *testing.T) {
	b, err := raster.NewScaled(1600, 400, 2)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	p := core.NewPainter(b)
	ppu := p.PxPerUnitF()
	if ppu <= 0 {
		t.Skip("no usable pixel rate")
	}
	for _, target := range []int{0, 33, 100, 517, 1201} {
		for _, start := range []core.Unit{0, 10, 50, 300} {
			lo := snapCover(p.UnitSpanPxX, start, target, ppu, false)
			if got := p.UnitSpanPxX(0, lo); got > target {
				t.Errorf("floor(start=%d, target=%d): snapped %d > target", start, target, got)
			}
			hi := snapCover(p.UnitSpanPxX, start, target, ppu, true)
			if got := p.UnitSpanPxX(0, hi); got < target {
				t.Errorf("ceil(start=%d, target=%d): snapped %d < target", start, target, got)
			}
		}
	}
	// Degenerate ppu: hands back the start rather than walking.
	if got := snapCover(p.UnitSpanPxX, 42, 99, 0, true); got != 42 {
		t.Errorf("zero ppu -> %d, want the start 42", got)
	}
}

// mew's mouse wire carries CELLS, so a hosted child's events used to land on
// cell centers — a scrollbar scrub lurched a cell at a time while the SDL
// pointer moved by pixels. The host is the one place the precise pointer
// exists (it passes through on its way to being encoded), so it remembers,
// and the round trip uses the memory when it still agrees with the cell the
// wire carried.
func TestPreciseHostedPointer(t *testing.T) {
	cw, ch := core.Unit(7), core.Unit(14)

	// Surface at col 3, row 2; wire reports cell (5, 4) — child-local cell
	// (3, 3). A precise pointer inside that same cell is used verbatim.
	ev := mew.TerminalMouse{Col: 5, Row: 4}
	px := core.Unit(3-1)*cw + core.Unit(2-1)*0 + 2*cw + 3 // editor units: into child cell col 3
	py := core.Unit(2-1)*ch + 2*ch + 5
	lx, ly, ok := preciseHostedLocal(px, py, true, 3, 2, cw, ch, ev)
	if !ok {
		t.Fatal("a consistent precise pointer was rejected")
	}
	if int(lx/cw)+1 != 3 || int(ly/ch)+1 != 3 {
		t.Errorf("precise local (%d,%d) is not inside child cell (3,3)", lx, ly)
	}
	// And it keeps its SUB-CELL part — the whole point.
	if lx == core.Unit(2)*cw+cw/2 && ly == core.Unit(2)*ch+ch/2 {
		t.Error("precise pointer collapsed to the cell center")
	}

	// A pointer in a DIFFERENT cell than the wire's is stale: fall back.
	if _, _, ok := preciseHostedLocal(px+5*cw, py, true, 3, 2, cw, ch, ev); ok {
		t.Error("a pointer disagreeing with the wire's cell must be rejected")
	}
	// No memory at all: fall back.
	if _, _, ok := preciseHostedLocal(px, py, false, 3, 2, cw, ch, ev); ok {
		t.Error("an invalid pointer must be rejected")
	}
}
