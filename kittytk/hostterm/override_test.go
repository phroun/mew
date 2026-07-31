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
