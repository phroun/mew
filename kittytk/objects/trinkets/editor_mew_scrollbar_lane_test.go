//go:build mew

package trinkets

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/mew"
)

// THE LANE-IN-CLIP INVARIANT: a hosted PTY paints its own cell pitch into a
// clip the editor cuts at that same pitch. Its vertical scrollbar lane must
// land INSIDE that clip at every zoom. It did not: sized to the host grid's
// cell-snapped span, the child's viewport overshot its pitch by a fraction that
// grew across the row, so the grid gained phantom columns and the lane snapped
// clean past the clip — no bar showed, and no reserved column either. Lockstep
// pitch (SetLockstepPitch, set on every hosted child) ties the viewport to the
// pitch the clip guarantees visible. Checked across the zoom range, where the
// two rates part company; integer zoom (size 12) is where they coincide and the
// bug was invisible.
func TestHostedLaneLandsInsideClip(t *testing.T) {
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

	for _, size := range []int{12, 13, 14, 20, 25} {
		b.SetFontSize(size)
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
		const col, row, w, h = 4, 2, 60, 12
		e.terminalOpen("pty1", w, h)
		e.terminalPlace([]mew.TerminalSurface{{
			ID: "pty1", Primary: true, Col: col, Row: row, Width: w, Height: h,
			ClipCol: col, ClipRow: row, ClipWidth: w, ClipHeight: h,
		}})
		// Scrollback so the vertical lane exists.
		var big []byte
		for i := 0; i < 400; i++ {
			big = append(big, []byte(fmt.Sprintf("line %d\r\n", i))...)
		}
		e.terminalFeed("pty1", big)
		e.paintTerminalSurfaces(p)

		e.termMu.Lock()
		st := e.termSurfaces["pty1"]
		e.termMu.Unlock()
		child := st.term
		track, _, _, _, _, ok := child.vScrollGeometry()
		if !ok {
			t.Errorf("size %d: no vertical geometry — the scrollback lane vanished", size)
			continue
		}
		// The clip's visible right edge, in the same render pixels the lane lives
		// in. The lane's right edge must not cross it, or the bar is off-screen.
		clip := e.lastClipFrame
		clipWidthPx := float64(p.UnitSpanPxX(0, clip.X+clip.Width) - p.UnitSpanPxX(0, clip.X))
		laneRight := track.X + track.W
		if laneRight > clipWidthPx+1 {
			t.Errorf("size %d: lane right edge %.1f is past the clip's visible width %.1f — scrollbar off-screen",
				size, laneRight, clipWidthPx)
		}
		// And the lane must still be a lane: hard against the right, one lane
		// thick, not collapsed inward by more than rounding.
		if track.X < clipWidthPx-2*track.W {
			t.Errorf("size %d: lane left %.1f sits well inside the clip %.1f — not anchored to the edge",
				size, track.X, clipWidthPx)
		}
		// The grid is a pure function of geometry — no phantom columns from an
		// overshot viewport. 60 placed minus the reserved lane is a stable count
		// across zoom, not a climbing one.
		settledCols, _ := child.terminal.Buffer().GetSize()
		if settledCols > w {
			t.Errorf("size %d: grid settled at %d columns, wider than the %d it was placed in — phantom columns",
				size, settledCols, w)
		}
	}
}
