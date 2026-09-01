package window

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

// At scale 1.0 the kit metrics ARE the classic geometry: same row, same
// cells, the same font pointer — so every pre-kit painter's output is
// reproduced bit for bit.
func TestTitleBarMetricsIdentityAtScaleOne(t *testing.T) {
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	font := &core.Font{Name: "ui-text", Size: 12}
	tm := TitleBarMetricsFor(cell, font, true)
	if tm.RowH != 16 || tm.CellW != 8 || tm.ButtonW != 24 || tm.YOff != 0 {
		t.Errorf("identity broken: RowH=%v CellW=%v ButtonW=%v YOff=%v", tm.RowH, tm.CellW, tm.ButtonW, tm.YOff)
	}
	if tm.Font != font {
		t.Error("scale 1.0 did not keep the original font pointer")
	}
}

// At 0.7 the row and cells quantize UP on the frame denomination's unit
// grid (option (c) by explicit ruling): 0.7 of a 16-unit cell is 12/16,
// of an 8-unit column 6/8; the font drops to the rounded point size and
// the glyphs center in what remains of the row.
func TestTitleBarMetricsQuantizeOnUnitGrid(t *testing.T) {
	t.Cleanup(func() { core.SetTitleBarScale(1) })
	core.SetTitleBarScale(0.7)
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	font := &core.Font{Name: "ui-text", Size: 12}
	tm := TitleBarMetricsFor(cell, font, true)
	if tm.RowH != 12 {
		t.Errorf("RowH = %v, want ceil(0.7×16) = 12", tm.RowH)
	}
	if tm.CellW != 6 {
		t.Errorf("CellW = %v, want ceil(0.7×8) = 6", tm.CellW)
	}
	if tm.ButtonW != 18 {
		t.Errorf("ButtonW = %v, want 18", tm.ButtonW)
	}
	if tm.Font.Size != 8 {
		t.Errorf("font size = %d, want round(0.7×12) = 8", tm.Font.Size)
	}
	// The controls' glyph face stays the retro MONOSPACE: ui-term at the
	// bar's point size, never the proportional title face.
	if tm.Mono == nil || tm.Mono.Name != "ui-term" || tm.Mono.Size != 8 {
		t.Errorf("control mono face = %+v, want ui-term at 8pt", tm.Mono)
	}
	// Glyph box 16×8/12 ≈ 10.67 units in a 12-unit row: the half-gap
	// floors to 0 (rounding it up sat the text a unit too low).
	if tm.YOff != 0 {
		t.Errorf("YOff = %v, want 0 (floored half-gap)", tm.YOff)
	}
}

// Cell surfaces pin the scale to 1.0 whatever the knob says: a terminal
// cannot render seven tenths of a character cell.
func TestTitleBarMetricsPinCellSurfaces(t *testing.T) {
	t.Cleanup(func() { core.SetTitleBarScale(1) })
	core.SetTitleBarScale(0.7)
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	font := &core.Font{Name: "ui-text", Size: 12}
	tm := TitleBarMetricsFor(cell, font, false)
	if tm.Scale != 1 || tm.RowH != 16 || tm.CellW != 8 || tm.Font != font {
		t.Errorf("cell surface not pinned: Scale=%v RowH=%v CellW=%v", tm.Scale, tm.RowH, tm.CellW)
	}
}

// The geometry decode is one shared table: spot-check each quadrant of it
// and the delta at both step sizes.
func TestDecodeTitleGeometry(t *testing.T) {
	for _, c := range []struct {
		cmd            string
		dir            string
		resize, coarse bool
	}{
		{core.CmdWindowMoveFineLeft, "Left", false, false},
		{core.CmdWindowMoveDown, "Down", false, true},
		{core.CmdWindowSizeFineUp, "Up", true, false},
		{core.CmdWindowSizeRight, "Right", true, true},
	} {
		dir, resize, coarse, ok := DecodeTitleGeometry(c.cmd)
		if !ok || dir != c.dir || resize != c.resize || coarse != c.coarse {
			t.Errorf("%s: got (%s,%v,%v,%v)", c.cmd, dir, resize, coarse, ok)
		}
	}
	if _, _, _, ok := DecodeTitleGeometry(core.CmdTrinketCancel); ok {
		t.Error("a non-geometry command decoded as geometry")
	}
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	if dx, dy := TitleGeometryDelta("Right", false, cell); dx != 8 || dy != 0 {
		t.Errorf("fine right = (%v,%v), want (8,0)", dx, dy)
	}
	if dx, dy := TitleGeometryDelta("Down", true, cell); dx != 0 || dy != 64 {
		t.Errorf("coarse down = (%v,%v), want (0,64) — 4 rows", dx, dy)
	}
}

// The double-click tracker fires on the second press within 400ms and a
// cell, consumes on fire (no tripling), and treats a press a cell away as
// a fresh first click.
func TestDoubleClickTrackerConsumesOnFire(t *testing.T) {
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	var tr DoubleClickTracker
	if tr.Press(100, 50, cell) {
		t.Error("first press fired")
	}
	if !tr.Press(103, 52, cell) {
		t.Error("second press within a cell did not fire")
	}
	if tr.Press(103, 52, cell) {
		t.Error("third press fired (memory not consumed)")
	}
	// A press far away is a fresh first click...
	tr.Reset()
	if tr.Press(100, 50, cell) {
		t.Error("press after reset fired")
	}
	if tr.Press(200, 50, cell) {
		t.Error("press a full row away paired with the first")
	}
	// ...and stale timing never pairs.
	tr = DoubleClickTracker{at: time.Now().Add(-time.Second), x: 100, y: 50}
	if tr.Press(100, 50, cell) {
		t.Error("a second press one second later fired")
	}
}

// The kit's metrics run from contentBounds and the hit tests as well as
// the painters — several times per window per frame — so building them
// must not allocate. It used to build both faces on every call, putting
// that garbage straight on the frame path.
func TestTitleBarMetricsDoesNotAllocatePerCall(t *testing.T) {
	t.Cleanup(func() { core.SetTitleBarScale(1) })
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	font := &core.Font{Name: "ui-text", Size: 12}

	for _, scale := range []float64{1, 0.7} {
		core.SetTitleBarScale(scale)
		// Warm the face cache, then measure the steady state.
		TitleBarMetricsFor(cell, font, true)
		got := testing.AllocsPerRun(200, func() {
			tm := TitleBarMetricsFor(cell, font, true)
			if tm.RowH <= 0 || tm.Mono == nil {
				t.Fatal("metrics came back unusable")
			}
		})
		if got != 0 {
			t.Errorf("scale %v: %.1f allocations per call, want 0", scale, got)
		}
	}

	// Both faces are still correct after caching: the text face keeps the
	// caller's own font at 1.0, and the controls stay ui-term at the
	// bar's size when scaled.
	core.SetTitleBarScale(1)
	if tm := TitleBarMetricsFor(cell, font, true); tm.Font != font {
		t.Error("scale 1.0 no longer hands back the caller's own font pointer")
	}
	core.SetTitleBarScale(0.7)
	tm := TitleBarMetricsFor(cell, font, true)
	if tm.Font.Size != 8 || tm.Mono.Name != "ui-term" || tm.Mono.Size != 8 {
		t.Errorf("scaled faces wrong: text %+v mono %+v", tm.Font, tm.Mono)
	}
}
