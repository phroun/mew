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
		// The erase trio. Both vocabularies name three inputs, and the pairs
		// are what matter rather than the words, which nearly rhyme: BS is
		// back/Backspace, the DEL character is del/Delete, and forward delete
		// is fdel/FDel. Reading "Delete" as the Delete key sends the cursor
		// the wrong way.
		{"back", "Backspace"},
		{"del", "Delete"},
		{"fdel", "FDel"},
		{"pgup", "PageUp"},
		// The home-row key, which upstream calls "Return". "Enter" is the
		// keypad's — a different key, and one mew never has to name here
		// because it folds the two on the way in.
		{"return", "Return"},
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

// EVERY modifier prefix crosses the boundary, not the three that happened to be
// listed.
//
// The prefix list is a membership test, and an absent entry is a silence rather
// than an error: the base never reaches the rename table, the key travels on
// under mew's spelling, and the emulator's encoder — which knows only
// upstream's — produces no bytes for it. So a keypad key, a Super chord, a
// Micro chord and a Hyper chord were each dropped on the way into a terminal
// viewport, with nothing to show that a keystroke had gone missing.
func TestEveryModifierPrefixCrossesTheBoundary(t *testing.T) {
	for _, tc := range []struct{ mew, want, what string }{
		// The keypad, which the graphical host can now report at all.
		{"P-home", "P-Home", "the pad's Home"},
		{"P-begin", "P-Begin", "the pad's 5, unlocked"},
		{"P-return", "P-Return", "mew folds the pad's Enter onto return"},
		{"C-P-pgup", "C-P-PageUp", "stacked with Control"},
		{"p-del", "p-Delete", "and the archaic pad prefix too"},
		{"P-7", "P-7", "a shown pad key has no name to rename"},
		// The prefixes that were missing before the keypad ever arrived.
		{"s-home", "s-Home", "Super"},
		{"m-pgup", "m-PageUp", "Micro"},
		{"H-fdel", "H-FDel", "Hyper"},
		{"G-up", "G-Up", "Glyph"},
		// And an event suffix still survives the round trip.
		{"P-home:Release", "P-Home:Release", "a pad release"},
		{"s-end:Repeat", "s-End:Repeat", "a held Super chord"},
	} {
		if got := dkhKeyName(tc.mew); got != tc.want {
			t.Errorf("%s: dkhKeyName(%q) = %q, want %q", tc.what, tc.mew, got, tc.want)
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
		// F10 is the key raw_key_input exists for: every layer above has its
		// own claim on it.
		{"F10", "\x1b[21~"},
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

	// The sink is PERSISTENT now (it is how a context-menu Paste reaches the
	// session), so after the encode it must still be installed — and the
	// drain must be off, or the child's later bytes would pile into a buffer
	// nobody returns.
	e.termMu.Lock()
	s := e.termSurfaces["pty1"]
	e.termMu.Unlock()
	if s.term.inputSink == nil {
		t.Error("the persistent sink should remain installed after the encode")
	}
	if s.draining {
		t.Error("draining must be off between hook calls")
	}
	if len(s.pending) != 0 {
		t.Errorf("pending should be empty between hook calls, has %d bytes", len(s.pending))
	}
}
