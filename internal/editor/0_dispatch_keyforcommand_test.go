package editor

import "testing"

// KeyForCommand returns the stored spelling of the key bound to a command, the
// lexicographically-first when several are bound, and "" when unbound.
func TestKeyForCommand(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hi\n")

	e.KeyProcessor.MapKey("^X 1", "only_here")
	if got := e.KeyForCommand("only_here"); got != "^X 1" {
		t.Errorf("KeyForCommand = %q, want %q", got, "^X 1")
	}

	// Two bindings: the lexicographically-first stored spelling wins (stable).
	e.KeyProcessor.MapKey("^X 2", "two_ways")
	e.KeyProcessor.MapKey("^A 9", "two_ways")
	if got := e.KeyForCommand("two_ways"); got != "^A 9" {
		t.Errorf("KeyForCommand (multi) = %q, want %q", got, "^A 9")
	}

	if got := e.KeyForCommand("nonexistent_command"); got != "" {
		t.Errorf("unbound command should resolve to empty, got %q", got)
	}
}
