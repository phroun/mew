package editor

import (
	"strings"
	"testing"
)

// press feeds one key through the processor and executes the resulting
// command, as the main loop does.
func press(e *Editor, key string) {
	if result := e.KeyProcessor.ProcessKey(key); result.Command != "" {
		e.executeCommand(result.Command)
	}
}

// An unmapped Meta key re-inserts the macOS Option character; bindings
// steal individual combos; false disables the layer.
func TestMacOptionInsertFallback(t *testing.T) {
	e, w := newTestEditor(t, "")
	press(e, "M-s")
	if got := docContent(w); got != "ß" {
		t.Fatalf("unmapped M-s should insert ß, got %q", got)
	}
	// M-d and M-5 are unbound in the shipped defaults (M-a/e/n/p/f/j drive
	// scroll/word/page), so they fall through to the Option-character insert.
	press(e, "M-d")
	press(e, "M-5")
	if got := docContent(w); got != "ß∂∞" {
		t.Fatalf("letter and number combos should insert, got %q", got)
	}

	// A binding steals the combo: no insertion, the command runs instead.
	e.PawScript.ExecuteAsync("map 'M-s', 'nop'")
	press(e, "M-s")
	if got := docContent(w); got != "ß∂∞" {
		t.Fatalf("bound M-s must not insert, got %q", got)
	}

	// Unknown Meta combos still do nothing.
	press(e, "M-F1")
	if got := docContent(w); got != "ß∂∞" {
		t.Fatalf("unknown meta keys stay ignored, got %q", got)
	}
}

// macOptionKeys=false turns the reverse-insert layer off.
func TestMacOptionKeysOff(t *testing.T) {
	e, w := newTestEditor(t, "", "macOptionKeys=false")
	press(e, "M-s")
	if got := docContent(w); got != "" {
		t.Fatalf("layer off: M-s must insert nothing, got %q", got)
	}

	// And back on at runtime.
	e.PawScript.ExecuteAsync("set_option 'macOptionKeys', 'true'")
	press(e, "M-s")
	if got := docContent(w); got != "ß" {
		t.Fatalf("layer re-enabled: got %q", got)
	}
	if v, _ := e.getOption(nil, "macOptionKeys"); v != "true" {
		t.Fatalf("option round-trip: %q", v)
	}
}

// The default is auto, and symbol combos round-trip through the escape
// handling (M-' inserts æ, M-\ inserts «).
func TestMacOptionDefaults(t *testing.T) {
	e, w := newTestEditor(t, "")
	if v, _ := e.getOption(nil, "macOptionKeys"); v != "auto" {
		t.Fatalf("default should be auto, got %q", v)
	}
	press(e, "M-'")
	press(e, "M-\\")
	if got := docContent(w); !strings.Contains(got, "æ") || !strings.Contains(got, "«") {
		t.Fatalf("symbol combos should insert, got %q", got)
	}
}
