package tui

import (
	"io"
	"strconv"
	"strings"
	"testing"
)

// A keypad key has to arrive AS a keypad key, on the way down as well as up.
//
// This is what report_all_keys (0b1000) is for, and nothing else buys it. A
// terminal without it sends any key that produces TEXT as that text and nothing
// else, so the pad's 7 goes down as the byte "7" — the same byte the main row
// sends, carrying no identity at all — while its repeats and its release have
// no legacy form, must go as CSI u, and carry keycode 57406. One key reported
// as two, and a release for a press nobody made.
//
// Disambiguate alone does not close it. It promotes the pad keys that produce
// NO text, which is exactly why P-Enter, P-Home and the pad arrows were always
// right and only the LOCKED pad was wrong. Nor is there a narrower lever: the
// application-keypad mode that would have been keypad-only is parsed and
// discarded by the kitty terminal, the protocol's own reference
// implementation, whose screen_alternate_keypad_mode handler is empty.
func TestTheKeypadIsAskedToIdentifyItself(t *testing.T) {
	const (
		disambiguate  = 1
		reportEvents  = 2
		reportAllKeys = 8
	)
	var out strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard, EnableMouse: true})
	b.ttyOut = &out
	b.enterTerminalModes()
	got := out.String()

	want := disambiguate | reportEvents | reportAllKeys
	flags, _, ok := pushedKeyboardFlags(got)
	if !ok || flags&want != want {
		t.Fatalf("startup wrote %q, which does not push %d "+
			"(disambiguate|report-events|report-all-keys); without report-all-keys "+
			"a locked keypad key goes down as an anonymous byte", got, want)
	}

	// The old value, kept named so a revert is loud rather than quiet: it is
	// the same flags minus the one that identifies the pad.
	if strings.Contains(got, "\033[>3u") {
		t.Error("the flags went back to disambiguate|report-events, which leaves " +
			"every text-producing keypad key anonymous on the way down")
	}
}

// pushedKeyboardFlags finds the "kitty" keyboard protocol push in a startup
// sequence and returns the flag set it asks for, and where it sits.
//
// The BITS, not the number: the set grows as the host asks the terminal for
// more - report-all-keys so the keypad identifies itself, associated text so a
// dead key's character survives - and a test that pins the total has to be
// edited every time one is added, which is the moment it stops saying what it
// meant and starts saying what it saw.
func pushedKeyboardFlags(out string) (flags, at int, ok bool) {
	at = strings.Index(out, "\033[>")
	if at < 0 {
		return 0, 0, false
	}
	rest := out[at+len("\033[>"):]
	end := strings.IndexByte(rest, 'u')
	if end < 0 {
		return 0, 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, 0, false
	}
	return n, at, true
}
