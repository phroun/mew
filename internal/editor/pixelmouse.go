package editor

import (
	"strconv"
	"strings"
)

// Pixel-precise mouse via SGR-Pixels (DECSET ?1016). When the terminal supports
// it, mouse reports arrive in PIXELS instead of cells; mew converts each to a
// cell plus a sub-cell horizontal offset (permille) so an INSERT-mode click
// lands the caret on the NEAREST cell edge rather than always just before the
// clicked character. Overwrite mode keeps the classic before-the-character
// landing. It works on any terminal that speaks ?1016 (xterm, kitty, Ghostty,
// foot, …); one that ignores the handshake simply never activates and mouse
// stays cell-resolution — a graceful, no-op fallback.
//
// Handshake — every reply arrives as a key event, exactly like the bidi probe's
// CPR (surfaced by direct-key-handler):
//  1. startup: DECRQM ?1016  → CSI ? 1016 $ p
//  2. reply says recognised  → query cell size: CSI 16 t
//  3. cell size (WinOp 6)    → enable pixels: CSI ? 1016 h, and go active
//
// The reports (WinOp:/DECRPM:) are the generic XTWINOPS/DECRPM surfacings; see
// the matching direct-key-handler cases.

const (
	pixelMouseIdle        = iota // not started
	pixelMouseAwaitDECRPM        // DECRQM sent, waiting for the reply
	pixelMouseAwaitCell          // recognised, waiting for the cell-size report
	pixelMouseActive             // enabled; reports are pixels
	pixelMouseUnsupported        // terminal has no ?1016
)

type pixelMouseState struct {
	phase int
	cellW int // cell width in report pixels
	cellH int // cell height in report pixels
}

// beginPixelMouseProbe asks the terminal whether it speaks SGR-Pixels (?1016).
// Called once, where mouse reporting is enabled.
func (e *Editor) beginPixelMouseProbe() {
	if !e.realTerminal || e.pixelMouse.phase != pixelMouseIdle {
		return
	}
	e.pixelMouse.phase = pixelMouseAwaitDECRPM
	e.Renderer.EmitRaw("\x1b[?1016$p") // DECRQM: is ?1016 recognised?
}

// handlePixelMouseReply consumes the "DECRPM:" / "WinOp:" report keys that drive
// the ?1016 handshake. Reports whether the key was one of ours (consumed, never
// typed).
func (e *Editor) handlePixelMouseReply(key string) bool {
	switch {
	case strings.HasPrefix(key, "DECRPM:"):
		mode, status := splitReportPair(strings.TrimPrefix(key, "DECRPM:"))
		if mode == 1016 && e.pixelMouse.phase == pixelMouseAwaitDECRPM {
			// status 1=set, 2=reset → recognised; 0=not recognised.
			if status == 1 || status == 2 {
				e.pixelMouse.phase = pixelMouseAwaitCell
				e.Renderer.EmitRaw("\x1b[16t") // ask for the cell pixel size
			} else {
				e.pixelMouse.phase = pixelMouseUnsupported
			}
		}
		return true
	case strings.HasPrefix(key, "WinOp:"):
		ps, a, b := splitReportTriple(strings.TrimPrefix(key, "WinOp:"))
		if ps == 6 && a > 0 && b > 0 { // CSI 6 ; height ; width t
			e.pixelMouse.cellH, e.pixelMouse.cellW = a, b
			if e.pixelMouse.phase == pixelMouseAwaitCell {
				e.pixelMouse.phase = pixelMouseActive
				e.Renderer.EmitRaw("\x1b[?1016h") // switch reports to pixels
			}
		}
		return true
	}
	return false
}

// pixelMouseIsActive reports whether mouse reports currently arrive in pixels.
func (e *Editor) pixelMouseIsActive() bool {
	return e.pixelMouse.phase == pixelMouseActive &&
		e.pixelMouse.cellW > 0 && e.pixelMouse.cellH > 0
}

// pixelToCell converts a 1-based pixel coordinate pair into 1-based cell
// coordinates plus the pointer's sub-cell horizontal offset in permille
// (0..999, where ≥500 is the right half). Returns subX=-1 if no cell size.
func (e *Editor) pixelToCell(px, py int) (cx, cy, subX int) {
	w, h := e.pixelMouse.cellW, e.pixelMouse.cellH
	if w <= 0 || h <= 0 {
		return px, py, -1
	}
	if px < 1 {
		px = 1
	}
	if py < 1 {
		py = 1
	}
	cx = (px-1)/w + 1
	cy = (py-1)/h + 1
	subX = ((px - 1) % w) * 1000 / w
	return cx, cy, subX
}

func splitReportPair(s string) (a, b int) {
	p := strings.SplitN(s, ";", 2)
	if len(p) == 2 {
		a, _ = strconv.Atoi(strings.TrimSpace(p[0]))
		b, _ = strconv.Atoi(strings.TrimSpace(p[1]))
	}
	return
}

func splitReportTriple(s string) (a, b, c int) {
	p := strings.SplitN(s, ";", 3)
	if len(p) >= 1 {
		a, _ = strconv.Atoi(strings.TrimSpace(p[0]))
	}
	if len(p) >= 2 {
		b, _ = strconv.Atoi(strings.TrimSpace(p[1]))
	}
	if len(p) >= 3 {
		c, _ = strconv.Atoi(strings.TrimSpace(p[2]))
	}
	return
}
