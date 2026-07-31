package editor

import "testing"

// pixelToCell splits a pixel coordinate into a 1-based cell and a sub-cell
// permille, with ≥500 meaning the right half (nearest-edge rounds forward).
func TestPixelToCell(t *testing.T) {
	e := &Editor{}
	e.pixelMouse = pixelMouseState{phase: pixelMouseActive, cellW: 10, cellH: 20}

	cases := []struct {
		px, py               int
		wantCX, wantCY, wSub int
	}{
		{1, 1, 1, 1, 0},     // top-left pixel: cell (1,1), left edge
		{5, 1, 1, 1, 400},   // left-ish of cell 1 → left half
		{6, 1, 1, 1, 500},   // exactly mid → counts as right half
		{10, 1, 1, 1, 900},  // right edge of cell 1
		{11, 1, 2, 1, 0},    // first pixel of cell 2
		{25, 41, 3, 3, 400}, // cell (3,3), left half
	}
	for _, c := range cases {
		cx, cy, sub := e.pixelToCell(c.px, c.py)
		if cx != c.wantCX || cy != c.wantCY || sub != c.wSub {
			t.Errorf("pixelToCell(%d,%d) = (%d,%d,%d), want (%d,%d,%d)",
				c.px, c.py, cx, cy, sub, c.wantCX, c.wantCY, c.wSub)
		}
	}

	// Inactive / no cell size → pass-through, sub -1.
	e.pixelMouse = pixelMouseState{}
	if cx, cy, sub := e.pixelToCell(7, 8); cx != 7 || cy != 8 || sub != -1 {
		t.Errorf("no cell size: got (%d,%d,%d), want (7,8,-1)", cx, cy, sub)
	}
}

// The ?1016 handshake advances only through recognised replies and lands active
// once a cell size arrives; an unrecognised DECRPM parks it unsupported.
func TestPixelMouseHandshake(t *testing.T) {
	// Exercise the state transitions that don't emit (Renderer is nil here).
	// Unrecognised → unsupported (no emit).
	e2 := &Editor{}
	e2.pixelMouse.phase = pixelMouseAwaitDECRPM
	if !e2.handlePixelMouseReply("DECRPM:1016;0") {
		t.Fatal("DECRPM reply should be consumed")
	}
	if e2.pixelMouse.phase != pixelMouseUnsupported {
		t.Fatalf("status 0 should mark unsupported, got phase %d", e2.pixelMouse.phase)
	}

	// A WinOp for a different Ps is consumed but changes nothing.
	e3 := &Editor{}
	e3.pixelMouse.phase = pixelMouseAwaitCell
	if !e3.handlePixelMouseReply("WinOp:8;40;120") {
		t.Fatal("WinOp reply should be consumed")
	}
	if e3.pixelMouse.phase != pixelMouseAwaitCell || e3.pixelMouse.cellW != 0 {
		t.Fatalf("non-cell WinOp should not activate, got phase %d cellW %d", e3.pixelMouse.phase, e3.pixelMouse.cellW)
	}

	// A non-report key is not ours.
	if e3.handlePixelMouseReply("MouseLeftPress") {
		t.Fatal("non-report key should not be consumed")
	}

	// A ?2048 in-band resize notification (WinOp:48;rows;cols;h;w) recomputes
	// the cached cell size from pixels/grid — the font-zoom case.
	e4 := &Editor{}
	e4.pixelMouse = pixelMouseState{phase: pixelMouseActive, cellW: 10, cellH: 20}
	if !e4.handlePixelMouseReply("WinOp:48;24;80;960;1600") {
		t.Fatal("WinOp:48 should be consumed")
	}
	if e4.pixelMouse.cellW != 20 || e4.pixelMouse.cellH != 40 { // 1600/80, 960/24
		t.Fatalf("resize should update cell size to 20x40, got %dx%d",
			e4.pixelMouse.cellW, e4.pixelMouse.cellH)
	}
}
