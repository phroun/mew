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
				// Ask for the cell pixel size AND enable in-band resize
				// notifications (?2048) so a font zoom that keeps the same grid
				// still tells us the cell size changed. Both flow back as WinOp:.
				e.Renderer.EmitRaw("\x1b[16t\x1b[?2048h")
			} else {
				e.pixelMouse.phase = pixelMouseUnsupported
			}
		}
		return true
	case strings.HasPrefix(key, "WinOp:"):
		f := splitReportInts(strings.TrimPrefix(key, "WinOp:"))
		switch {
		case len(f) >= 3 && f[0] == 6: // CSI 6 ; height ; width t — cell pixel size
			if f[1] > 0 && f[2] > 0 {
				e.pixelMouse.cellH, e.pixelMouse.cellW = f[1], f[2]
				e.pixelMouseGoActive()
			}
		case len(f) >= 5 && f[0] == 48: // CSI 48 ; rows ; cols ; h ; w t — ?2048 resize
			rows, cols, h, w := f[1], f[2], f[3], f[4]
			if rows > 0 && cols > 0 && h > 0 && w > 0 {
				e.pixelMouse.cellH, e.pixelMouse.cellW = h/rows, w/cols
				e.pixelMouseGoActive()
			}
		}
		return true
	}
	return false
}

// pixelMouseGoActive enables pixel reports once a cell size is known (from
// either the CSI 16 t reply or a ?2048 notification, whichever arrives first).
func (e *Editor) pixelMouseGoActive() {
	if e.pixelMouse.phase == pixelMouseAwaitCell &&
		e.pixelMouse.cellW > 0 && e.pixelMouse.cellH > 0 {
		e.pixelMouse.phase = pixelMouseActive
		e.Renderer.EmitRaw("\x1b[?1016h") // switch reports to pixels
	}
}

// refreshPixelMouseCellSize re-queries the cell pixel size after a resize, for
// terminals that report it via CSI 16 t but do not send the ?2048 in-band
// notification. No-op unless the handshake is under way or active.
func (e *Editor) refreshPixelMouseCellSize() {
	if !e.realTerminal {
		return
	}
	switch e.pixelMouse.phase {
	case pixelMouseAwaitCell, pixelMouseActive:
		e.Renderer.EmitRaw("\x1b[16t")
	}
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

func splitReportInts(s string) []int {
	parts := strings.Split(s, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(strings.TrimSpace(p))
	}
	return out
}
