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
		hostterm.TerminalGhostty:       "drift",
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

// flipBidiForHost="auto" takes its answer from hostterm.BidiProfile, the one
// table that says what a terminal does with what it is sent. mew keeps no
// second copy of it -- it had one, and the two disagreed about kitty for as
// long as both existed. Explicit true/false do not consult it at all.
func TestFlipBidiForHostResolve(t *testing.T) {
	t.Cleanup(func() { hostterm.Override(hostterm.TerminalUnknown) })

	// A reordering host, whole-span, whose fill cannot be trusted.
	hostterm.Override(hostterm.TerminalAppleTerminal)
	if flip, wordwise, rideSafe, known := flipSettings("auto"); !flip || wordwise || !rideSafe || !known {
		t.Errorf("Apple Terminal: auto gave flip=%v wordwise=%v rideSafe=%v known=%v",
			flip, wordwise, rideSafe, known)
	}

	// One that leaves what it is sent alone.
	hostterm.Override(hostterm.TerminalITerm2)
	if flip, _, rideSafe, known := flipSettings("auto"); flip || rideSafe || !known {
		t.Errorf("iTerm2: auto gave flip=%v rideSafe=%v known=%v", flip, rideSafe, known)
	}

	// And one no name recognises, left to the probe.
	hostterm.Override(hostterm.TerminalUnknown)
	if _, _, _, known := flipSettings("auto"); known {
		t.Error("an unrecognised host was answered for rather than left to the probe")
	}

	// Explicit settings ignore the host entirely.
	hostterm.Override(hostterm.TerminalITerm2)
	if flip, _, rideSafe, known := flipSettings("true"); !flip || !rideSafe || !known {
		t.Errorf(`flipSettings("true") = flip=%v rideSafe=%v known=%v on a stream-order `+
			"host, want the setting honoured", flip, rideSafe, known)
	}
	hostterm.Override(hostterm.TerminalAppleTerminal)
	if flip, _, _, known := flipSettings("false"); flip || !known {
		t.Errorf(`flipSettings("false") = flip=%v known=%v on a reordering host, want `+
			"the setting honoured", flip, known)
	}

	if !resolveFlipBidiForHost("true") {
		t.Errorf(`resolveFlipBidiForHost("true") = false, want true`)
	}
	if resolveFlipBidiForHost("false") {
		t.Errorf(`resolveFlipBidiForHost("false") = true, want false`)
	}
}
