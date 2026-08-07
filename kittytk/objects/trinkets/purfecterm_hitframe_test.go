package trinkets

import (
	"math"
	"strconv"
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

	for _, c := range zoomCases() {
		applyZoom(term, c)
		wPx, hPx, _ := term.gfxPixelFrame()
		// A pointer at the widget's far edges must land on the frame's far
		// edges — the same numbers the lanes are placed against.
		px, py := term.gfxPointerPx(640, 384)
		if math.Abs(px-wPx) > 1 {
			t.Errorf("%s: pointer at the right edge maps to %v, but the paint frame ends at %v",
				c.name, px, wPx)
		}
		if math.Abs(py-hPx) > 1 {
			t.Errorf("%s: pointer at the bottom maps to %v, but the paint frame ends at %v",
				c.name, py, hPx)
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

	for _, c := range zoomCases() {
		applyZoom(term, c)
		ppu := c.ppu
		track, thumb, _, _, _, ok := term.vScrollGeometry()
		if !ok {
			t.Fatalf("%s: no vertical geometry", c.name)
		}
		// Convert the PAINTED thumb centre back to pointer units and ask the
		// hit test about it. If the two frames differ, this misses.
		ux := term.gfxUnitForPx(thumb.X + thumb.W/2)
		_, uy := term.gfxUnitsForPx(0, thumb.Y+thumb.H/2)
		if !term.overScrollLane(ux, uy) {
			t.Errorf("%s: the painted thumb centre is not inside the lane hit box", c.name)
		}
		gx, gy := term.gfxPointerPx(ux, uy)
		if !thumb.contains(gx, gy) {
			t.Errorf("%s: painted thumb (x=%v w=%v) does not contain its own centre mapped back (%v)",
				c.name, thumb.X, thumb.W, gx)
		}
		// A point a full cell to the LEFT of the track is not on the lane.
		if term.overScrollLane(term.gfxUnitForPx(track.X-2*ppu), uy) {
			t.Errorf("%s: a point left of the track was accepted as lane", c.name)
		}
	}
}

// zoomCase is a real zoom level: the renderer's ppu together with the widget's
// SNAPPED device extent, which is what the outer system actually hands out and
// is NOT bounds*ppu. Their divergence is the whole bug — at font_size 25
// (thirteen zoom-plus presses from 12, at scale 2) it reaches a full lane
// width at the right-hand edge, so the lane's hit box misses it entirely.
type zoomCase struct {
	name    string
	ppu     float64
	vpWpx   float64
	vpHpx   float64
	boundsW core.Unit
	boundsH core.Unit
}

func zoomCases() []zoomCase {
	const w, h = core.Unit(640), core.Unit(384)
	out := []zoomCase{}
	for _, fs := range []int{12, 13, 20, 24, 25, 26} {
		ppu := 2.0 * float64(fs) / 12.0
		// The backend snaps a span to whole cells: the extent is a whole
		// number of cell widths, which is where it parts company with W*ppu.
		cellPx := math.Round(8 * ppu)
		cols := math.Floor(float64(w) * ppu / cellPx)
		vpW := cols * cellPx
		rowPx := math.Round(16 * ppu)
		rows := math.Floor(float64(h) * ppu / rowPx)
		vpH := rows * rowPx
		out = append(out, zoomCase{
			name:    "font_size=" + strconv.Itoa(fs),
			ppu:     ppu,
			vpWpx:   vpW,
			vpHpx:   vpH,
			boundsW: w, boundsH: h,
		})
	}
	return out
}

// applyZoom sets the cached paint state exactly as paintGraphical would. The
// lanes and pointer share ONE frame — bounds*ppu, the rate the cells paint at —
// so only ppu and the bounds are needed; the old snapped-extent/hitK scaling is
// gone (its divergence from ppu was the drift this test guards).
func applyZoom(t *PurfecTerm, c zoomCase) {
	t.SetBounds(core.UnitRect{Width: c.boundsW, Height: c.boundsH})
	t.gfx.ppu = c.ppu
}

// gfxUnitForPx / gfxUnitsForPx invert gfxPointerPx, so a test can ask about a
// PAINTED pixel using the coordinates the toolkit would deliver.
func (t *PurfecTerm) gfxUnitForPx(px float64) core.Unit {
	x, _ := t.gfxUnitsForPx(px, 0)
	return x
}

func (t *PurfecTerm) gfxUnitsForPx(px, py float64) (core.Unit, core.Unit) {
	_, _, ppu := t.gfxPixelFrame()
	if ppu <= 0 {
		ppu = 1
	}
	// Inverse of gfxPointerPx, which now maps at ppu (the content rate) with no
	// hitKX — see the frame invariant the pointer/lane/paint all share.
	return core.Unit(px / ppu), core.Unit(py / ppu)
}
