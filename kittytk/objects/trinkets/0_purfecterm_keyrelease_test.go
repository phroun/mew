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

	// The press first: a release is only sent for a key this child was handed,
	// so on its own it would prove the matching rule and not this path.
	term.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a"})
	pressed := out()

	if !term.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"}) {
		t.Fatal("HandleKeyRelease returned false; the release was not claimed")
	}
	got := strings.TrimPrefix(out(), pressed)
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
	relTerm.HandleKeyPress(core.KeyPressEvent{Key: "Up"})
	relPress := relOut()
	relTerm.HandleKeyRelease(core.KeyReleaseEvent{Key: "Up"})

	press, release := pressOut(), strings.TrimPrefix(relOut(), relPress)
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

// A held key reaches the child marked as a repeat.
//
// The event says "repeat" in a field of its own while the emulator's encoder
// reads the marker off the NAME, so this is the layer that has to put it back.
// It did not, and neither backend even set the field — the TUI trimmed the
// protocol's marker off the name and SDL never read its own repeat bit — so
// every layer below this was correct and unreachable: mew stripped and
// re-attached a marker that never arrived, and a hosted browser was told a held
// key had been struck again, its repeat flag clear every time.
func TestHeldKeyReachesTheChildAsARepeat(t *testing.T) {
	flags := purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportEvents
	term, out := newReleaseTestTerm(t, flags)

	term.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a"})
	first := out()

	repTerm, repOut := newReleaseTestTerm(t, flags)
	repTerm.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a", Repeat: true})
	repeat := repOut()

	if first == "" || repeat == "" {
		t.Fatalf("press=%q repeat=%q, want both sent", first, repeat)
	}
	if first == repeat {
		t.Errorf("a struck key and a held one both sent %q; the guest cannot tell "+
			"a repeat from another press", first)
	}
	if !strings.Contains(repeat, ":2") {
		t.Errorf("the repeat sent %q, which carries no event type; the protocol "+
			"marks a repeat with :2", repeat)
	}
}

// A child that never asked for release events is not sent any.
//
// Sending them unasked is how a keystroke appears to arrive twice, so the
// emulator drops them — this test is here because the trinket now forwards
// unconditionally and it is the layer below that decides.
func TestReleaseIsNotSentToAChildThatDidNotAsk(t *testing.T) {
	// Pressed first in both, so what is being tested is the negotiation and not
	// the press-matching rule, which would drop an unpressed release anyway.
	term, out := newReleaseTestTerm(t, 0)
	term.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a"})
	pressed := out()

	term.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"})
	if got := strings.TrimPrefix(out(), pressed); got != "" {
		t.Errorf("a child that negotiated nothing was sent %q for a release", got)
	}

	// And with disambiguation alone, which is not event reporting.
	term2, out2 := newReleaseTestTerm(t, purfecterm.KeyboardDisambiguate)
	term2.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a"})
	pressed2 := out2()
	term2.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"})
	if got := strings.TrimPrefix(out2(), pressed2); got != "" {
		t.Errorf("under disambiguation alone a release sent %q, want nothing", got)
	}
}

// A key the child was never handed does not come back up at it either.
//
// This is the first bug's mirror image. A press reaches this trinket only if
// nothing above claimed it — a menu accelerator, a window shortcut — and this
// trinket claims some itself for scrollback. A release is routed past all of
// that, straight to whatever holds focus, because those gestures are decided
// on the press. Both rules are right; together they told a guest to let go of
// keys it had never been told to hold.
func TestAReleaseWithoutItsPressIsNotSent(t *testing.T) {
	flags := purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportEvents
	term, out := newReleaseTestTerm(t, flags)

	if term.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"}) {
		t.Error("a release with no press behind it was claimed")
	}
	if got := out(); got != "" {
		t.Errorf("the child was sent %q for a key it never saw pressed", got)
	}

	// One press answers for one release, and no more: a key cannot come up
	// twice without going down in between, and an entry left behind would keep
	// answering for every later release of that name.
	term.HandleKeyPress(core.KeyPressEvent{Key: "a", Text: "a"})
	term.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"})
	settled := out()
	term.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"})
	if got := out(); got != settled {
		t.Errorf("a second release sent %q; one press answered twice",
			strings.TrimPrefix(got, settled))
	}

	// And a key still held when focus leaves is forgotten, so its press cannot
	// answer for some later release of the same key.
	term.HandleKeyPress(core.KeyPressEvent{Key: "b", Text: "b"})
	term.HandleFocusOut()
	held := out()
	term.HandleKeyRelease(core.KeyReleaseEvent{Key: "b"})
	if got := out(); got != held {
		t.Errorf("a press left over from before the focus change sent %q",
			strings.TrimPrefix(got, held))
	}
}
