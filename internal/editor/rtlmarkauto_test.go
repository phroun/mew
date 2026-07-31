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

// flipBidiForHost="auto": Apple Terminal flips whole-run, Kitty flips word-wise,
// the stream-order terminals do not flip (all recognised, so all skip the
// probe), and an unrecognised host is left to the probe. Explicit true/false
// pass through.
func TestFlipBidiForHostResolve(t *testing.T) {
	cases := map[hostterm.Kind]struct{ flip, wordwise, known bool }{
		hostterm.TerminalAppleTerminal: {true, false, true},
		hostterm.TerminalKitty:         {true, true, true},
		hostterm.TerminalITerm2:        {false, false, true},
		hostterm.TerminalAlacritty:     {false, false, true},
		hostterm.TerminalGhostty:       {false, false, true},
		hostterm.TerminalUnknown:       {false, false, false},
		hostterm.TerminalPurfecterm:    {false, false, false},
	}
	for k, w := range cases {
		flip, wordwise, known := hostFlipDecision(k)
		if flip != w.flip || wordwise != w.wordwise || known != w.known {
			t.Errorf("hostFlipDecision(%s) = (%v,%v,%v), want (%v,%v,%v)",
				k, flip, wordwise, known, w.flip, w.wordwise, w.known)
		}
	}
	if !resolveFlipBidiForHost("true") {
		t.Errorf(`resolveFlipBidiForHost("true") = false, want true`)
	}
	if resolveFlipBidiForHost("false") {
		t.Errorf(`resolveFlipBidiForHost("false") = true, want false`)
	}
}
