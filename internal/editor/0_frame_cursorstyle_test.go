package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// The mode picks the shape: navigation sits over the top of the other two,
// then overwrite, then insert.
func TestCursorStyleFollowsMode(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")

	if got := e.cursorStyleFor(w); got != 5 {
		t.Fatalf("insert cursor = %d, want the default 5 (blinking bar)", got)
	}
	w.ViewState.OverwriteMode = true
	if got := e.cursorStyleFor(w); got != 3 {
		t.Fatalf("overwrite cursor = %d, want the default 3 (blinking underline)", got)
	}
	// Navigation wins even while overwrite is on.
	w.BrowseActive = true
	if got := e.cursorStyleFor(w); got != 2 {
		t.Fatalf("navigation cursor = %d, want the default 2 (steady block)", got)
	}
	w.BrowseActive = false
	w.ViewState.OverwriteMode = false
	if got := e.cursorStyleFor(w); got != 5 {
		t.Fatalf("back to insert = %d, want 5", got)
	}
}

// The three options round-trip through set_option/get_option and drive the
// shape live.
func TestCursorStyleOptions(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	for _, tc := range []struct{ name, def string }{
		{"insertCursor", "5"},
		{"overwriteCursor", "3"},
		{"navigationCursor", "2"},
	} {
		if v, _ := e.getOption(w, strings.ToLower(tc.name)); v != tc.def {
			t.Fatalf("%s default = %q, want %q", tc.name, v, tc.def)
		}
	}

	e.executeCommand(`set_option "insertCursor", "6"`)
	if v, _ := e.getOption(w, "insertcursor"); v != "6" {
		t.Fatalf("insertCursor after set = %q, want 6", v)
	}
	if got := e.cursorStyleFor(w); got != 6 {
		t.Fatalf("cursor shape = %d, want the new 6", got)
	}

	e.executeCommand(`set_option "overwriteCursor", "4"`)
	e.executeCommand(`set_option "navigationCursor", "1"`)
	w.ViewState.OverwriteMode = true
	if got := e.cursorStyleFor(w); got != 4 {
		t.Fatalf("overwrite shape = %d, want 4", got)
	}
	w.BrowseActive = true
	if got := e.cursorStyleFor(w); got != 1 {
		t.Fatalf("navigation shape = %d, want 1", got)
	}
}

// Values outside DECSCUSR's 0-6 are rejected, leaving the option alone.
func TestCursorStyleRejectsOutOfRange(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	for _, bad := range []string{"7", "42", "-1", "banana"} {
		if e.setOption(nil, "insertCursor", bad) {
			t.Fatalf("set_option insertCursor=%q should be rejected", bad)
		}
	}
	if v, _ := e.getOption(w, "insertcursor"); v != "5" {
		t.Fatalf("insertCursor = %q, want the default 5 after rejected sets", v)
	}
	// 0 (the terminal's own default) IS in range.
	if !e.setOption(nil, "insertCursor", "0") {
		t.Fatal("insertCursor=0 should be accepted")
	}
}

// A rendered frame emits DECSCUSR for the focused mode, and only alongside a
// visible cursor.
func TestCursorStyleEmittedOnRender(t *testing.T) {
	e, w, out := newRenderedEditor(t, "hello\n")
	e.performRender()
	if got := out.String(); !strings.Contains(got, "\x1b[5 q") {
		t.Fatalf("insert mode should emit DECSCUSR 5; frame = %q", lastEscapes(got))
	}

	// An unchanged shape is not re-emitted.
	out.Reset()
	e.performRender()
	if strings.Contains(out.String(), " q") {
		t.Fatal("an unchanged cursor shape should stay off the wire")
	}

	// Switching mode re-emits.
	out.Reset()
	w.ViewState.OverwriteMode = true
	e.performRender()
	if got := out.String(); !strings.Contains(got, "\x1b[3 q") {
		t.Fatalf("overwrite mode should emit DECSCUSR 3; frame = %q", lastEscapes(got))
	}
}

// While the caret is deliberately hidden — inert inside a focused link button —
// no shape is selected: DECSCUSR never accompanies a hidden cursor.
func TestCursorStyleSilentWhileCaretHidden(t *testing.T) {
	e, w, out := newRenderedEditor(t, "hello\n")
	e.performRender() // settle the initial shape
	out.Reset()

	// Stand in for the focused-button caret-hide the editor installs.
	e.Renderer.SetCaretHiddenFn(func(*viewport.Viewport) bool { return true })
	w.ViewState.OverwriteMode = true // a DIFFERENT shape than the one in effect
	e.performRender()

	got := out.String()
	if strings.Contains(got, " q") {
		t.Fatalf("a hidden caret must not select a shape; frame = %q", lastEscapes(got))
	}
	if !strings.Contains(got, "\x1b[?25l") {
		t.Fatal("the caret should have been hidden")
	}

	// When it comes back, the mode's shape is asserted then.
	out.Reset()
	e.Renderer.SetCaretHiddenFn(func(*viewport.Viewport) bool { return false })
	e.performRender()
	if got := out.String(); !strings.Contains(got, "\x1b[3 q") {
		t.Fatalf("shape should be asserted when the caret returns; frame = %q", lastEscapes(got))
	}
}

// lastEscapes renders a frame's escape bytes readably for a failure message.
func lastEscapes(s string) string {
	return strings.ReplaceAll(s, "\x1b", "<ESC>")
}
