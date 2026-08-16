package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/purfecterm"
)

// newReleaseTestTerm builds a focused PurfecTerm with its outbound bytes
// captured, and negotiates the keyboard flags the caller names.
func newReleaseTestTerm(t *testing.T, flags int) (*PurfecTerm, func() string) {
	t.Helper()
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})
	var sink []byte
	term.SetInputSink(func(b []byte) { sink = append(sink, b...) })
	term.Terminal().SetFocused(true)
	if buf := term.Terminal().Buffer(); buf != nil && flags != 0 {
		buf.SetKeyboardFlags(flags)
	}
	return term, func() string { return string(sink) }
}

// A key coming back up reaches the child, once the child has asked for it.
//
// This is the link that was missing on BOTH hosts. TrinketBase.HandleKeyRelease
// returns false and nothing overrode it, so a release stopped here — the SDL
// backend had been dispatching these events all along and they died one call
// short of the child. A browser tracking a held key was told about presses only.
func TestKeyReleaseReachesTheChild(t *testing.T) {
	flags := purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportEvents
	term, out := newReleaseTestTerm(t, flags)

	if !term.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"}) {
		t.Fatal("HandleKeyRelease returned false; the release was not claimed")
	}
	got := out()
	if got == "" {
		t.Fatal("a release produced no bytes; it stopped at the trinket")
	}
	if want := "\x1b[97;1:3u"; got != want {
		t.Errorf("release of 'a' sent %q, want %q", got, want)
	}
}

// The press and the release are told apart at the child.
//
// A release that arrived looking like a press would be worse than none: the
// guest would see the key struck twice. The event type in the sequence is what
// separates them.
func TestPressAndReleaseDifferOnTheWire(t *testing.T) {
	flags := purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportEvents

	pressTerm, pressOut := newReleaseTestTerm(t, flags)
	pressTerm.HandleKeyPress(core.KeyPressEvent{Key: "Up"})

	relTerm, relOut := newReleaseTestTerm(t, flags)
	relTerm.HandleKeyRelease(core.KeyReleaseEvent{Key: "Up"})

	press, release := pressOut(), relOut()
	if press == "" || release == "" {
		t.Fatalf("press=%q release=%q, want both sent", press, release)
	}
	if press == release {
		t.Errorf("press and release both sent %q; the guest cannot tell them apart", press)
	}
	if !strings.Contains(release, ":3") {
		t.Errorf("release sent %q, which carries no event type; the protocol marks "+
			"a release with :3", release)
	}
}

// A child that never asked for release events is not sent any.
//
// Sending them unasked is how a keystroke appears to arrive twice, so the
// emulator drops them — this test is here because the trinket now forwards
// unconditionally and it is the layer below that decides.
func TestReleaseIsNotSentToAChildThatDidNotAsk(t *testing.T) {
	term, out := newReleaseTestTerm(t, 0)

	term.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"})
	if got := out(); got != "" {
		t.Errorf("a child that negotiated nothing was sent %q for a release", got)
	}

	// And with disambiguation alone, which is not event reporting.
	term2, out2 := newReleaseTestTerm(t, purfecterm.KeyboardDisambiguate)
	term2.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"})
	if got := out2(); got != "" {
		t.Errorf("under disambiguation alone a release sent %q, want nothing", got)
	}
}
