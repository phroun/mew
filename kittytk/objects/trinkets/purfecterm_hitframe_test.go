package trinkets

import (
	"math"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// The hit test and the paint must share ONE coordinate frame. Cells and lanes
// are placed by multiplying units by ppu and rounding each edge; a hit test
// that scales the pointer any other way disagrees by a fraction of a cell,
// which compounds across the row — worst at the far right, where the vertical
// scrollbar lives.
func TestPointerFrameMatchesPaintFrame(t *testing.T) {
	term := NewPurfecTerm()
	defer term.Close()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 384})
	term.Feed([]byte(strings.Repeat("line\r\n", 200)))

	for _, ppu := range []float64{1.0, 1.07, 0.94, 1.25, 1.6667} {
		term.gfx.ppu = ppu
		wPx, _, gotPPU := term.gfxPixelFrame()
		if gotPPU != ppu {
			t.Fatalf("frame ppu = %v, want %v", gotPPU, ppu)
		}
		// A pointer at the widget's right edge must land at the frame's right
		// edge — the same number the lane is placed against.
		px, _ := term.gfxPointerPx(640, 0)
		if math.Abs(px-wPx) > 1 {
			t.Errorf("ppu=%.4f: pointer at the right edge maps to %v, but the paint frame ends at %v",
				ppu, px, wPx)
		}
	}
}

// The vertical lane's hit test must accept exactly the pixels it is painted
// on, at every ppu — this is the hover/click box the user found drifting.
func TestVerticalLaneHitBoxMatchesItsPaint(t *testing.T) {
	term := NewPurfecTerm()
	defer term.Close()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 384})
	term.Feed([]byte(strings.Repeat("line\r\n", 200)))
	if term.Terminal().Buffer().GetMaxScrollOffset() == 0 {
		t.Skip("no scrollback")
	}

	for _, ppu := range []float64{1.0, 1.07, 0.94, 1.25, 1.6667} {
		term.gfx.ppu = ppu
		track, thumb, _, _, _, ok := term.vScrollGeometry()
		if !ok {
			t.Fatalf("ppu=%.4f: no vertical geometry", ppu)
		}
		// Convert the PAINTED thumb centre back to pointer units and ask the
		// hit test about it. If the two frames differ, this misses.
		ux := core.Unit((thumb.X + thumb.W/2) / ppu)
		uy := core.Unit((thumb.Y + thumb.H/2) / ppu)
		if !term.overScrollLane(ux, uy) {
			t.Errorf("ppu=%.4f: the painted thumb centre is not inside the lane hit box", ppu)
		}
		gx, gy := term.gfxPointerPx(ux, uy)
		if !thumb.contains(gx, gy) {
			t.Errorf("ppu=%.4f: painted thumb (x=%v w=%v) does not contain its own centre mapped back (%v)",
				ppu, thumb.X, thumb.W, gx)
		}
		// A point a full cell to the LEFT of the track is not on the lane.
		if term.overScrollLane(core.Unit((track.X-16)/ppu), uy) {
			t.Errorf("ppu=%.4f: a point left of the track was accepted as lane", ppu)
		}
	}
}
