package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// Editor mode turns a PurfecTerm into a full-screen editor display
// surface: the scrollback buffer is disabled (an editor repaints and
// owns its viewport), and the local Shift+navigation scroll keys are
// no longer intercepted - they reach the child (the editor) like any
// other key. These behaviors are observable in the text path, so they
// run in the default build without the graphical stack.

func TestEditorModeDisablesScrollback(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	defer term.Close()

	if term.EditorMode() {
		t.Fatal("editor mode should be off by default")
	}
	term.SetEditorMode(true)
	if !term.EditorMode() {
		t.Fatal("EditorMode() false after SetEditorMode(true)")
	}
	if !term.Terminal().Buffer().IsScrollbackDisabled() {
		t.Fatal("scrollback not disabled in editor mode")
	}

	// Feed far more lines than the 24-row screen holds: with scrollback
	// disabled the overflow is discarded, never accumulated.
	term.Feed([]byte(strings.Repeat("line\r\n", 200)))
	if n := term.Terminal().Buffer().GetScrollbackSize(); n != 0 {
		t.Fatalf("scrollback grew to %d lines in editor mode, want 0", n)
	}

	// Turning it back off re-enables accumulation.
	term.SetEditorMode(false)
	if term.Terminal().Buffer().IsScrollbackDisabled() {
		t.Fatal("scrollback still disabled after leaving editor mode")
	}
	term.Feed([]byte(strings.Repeat("more\r\n", 200)))
	if n := term.Terminal().Buffer().GetScrollbackSize(); n == 0 {
		t.Fatal("scrollback did not accumulate after leaving editor mode")
	}
}

func TestEditorModeForwardsScrollKeys(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	defer term.Close()

	var sink []byte
	term.SetInputSink(func(b []byte) { sink = append(sink, b...) })

	// Normal mode: Shift+PageUp is a local scrollback gesture - consumed,
	// never sent to the child.
	sink = nil
	term.HandleKeyPress(core.KeyPressEvent{Key: "S-PageUp"})
	if len(sink) != 0 {
		t.Fatalf("normal mode leaked scrollback key to child: %q", sink)
	}

	// Editor mode: there is no scrollback, so the same key passes through
	// to the child (the editor) as input.
	term.SetEditorMode(true)
	sink = nil
	term.HandleKeyPress(core.KeyPressEvent{Key: "S-PageUp"})
	if len(sink) == 0 {
		t.Fatal("editor mode did not forward the scroll key to the child")
	}

	// A plain typed key reaches the child in both modes (sanity: the sink
	// wiring itself works).
	sink = nil
	term.HandleKeyPress(core.KeyPressEvent{Key: "a"})
	if len(sink) == 0 {
		t.Fatal("typed key never reached the child")
	}
}

func TestEditorModeNoScrollLane(t *testing.T) {
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	defer term.Close()

	term.SetEditorMode(true)
	// No scrollbar lanes exist in editor mode, so no local point is ever
	// "over" one (drives the I-beam-vs-arrow cursor choice).
	if term.overScrollLane(1, 1) {
		t.Fatal("editor mode reports a point over a scrollbar lane")
	}
}
