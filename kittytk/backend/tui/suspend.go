package tui

import (
	"golang.org/x/term"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Suspending the host: handing the terminal back to the shell that launched
// this process, stopping, and taking the terminal again when the shell
// continues it.
//
// The process that owns the terminal is the whole display service, so every
// application on it freezes together. Remote clients keep running; what they
// send waits in the socket until the service is continued.
//
// Two things start a suspend. The host_suspend command calls SuspendHost, and
// a SIGTSTP from outside - kill -TSTP, or a wrapper passing job control
// through - is caught by the watcher (see suspend_unix.go) and routed the same
// way. ^Z is neither: the key reader keeps the terminal in raw mode, where the
// terminal does not turn ^Z into a signal, so it arrives as a key and means
// whatever the keymap says, which by default is nothing.

// suspend hands the terminal back, calls stop, and takes the terminal again
// once stop returns. stop is what actually stops the process; it is a parameter
// so the hand-back and the retake can be tested without stopping the test.
//
// t.mu is held from the hand-back to the retake, so a frame the event loop
// finishes in the meantime cannot land on the shell's screen. The key reader is
// paused before the lock is taken, because pausing releases the keys still held
// and the releases reach handleKey, which takes t.mu itself.
func (t *TUIBackend) suspend(stop func()) {
	t.suspendMu.Lock()
	defer t.suspendMu.Unlock()

	t.mu.Lock()
	kb := t.keyboard
	t.mu.Unlock()
	if kb != nil {
		kb.Pause()
	}

	t.mu.Lock()
	t.leaveTerminalModes()
	// While the shell has the terminal there is nothing for an emergency exit
	// to restore, and writing the restore escapes again would land on the
	// shell's screen.
	unregisterLive(t)

	stop()

	if kb != nil {
		kb.Resume()
	}
	t.retakeTerminalLocked()
	t.mu.Unlock()

	registerLive(t)
	t.queueRepaint()
}

// retakeTerminalLocked puts back everything the hand-back turned off, and
// forgets what the backend believed the screen showed. The alternate screen
// comes back empty, so the next frame has to be written in full; the caret,
// the pictures and the terminal's size are all things the shell may have
// changed in the meantime. The caller holds t.mu.
func (t *TUIBackend) retakeTerminalLocked() {
	if t.fd >= 0 && term.IsTerminal(t.fd) {
		if cols, rows, err := term.GetSize(t.fd); err == nil && (cols != t.cols || rows != t.rows) {
			t.cols, t.rows = cols, rows
			t.allocateBuffers()
		}
	}

	t.enterTerminalModes()
	// enterTerminalModes hides the caret; the hand-back had shown it.
	t.cursorShown = false
	// Pixel mouse is turned on by the startup probe, not by enterTerminalModes,
	// so it has to come back on here. The probe's answer still holds: it is
	// the same terminal.
	if t.pixelMouse {
		t.writeTTY("\033[?1016h")
	}

	// Every row is cleared and written again on the next frame, the same as
	// after a resize.
	t.needsLineClear = true
	// The caret's shape and colour: the hand-back reset the colour, and the
	// shell may have set a shape of its own. -1 is no DECSCUSR shape, so the
	// next frame sends whichever one is wanted.
	t.cursorStyleSent = -1
	t.cursorColorSent = style.ColorDefault
	// Pictures went with the old alternate screen. Forgetting them makes the
	// next flush send them all again.
	t.shownImages = nil
}

// queueRepaint asks for a frame at the terminal's current size. A resize event
// is what does that: it reaches the desktop as a new screen size (which it may
// be), and every event delivered is followed by a frame.
func (t *TUIBackend) queueRepaint() {
	t.mu.Lock()
	ev := core.ResizeEvent{
		Width:  t.metrics.CellToUnitsX(t.cols),
		Height: t.metrics.CellToUnitsY(t.rows),
		Cols:   t.cols,
		Rows:   t.rows,
	}
	t.mu.Unlock()
	select {
	case t.eventQueue <- ev:
	default:
	}
}
