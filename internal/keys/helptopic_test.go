package keys

import "testing"

// HelpTopic resolves the "help" virtual binding for a key prefix, walking from
// the deepest matching prefix down to the root: "^B" prefers "^B help", the
// root prefers a bare "help", and a prefix with no help binding falls back to a
// shorter one that has it.
func TestHelpTopicResolvesDeepestPrefix(t *testing.T) {
	sp := NewSequenceProcessor(nil)
	sp.SetMappings(map[string]string{
		"help":    "keys",
		"^B help": "keys_buffer",
		"^B P":    "window_prior",
		"^K B":    "mark_begin",
		"^K B ^K": "noop",
		"^X ^S":   "buffer_save",
	})

	cases := []struct{ seq, want string }{
		{"", "keys"},          // root
		{"^B", "keys_buffer"}, // deepest match
		{"^K", "keys"},        // no "^K help": fall back to root
		{"^K B", "keys"},      // still no help under ^K: root
		{"^X", "keys"},        // root fallback
	}
	for _, c := range cases {
		if got := sp.HelpTopic(c.seq); got != c.want {
			t.Errorf("HelpTopic(%q) = %q, want %q", c.seq, got, c.want)
		}
	}
}

// With no help bindings at all, HelpTopic is always empty (Quick Help then
// falls back to its built-in reference).
func TestHelpTopicEmptyWithoutBindings(t *testing.T) {
	sp := NewSequenceProcessor(nil)
	sp.SetMappings(map[string]string{"^B P": "window_prior"})
	if got := sp.HelpTopic("^B"); got != "" {
		t.Errorf("HelpTopic without help bindings = %q, want empty", got)
	}
}

// The "help" pseudo-key names a topic; it must never surface as a pressable key
// completion after a prefix.
func TestHelpVirtualKeyExcludedFromCompletions(t *testing.T) {
	sp := NewSequenceProcessor(nil)
	sp.SetMappings(map[string]string{
		"^B help": "keys_buffer",
		"^B P":    "window_prior",
		"^B N":    "window_next",
	})
	sp.ProcessKey("^B")
	for _, c := range sp.GetPossibleCompletions() {
		if c == "help" {
			t.Fatalf("completions must not include the virtual \"help\" key: %v", sp.GetPossibleCompletions())
		}
	}
}
