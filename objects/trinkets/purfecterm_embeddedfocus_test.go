package trinkets

import "testing"

// A terminal hosted inside another focused trinket never enters the focus
// chain, so it has no focus of its own to hold: its host declares it. What it
// declares has to reach everything that behaves differently when focused —
// the cursor form and blink (gfxFocused), the platform caret, and the
// emulator's own flag, which is what a child process sees when it asked for
// focus reporting.
func TestEmbeddedFocusIsTheWholeNotionOfFocus(t *testing.T) {
	term := NewPurfecTerm()
	term.Init(term)

	if term.focused() || term.gfxFocused() {
		t.Fatal("a fresh unfocused terminal should not report focus")
	}
	if term.terminal != nil && term.terminal.IsFocused() {
		t.Fatal("the emulator should start unfocused too")
	}

	term.SetEmbeddedFocus(true)
	if !term.EmbeddedFocus() || !term.focused() {
		t.Error("SetEmbeddedFocus(true) should make the terminal report focus")
	}
	// The window chain is active for a parentless child, so the graphical
	// cursor takes its focused form instead of the hollow box.
	if !term.gfxFocused() {
		t.Error("the graphical cursor should take its focused form")
	}
	if term.terminal != nil && !term.terminal.IsFocused() {
		t.Error("the emulator's own focused flag should follow")
	}

	term.SetEmbeddedFocus(false)
	if term.focused() || term.gfxFocused() {
		t.Error("clearing it should put the terminal back to unfocused")
	}
	if term.terminal != nil && term.terminal.IsFocused() {
		t.Error("the emulator's flag should clear with it")
	}
}

// A mirror paint renders the terminal UNFOCUSED (hollow cursor, no platform
// caret) without disturbing the real focus or the emulator's own focus flag —
// so a host can paint one live terminal in several places while only its
// primary owns the caret.
func TestMirrorPaintRendersUnfocusedWithoutLosingFocus(t *testing.T) {
	term := NewPurfecTerm()
	term.Init(term)
	term.SetEmbeddedFocus(true)
	if !term.paintFocused() || !term.gfxFocused() {
		t.Fatal("an embedded-focused terminal should render focused")
	}

	term.mirrorPaint.Store(true)
	if term.paintFocused() || term.gfxFocused() {
		t.Error("a mirror paint must render unfocused (no focused cursor, no caret)")
	}
	if !term.focused() {
		t.Error("a mirror paint must NOT disturb the real focus")
	}
	if term.terminal != nil && !term.terminal.IsFocused() {
		t.Error("a mirror paint must NOT disturb the emulator's focus reporting")
	}

	term.mirrorPaint.Store(false)
	if !term.paintFocused() || !term.gfxFocused() {
		t.Error("clearing the mirror flag restores focused rendering")
	}
}

// A mirror paint must not resize the child: it draws the grid the primary
// settled, so updateTerminalSize early-returns under the mirror flag and emits
// no resize from the mirror's own rectangle. (Re-fitting from a smaller mirror
// is what scrolled the shared buffer and wrapped at the wrong column.)
func TestMirrorPaintSuppressesResize(t *testing.T) {
	term := NewPurfecTerm()
	term.Init(term)
	if term.Terminal() == nil {
		t.Skip("no term")
	}
	resizes := 0
	term.SetResizeSink(func(cols, rows int) { resizes++ })

	term.SetBounds(term.Bounds())
	term.updateTerminalSize()
	base := resizes

	term.mirrorPaint.Store(true)
	term.updateTerminalSize()
	term.mirrorPaint.Store(false)
	if resizes != base {
		t.Fatalf("a mirror paint emitted a resize (%d -> %d)", base, resizes)
	}
}
