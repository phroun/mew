package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// cmdBool runs a command and reports its boolean status (true unless the command
// explicitly returned BoolStatus(false)), mirroring runBoundCommand.
func cmdBool(e *Editor, cmd string) bool {
	if b, ok := e.executeCommandResult(cmd).(pawscript.BoolStatus); ok {
		return bool(b)
	}
	return true
}

// A document viewport deletes a newline like any character by default: backspace
// at the start of a line joins it with the line above.
func TestDeleteNewlineAsCharDefaultAllows(t *testing.T) {
	e, w := newTestEditor(t, "abc\ndef\n")
	if v, _ := e.getOption(w, "deletenewlineaschar"); v != "yes" {
		t.Fatalf("default deleteNewlineAsChar = %q, want yes", v)
	}
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 0})
	if !cmdBool(e, "del_char_prior") {
		t.Fatal("backspace at a line start should succeed by default")
	}
	if got := w.Buffer.GetContent(); got != "abcdef\n" {
		t.Fatalf("content = %q, want the lines joined", got)
	}
}

// With the option off, backspace at the start of a line declines (returns false,
// no edit) rather than joining lines — but an in-line backspace still works.
func TestDeleteNewlineAsCharOffDeclinesBackspace(t *testing.T) {
	e, w := newTestEditor(t, "abc\ndef\n")
	if !e.setOption(w, "deletenewlineaschar", "no") {
		t.Fatal("set_option deleteNewlineAsChar no")
	}
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 0})
	if cmdBool(e, "del_char_prior") {
		t.Error("backspace at a line start should decline when the option is off")
	}
	if got := w.Buffer.GetContent(); got != "abc\ndef\n" {
		t.Fatalf("content changed to %q; the join must be declined", got)
	}
	// In-line backspace is unaffected.
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 1})
	if !cmdBool(e, "del_char_prior") {
		t.Error("an in-line backspace should still work")
	}
	if got := w.Buffer.GetContent(); got != "abc\nef\n" {
		t.Fatalf("in-line delete gave %q", got)
	}
}

// With the option off, forward-delete at end of line declines rather than
// pulling the next line up.
func TestDeleteNewlineAsCharOffDeclinesForward(t *testing.T) {
	e, w := newTestEditor(t, "abc\ndef\n")
	if !e.setOption(w, "deletenewlineaschar", "no") {
		t.Fatal("set_option deleteNewlineAsChar no")
	}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 3}) // end of "abc"
	if cmdBool(e, "del_char_next") {
		t.Error("forward-delete at end of line should decline when off")
	}
	if got := w.Buffer.GetContent(); got != "abc\ndef\n" {
		t.Fatalf("content changed to %q; the join must be declined", got)
	}
}

// A prompt viewport protects newlines by default (deleteNewlineAsChar false),
// forced at creation regardless of the options passed.
func TestPromptViewportProtectsNewlines(t *testing.T) {
	e, _ := newTestEditor(t, "")
	id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:   viewport.PromptViewport,
		ID:     "p",
		Buffer: buffer.NewFromString("x"),
	})
	p := e.ViewportManager.GetViewport(id)
	if p == nil {
		t.Fatal("prompt viewport not created")
	}
	if !p.ViewState.ProtectNewlines {
		t.Error("a prompt viewport should protect newlines")
	}
	if v, _ := e.getOption(p, "deletenewlineaschar"); v != "no" {
		t.Fatalf("prompt deleteNewlineAsChar = %q, want no", v)
	}
}
