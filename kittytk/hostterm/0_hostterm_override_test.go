package hostterm

import "testing"

// The SDL host pins its own identity so it is not mis-detected as the terminal
// it was launched from. Override must win over environment detection, and the
// kind must carry a stable string.
func TestOverridePinsSDL(t *testing.T) {
	if TerminalSDL.String() != "sdl" {
		t.Fatalf("TerminalSDL.String() = %q, want sdl", TerminalSDL.String())
	}
	// Detection from an Apple Terminal environment classifies as Apple…
	if got := detect(func(k string) string {
		if k == "TERM_PROGRAM" {
			return "Apple_Terminal"
		}
		return ""
	}); got != TerminalAppleTerminal {
		t.Fatalf("sanity: Apple env detects %v, want Apple", got)
	}
	// …but an explicit Override wins for the process (SDL host launched from it).
	Override(TerminalSDL)
	if got := Detect(); got != TerminalSDL {
		t.Fatalf("after Override, Detect() = %v, want TerminalSDL", got)
	}
}

// What a terminal does with what it is sent, per terminal -- and unknown as its
// own answer rather than a guess.
//
// It matters to anything holding a line it has already ORDERED. macOS
// Terminal.app orders such a line again; the stream-order terminals leave it
// alone, and turning a run back for one of those is itself the bug.
func TestABidiProfileIsPerTerminal(t *testing.T) {
	if applies, wordwise, rideSafe, known := BidiProfile(TerminalAppleTerminal); !known || !applies || wordwise || !rideSafe {
		t.Errorf("Apple Terminal: applies=%v wordwise=%v rideSafe=%v known=%v, want a "+
			"known whole-span reorderer that miscounts a fill",
			applies, wordwise, rideSafe, known)
	}
	for _, k := range []Kind{
		TerminalITerm2, TerminalGhostty, TerminalKitty, TerminalAlacritty,
		TerminalCoolRetroTerm, TerminalPurfecterm,
	} {
		if applies, _, rideSafe, known := BidiProfile(k); !known || applies || rideSafe {
			t.Errorf("%v: applies=%v rideSafe=%v known=%v, want a known stream-order "+
				"terminal", k, applies, rideSafe, known)
		}
	}
	if _, _, _, known := BidiProfile(TerminalUnknown); known {
		t.Error("an unrecognised terminal was answered for rather than left unknown")
	}
}

// The graphical host draws natively: nothing it renders reaches a terminal, so
// the quirks of whatever terminal LAUNCHED it are about a journey its pixels
// never take.
func TestTheGraphicalHostCarriesNoTerminalsQuirks(t *testing.T) {
	applies, _, _, known := BidiProfile(TerminalSDL)
	if !known {
		t.Fatal("the graphical host was left unknown")
	}
	if applies {
		t.Error("the graphical host was taken for a terminal that reorders")
	}

	// And it says so itself rather than inheriting: pinned, it reports its own
	// identity whatever the environment claims.
	t.Cleanup(func() { Override(TerminalUnknown) })
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	Override(TerminalSDL)
	if got := Detect(); got != TerminalSDL {
		t.Errorf("launched from Apple Terminal, the graphical host detects %v", got)
	}
}
