//go:build mew

package trinkets

import (
	"testing"
)

// mew names its keys its own way and the emulator's encoder was written
// against direct-key-handler's. The rename happens at the boundary, carries
// modifier prefixes across, and leaves alone every name the two already
// agree on.
func TestDKHKeyName(t *testing.T) {
	for _, tc := range []struct{ mew, want string }{
		{"esc", "Escape"},
		{"back", "Backspace"},
		{"fdel", "Delete"},
		{"del", "Delete"},
		{"pgup", "PageUp"},
		{"return", "Enter"},
		{"space", "Space"},
		{"S-tab", "S-Tab"},
		{"M-up", "M-Up"},
		{"S-M-home", "S-M-Home"},
		// Already the same in both vocabularies.
		{"^C", "^C"},
		{"F5", "F5"},
		{"a", "a"},
		{"M-x", "M-x"},
	} {
		if got := dkhKeyName(tc.mew); got != tc.want {
			t.Errorf("dkhKeyName(%q) = %q, want %q", tc.mew, got, tc.want)
		}
	}
}

// terminalKey asks the emulator what a key means and hands back the bytes it
// produced. The encoder is purfecterm's own, so a terminal inside mew and a
// terminal on its own agree about what a key is on the wire.
func TestTerminalKeyEncodesThroughTheEmulator(t *testing.T) {
	e := NewEditor()
	e.terminalOpen("pty1", 80, 24)

	for _, tc := range []struct{ key, want string }{
		{"^C", "\x03"},
		{"esc", "\x1b"},
		{"up", "\x1b[A"},
		{"F5", "\x1b[15~"},
		{"pgup", "\x1b[5~"},
		{"a", "a"},
	} {
		if got := string(e.terminalKey("pty1", tc.key)); got != tc.want {
			t.Errorf("terminalKey(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}

	// An unknown surface is not an error, just nothing.
	if got := e.terminalKey("nosuch", "^C"); got != nil {
		t.Errorf("unknown surface returned %q, want nil", got)
	}

	// Capturing the bytes must not leave the sink installed, or the child's
	// later output would be captured by a dead closure.
	e.termMu.Lock()
	s := e.termSurfaces["pty1"]
	e.termMu.Unlock()
	if s.term.inputSink != nil {
		t.Error("the capture sink should be detached after the encode")
	}
}
