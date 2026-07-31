package editor

import (
	"testing"

	"github.com/phroun/kittytk/hostterm"
)

// "auto" maps a detected terminal to a concrete rtlMarkMode; an explicit mode
// passes through unchanged.
func TestRtlMarkModeForTerminal(t *testing.T) {
	want := map[hostterm.Kind]string{
		hostterm.TerminalITerm2:        "iterm2",
		hostterm.TerminalAlacritty:     "drift",
		hostterm.TerminalGhostty:       "normal", // quirks TBD
		hostterm.TerminalKitty:         "normal", // quirks TBD
		hostterm.TerminalAppleTerminal: "compose",
		hostterm.TerminalCoolRetroTerm: "normal",
		hostterm.TerminalPurfecterm:    "normal",
		hostterm.TerminalUnknown:       "normal",
	}
	for k, w := range want {
		if got := rtlMarkModeForTerminal(k); got != w {
			t.Errorf("%s -> %q, want %q", k, got, w)
		}
	}
	for _, m := range []string{"normal", "iterm2", "compose", "drift"} {
		if got := resolveRtlMarkMode(m); got != m {
			t.Errorf("explicit %q resolved to %q, want passthrough", m, got)
		}
	}
}

// flipBidiForHost="auto" flips only for a sniffed bidi host (Apple Terminal);
// the stream-order terminals are known to NOT flip (and skip the probe), and an
// unrecognised host is left to the probe. Explicit true/false pass through.
func TestFlipBidiForHostResolve(t *testing.T) {
	cases := map[hostterm.Kind]struct{ flip, known bool }{
		hostterm.TerminalAppleTerminal: {true, true},
		hostterm.TerminalITerm2:        {false, true},
		hostterm.TerminalAlacritty:     {false, true},
		hostterm.TerminalGhostty:       {false, true},
		hostterm.TerminalKitty:         {false, true},
		hostterm.TerminalUnknown:       {false, false},
		hostterm.TerminalPurfecterm:    {false, false},
	}
	for k, w := range cases {
		flip, known := hostFlipDecision(k)
		if flip != w.flip || known != w.known {
			t.Errorf("hostFlipDecision(%s) = (%v,%v), want (%v,%v)", k, flip, known, w.flip, w.known)
		}
	}
	if !resolveFlipBidiForHost("true") {
		t.Errorf(`resolveFlipBidiForHost("true") = false, want true`)
	}
	if resolveFlipBidiForHost("false") {
		t.Errorf(`resolveFlipBidiForHost("false") = true, want false`)
	}
}
