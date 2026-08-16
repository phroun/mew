//go:build mew

package trinkets

import (
	"testing"
)

// An event suffix decorates a name; it is not part of it. The rename has to set
// it aside and put it back, or a release arrives under a name the emulator's
// encoder has never heard of.
func TestDKHKeyNameKeepsTheEventSuffix(t *testing.T) {
	for _, tc := range []struct{ mew, want string }{
		{"up:Release", "Up:Release"},
		{"pgup:Release", "PageUp:Release"},
		{"S-M-home:Release", "S-M-Home:Release"},
		{"a:Release", "a:Release"},
		{"^C:Release", "^C:Release"},
		{"esc:Repeat", "Escape:Repeat"},
	} {
		if got := dkhKeyName(tc.mew); got != tc.want {
			t.Errorf("dkhKeyName(%q) = %q, want %q", tc.mew, got, tc.want)
		}
	}
}

// A key coming back UP reaches the child that asked for it.
//
// This is the last leg of the release chain, and it was the quietest break in
// it: everything above — a backend producing the release, the desktop routing
// it, the trinket forwarding it — could be right and the child still see
// nothing, because the name arrived here spelled mew's way and left the encoder
// with a key it could not resolve.
//
// The negotiation is what licenses the release at all, so it is exercised both
// ways: a child that pushed the protocol's flags gets the event, and a child
// that asked for nothing gets nothing, because a release has no legacy form and
// inventing one would type garbage at a program that cannot read it.
func TestTerminalKeyEncodesAReleaseForANegotiatedChild(t *testing.T) {
	e := NewEditor()
	e.terminalOpen("pty1", 80, 24)

	// Before any negotiation there is no release to send.
	if got := e.terminalKey("pty1", "up:Release"); got != nil {
		t.Errorf("release to a child that negotiated nothing = %q, want nothing", got)
	}

	// The child pushes disambiguate|report-events|alternates|all-keys, which is
	// what awrit sends on startup.
	e.termMu.Lock()
	s := e.termSurfaces["pty1"]
	e.termMu.Unlock()
	s.term.Feed([]byte("\x1b[>15u"))

	for _, tc := range []struct{ key, want string }{
		{"up:Release", "\x1b[1;1:3A"},
		// The alternates flag is among the four, so the letter reports its
		// shifted form beside the base key: 97 is "a", 65 is "A".
		{"a:Release", "\x1b[97:65;1:3u"},
	} {
		if got := string(e.terminalKey("pty1", tc.key)); got != tc.want {
			t.Errorf("terminalKey(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}

	// The press still encodes, and under the negotiated flags it takes the
	// protocol's form rather than the legacy one.
	if got := e.terminalKey("pty1", "up"); len(got) == 0 {
		t.Error("the press stopped encoding once the child negotiated")
	}
}

// A held key reaches the child marked as a repeat, and a child that asked for
// nothing still gets an ordinary press for it.
//
// The two answers are both right, and the difference is the negotiation. A
// browser reports a repeat as keydown with repeat set and cannot tell a held
// key from a drummed one without the marker; a program that never asked has no
// way to read one and must not be sent something it cannot parse — a repeat is
// a press to it, which is what a legacy terminal has always sent.
func TestTerminalKeyMarksARepeatOnlyForANegotiatedChild(t *testing.T) {
	e := NewEditor()
	e.terminalOpen("pty1", 80, 24)

	if got := string(e.terminalKey("pty1", "a:Repeat")); got != "a" {
		t.Errorf("repeat to a child that negotiated nothing = %q, want the plain press %q", got, "a")
	}

	e.termMu.Lock()
	s := e.termSurfaces["pty1"]
	e.termMu.Unlock()
	s.term.Feed([]byte("\x1b[>15u"))

	for _, tc := range []struct{ key, want string }{
		{"a:Repeat", "\x1b[97:65;1:2u"},
		{"up:Repeat", "\x1b[1;1:2A"},
	} {
		if got := string(e.terminalKey("pty1", tc.key)); got != tc.want {
			t.Errorf("terminalKey(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}
