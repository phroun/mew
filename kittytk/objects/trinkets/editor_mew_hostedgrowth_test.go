//go:build mew

package trinkets

import (
	"fmt"
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
