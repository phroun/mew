package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// The outer-terminal pixel-mouse probe (SGR-Pixels, ?1016) enables ?1016 only
// once BOTH replies have arrived — DECRQM says the mode is settable AND CSI 16 t
// reports a cell pixel size — and the two race in either order. Until both land
// the backend must stay on cell coordinates and must not emit ?1016h.
func TestOuterPixelMouseProbeEnables(t *testing.T) {
	var tty bytes.Buffer
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
		hasMouse:   true,
		ttyOut:     &tty,
	}

	// First reply alone (mode recognized, size still unknown) must NOT enable.
	b.handleKey("DECRPM:1016;2") // 2 = reset (recognized, currently off)
	if b.pixelMouse {
		t.Fatal("enabled ?1016 with only the DECRPM reply, before the cell size")
	}
	if strings.Contains(tty.String(), "1016h") {
		t.Fatal("emitted ?1016h before both probe replies")
	}

	// Second reply completes the pair (Ps=6: height=16, width=8) → enable now.
	b.handleKey("WinOp:6;16;8")
	if !b.pixelMouse {
		t.Fatal("did not enable ?1016 after both probe replies arrived")
	}
	if b.outerCellW != 8 || b.outerCellH != 16 {
		t.Fatalf("outer cell size = %dx%d, want 8x16", b.outerCellW, b.outerCellH)
	}
	if !strings.Contains(tty.String(), "\033[?1016h") {
		t.Fatalf("did not emit ?1016h on enable; tty=%q", tty.String())
	}
}

// A DECRQM reply of "unrecognized" (0) or "permanently reset" (4) means ?1016
// cannot be turned on, so the backend stays on cells even with a known cell
// size. A DECRPM for a different mode must be ignored entirely.
func TestOuterPixelMouseProbeRefused(t *testing.T) {
	for _, pm := range []string{"0", "4"} {
		var tty bytes.Buffer
		b := &TUIBackend{
			metrics:    core.DefaultCellMetrics(),
			eventQueue: make(chan core.Event, 8),
			hasMouse:   true,
			ttyOut:     &tty,
		}
		b.handleKey("WinOp:6;16;8")
		b.handleKey("DECRPM:1016;" + pm)
		if b.pixelMouse {
			t.Fatalf("Pm=%s: enabled ?1016 though the mode is not settable", pm)
		}
	}

	// A DECRPM for an unrelated mode must not flip the ?1016 gate.
	b := &TUIBackend{metrics: core.DefaultCellMetrics(), eventQueue: make(chan core.Event, 8), hasMouse: true, ttyOut: &bytes.Buffer{}}
	b.handleKey("WinOp:6;16;8")
	b.handleKey("DECRPM:2004;1") // bracketed paste, nothing to do with pixels
	if b.outerPixelOK || b.pixelMouse {
		t.Fatal("a non-1016 DECRPM enabled the pixel path")
	}
}

// Under ?1016 a mouse report is a 1-based OUTER PIXEL: the integer cell divides
// out of the outer cell size and the remainder scales into a fraction of THIS
// backend's cell width, giving the sub-cell position. In the default cell mode
// the same report is a 1-based cell column mapping to its left edge.
func TestOuterPixelMouseCoordinateConversion(t *testing.T) {
	press := func(b *TUIBackend, pos, action string) core.MousePressEvent {
		t.Helper()
		b.handleKey(pos)
		b.handleKey(action)
		// The position report ahead of the press is the pointer arriving
		// there, and dispatches as a move; the press is the event behind it.
		var ev core.Event
		for drained := false; !drained; {
			select {
			case e := <-b.eventQueue:
				ev = e
			default:
				drained = true
			}
		}
		mp, ok := ev.(core.MousePressEvent)
		if !ok {
			t.Fatalf("%s: dispatched as %T, want MousePressEvent", action, ev)
		}
		return mp
	}

	// Cell mode: metrics 8x16, no pixel state. Mouse@3,2 → cell (2,1) left edge.
	cell := &TUIBackend{metrics: core.DefaultCellMetrics(), eventQueue: make(chan core.Event, 8)}
	if ev := press(cell, "Mouse@3,2", "MouseLeft"); ev.X != 16 || ev.Y != 16 {
		t.Fatalf("cell mode: X,Y = %d,%d, want 16,16", ev.X, ev.Y)
	}

	// Pixel mode: this backend's cells are 8x16 units; the OUTER terminal's
	// cells are 10x20 px. Mouse@26,51 → px (25,50):
	//   X: cell 25/10=2 → 16 units; frac 5 → 5*8/10 = 4 units → 20.
	//   Y: cell 50/20=2 → 32 units; frac 10 → 10*16/20 = 8 units → 40.
	px := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
		pixelMouse: true,
		outerCellW: 10,
		outerCellH: 20,
	}
	if ev := press(px, "Mouse@26,51", "MouseLeft"); ev.X != 20 || ev.Y != 40 {
		t.Fatalf("pixel mode: X,Y = %d,%d, want 20,40", ev.X, ev.Y)
	}

	// A drag carries its position embedded in the action key; it must take the
	// same pixel conversion. Mouse@ ... then a drag to px (5,5): cell 0, frac 5
	// → X 5*8/10=4, Y 5*16/20=4.
	px.handleKey("MouseDragLeft@6,6")
	select {
	case ev := <-px.eventQueue:
		mv, ok := ev.(core.MouseMoveEvent)
		if !ok || mv.X != 4 || mv.Y != 4 {
			t.Fatalf("pixel drag: %T X,Y = %d,%d, want move 4,4", ev, mv.X, mv.Y)
		}
	default:
		t.Fatal("pixel drag was dropped")
	}
}
