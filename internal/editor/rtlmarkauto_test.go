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

// flipBidiForHost="auto": Apple Terminal flips whole-run and needs the ride-safe
// selection; Kitty flips word-wise and keeps the real bar; the stream-order
// terminals do not flip (all recognised, so all skip the probe); an unrecognised
// host is left to the probe. Explicit true/false pass through.
func TestFlipBidiForHostResolve(t *testing.T) {
	cases := map[hostterm.Kind]hostBidiProfile{
		hostterm.TerminalAppleTerminal: {flip: true, wordwise: false, rideSafe: true, known: true},
		hostterm.TerminalKitty:         {flip: true, wordwise: true, rideSafe: false, known: true},
		hostterm.TerminalITerm2:        {known: true},
		hostterm.TerminalAlacritty:     {known: true},
		hostterm.TerminalGhostty:       {known: true},
		hostterm.TerminalUnknown:       {},
		hostterm.TerminalPurfecterm:    {},
	}
	for k, want := range cases {
		if got := hostBidiProfileFor(k); got != want {
			t.Errorf("hostBidiProfileFor(%s) = %+v, want %+v", k, got, want)
		}
	}
	if !resolveFlipBidiForHost("true") {
		t.Errorf(`resolveFlipBidiForHost("true") = false, want true`)
	}
	if resolveFlipBidiForHost("false") {
		t.Errorf(`resolveFlipBidiForHost("false") = true, want false`)
	}
}
