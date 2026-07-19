package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The terminal is a pure display+input surface: the child process lives on
// the client, so a keystroke must be delivered to the input sink (bound for
// the client's PTY) rather than swallowed. This is the contract the wire's
// input event and the in-process ptydriver both depend on.
func TestPurfecTermKeyReachesInputSink(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("no terminal")
	}
	var got []byte
	term.SetInputSink(func(b []byte) { got = append(got, b...) })
	term.HandleFocusIn()

	term.HandleKeyPress(core.KeyPressEvent{Key: "A"})
	if string(got) != "A" {
		t.Errorf("input sink got %q, want %q", got, "A")
	}
}

// Shift-modified scrollback keys scroll the view locally and must NOT be
// sent to the child (they are terminal-multiplexer navigation, not input).
func TestPurfecTermScrollbackKeysStayLocal(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("no terminal")
	}
	var got []byte
	term.SetInputSink(func(b []byte) { got = append(got, b...) })
	term.HandleFocusIn()

	for _, k := range []string{"S-PageUp", "S-PageDown", "S-Up", "S-Down", "S-Home", "S-End"} {
		term.HandleKeyPress(core.KeyPressEvent{Key: k})
	}
	if len(got) != 0 {
		t.Errorf("scrollback keys leaked %q to the child input sink", got)
	}
}

// SetResizeSink fires once immediately with the current grid size so a
// freshly attached client can size its PTY without waiting for a relayout.
func TestPurfecTermResizeSinkFiresOnAttach(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("no terminal")
	}
	var cols, rows int
	var fired bool
	term.SetResizeSink(func(c, r int) { cols, rows, fired = c, r, true })
	if !fired {
		t.Fatal("resize sink did not fire on attach")
	}
	if cols <= 0 || rows <= 0 {
		t.Errorf("resize sink fired with a degenerate size %dx%d", cols, rows)
	}
}
