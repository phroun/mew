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
// explicit true/false pass through.
func TestFlipBidiForHostResolve(t *testing.T) {
	flip := map[hostterm.Kind]bool{
		hostterm.TerminalAppleTerminal: true,
		hostterm.TerminalITerm2:        false,
		hostterm.TerminalAlacritty:     false,
		hostterm.TerminalGhostty:       false,
		hostterm.TerminalUnknown:       false,
	}
	for k, w := range flip {
		if got := flipBidiForTerminal(k); got != w {
			t.Errorf("flipBidiForTerminal(%s) = %v, want %v", k, got, w)
		}
	}
	if !resolveFlipBidiForHost("true") {
		t.Errorf(`resolveFlipBidiForHost("true") = false, want true`)
	}
	if resolveFlipBidiForHost("false") {
		t.Errorf(`resolveFlipBidiForHost("false") = true, want false`)
	}
}
