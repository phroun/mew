//go:build mew

package trinkets

import (
	"fmt"
	"math"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/mew"
)

// A hosted PTY shown in tiles of different heights caches ONE gfx frame (the
// last tile painted), so a scrollbar drag/hover used that tile's track height no
// matter which tile it happened in — the thumb dragged as though the pane were
// the tall one while the short one was on screen. terminalMouse now restores the
// interacted tile's frame first (and pins it for a drag). Assert the child's
// track height follows the tile the pointer is in.
func TestHostedScrollbarFollowsInteractedTile(t *testing.T) {
	b, err := raster.NewScaled(1200, 800, 2)
	if err != nil {
		t.Skip(err)
	}
	if tm, ok := interface{}(b).(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
		defer core.SetTextMeasurer(nil)
	}
	b.SetFontSize(12)
	p := core.NewPainter(b)
	if !p.Graphical() {
		t.Skip("not graphical")
	}
	gp := &gfxFrameParent{}
	gp.TrinketBase = *core.NewTrinketBase()
	gp.Init(gp)
	e := NewEditor()
	e.SetParent(gp)
	e.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 700, Height: 500})
	cw, ch := e.cellDims()

	const primH, mirrH = 24, 10
	e.terminalOpen("pty1", 80, 24)
	e.terminalPlace([]mew.TerminalSurface{
		{ID: "pty1", Primary: true, Col: 1, Row: 1, Width: 40, Height: primH, ClipCol: 1, ClipRow: 1, ClipWidth: 40, ClipHeight: primH},
		{ID: "pty1", Primary: false, Col: 41, Row: 1, Width: 40, Height: mirrH, ClipCol: 41, ClipRow: 1, ClipWidth: 40, ClipHeight: mirrH},
	})
	var big []byte
	for i := 0; i < 400; i++ {
		big = append(big, []byte(fmt.Sprintf("line %d\r\n", i))...)
	}
	e.terminalFeed("pty1", big)
	e.paintTerminalSurfaces(p) // leaves the frame on the last-painted tile (the mirror)

	e.termMu.Lock()
	child := e.termSurfaces["pty1"].term
	e.termMu.Unlock()
	ppu := child.gfx.ppu

	wantPrim := math.Round(float64(core.Unit(primH)*ch) * ppu)
	wantMirr := math.Round(float64(core.Unit(mirrH)*ch) * ppu)

	// A hover in the PRIMARY (tall) tile must give the tall track.
	e.notePointer(core.Unit(20)*cw, core.Unit(12)*ch)
	e.terminalMouse("pty1", mew.TerminalMouse{Col: 20, Row: 12, Action: mew.TerminalMouseMotion})
	if got := child.gfx.vpHpx; math.Abs(got-wantPrim) > 1 {
		t.Errorf("hover in the tall primary used track height %.0f, want %.0f (%d rows)", got, wantPrim, primH)
	}

	// A hover in the MIRROR (short) tile must give the short track.
	e.notePointer(core.Unit(60)*cw, core.Unit(5)*ch)
	e.terminalMouse("pty1", mew.TerminalMouse{Col: 20, Row: 5, Action: mew.TerminalMouseMotion})
	if got := child.gfx.vpHpx; math.Abs(got-wantMirr) > 1 {
		t.Errorf("hover in the short mirror used track height %.0f, want %.0f (%d rows)", got, wantMirr, mirrH)
	}
}
