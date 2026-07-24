package editor

import (
	"testing"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/keys"
)

// keysRefAction extracts and DECODES the action from a keys#... anchor: "."
// decodes to "|" and "," to "&", because a dokuwiki anchor cannot carry those
// literally (a fallback-chain command name contains "|").
func TestKeysRefAction(t *testing.T) {
	cases := []struct {
		target      string
		want        string
		wantVerbose bool
		ok          bool
	}{
		{"keys#go_page_prior", "go_page_prior", false, true},
		{"keys#buffer_undo", "buffer_undo", false, true},
		{"keys#buffer_redo.buffer_undo", "buffer_redo|buffer_undo", false, true}, // . -> |
		{"keys#a,b", "a&b", false, true},                                         // , -> &
		{"keys# spaced ", "spaced", false, true},
		{"keys#", "", false, false},
		{"help:keys#x", "", false, false}, // must be the bare "keys" page
		{"go_page_prior", "", false, false},
		{"keys_verbose#go_page_prior", "go_page_prior", true, true}, // verbose variant
		{"keys_verbose#buffer_redo.buffer_undo", "buffer_redo|buffer_undo", true, true},
		{"keys_verbose#", "", true, false},
	}
	for _, c := range cases {
		got, verbose, ok := keysRefAction(c.target)
		if got != c.want || verbose != c.wantVerbose || ok != c.ok {
			t.Errorf("keysRefAction(%q) = (%q,%v,%v), want (%q,%v,%v)",
				c.target, got, verbose, ok, c.want, c.wantVerbose, c.ok)
		}
	}
}

// verboseKeySequence spells a binding out for beginners: modifiers as words,
// chord keys joined with then / followed by / and finally, and Shift derived
// from letter case for non-Ctrl keys only.
func TestVerboseKeySequence(t *testing.T) {
	cases := []struct{ seq, want string }{
		{"^B", "Ctrl+B"},
		{"^B O", "Ctrl+B then O"},
		{"^K F", "Ctrl+K then F"},
		{"M-b", "Meta+B"},                         // non-Ctrl lowercase: no Shift
		{"M-B", "Meta+Shift-B"},                   // non-Ctrl uppercase: Shift
		{"^C", "Ctrl+C"},                          // Ctrl uppercase: NOT Shift
		{"S-tab", "Shift-Tab"},                    // explicit Shift on a named key
		{"s-x", "Super+X"},                        // Super
		{"^M-b", "Ctrl+Meta+B"},                   // combined modifiers
		{"esc x", "Esc then X"},                   // named key + letter chord
		{"^B C D", "Ctrl+B then C followed by D"}, // 3 keys: no "and finally"
		{"^B C D E", "Ctrl+B then C followed by D and finally E"},    // 4 keys
		{"a b c d e", "A then B followed by C then D and finally E"}, // 5 keys
	}
	for _, c := range cases {
		if got := verboseKeySequence(c.seq); got != c.want {
			t.Errorf("verboseKeySequence(%q) = %q, want %q", c.seq, got, c.want)
		}
	}
}

// keyBindingDisplay picks ONE key per badge: the candidate set is every key
// bound EXACTLY to the action, and the choice among them ranks each key
// SEQUENCE against the author's alias (exact, then closest beginning, then
// closest end), with load-order precedence as the tie-break.
func TestKeyBindingDisplay(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keys.NewSequenceProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{
		"^/":   "buffer_undo",
		"^_":   "buffer_undo",
		"^B -": "buffer_undo",
		"^Z":   "buffer_redo|buffer_undo", // a fallback chain
		"^B =": "buffer_redo",
	})
	// Distinct precedences so "last configured" is unambiguous.
	e.mappingOrigins = map[string]config.MappingOrigin{
		"^/":   {Precedence: 1},
		"^_":   {Precedence: 2},
		"^B -": {Precedence: 3},
		"^Z":   {Precedence: 4},
		"^B =": {Precedence: 5},
	}

	cases := []struct {
		name, action, preferred, want string
	}{
		{"exact alias wins", "buffer_undo", "^_", "^_"},
		{"no alias -> last configured", "buffer_undo", "", "^B -"},          // highest precedence of the three
		{"closest beginning", "buffer_undo", "^B W", "^B -"},                // shares "^B " with ^B -
		{"closest end", "buffer_undo", "X -", "^B -"},                       // no shared start; shares " -" at the end
		{"tie on beginning -> last configured", "buffer_undo", "^", "^B -"}, // all share "^"; prec breaks it
		{"single exact-command chain", "buffer_redo|buffer_undo", "", "^Z"},
		{"primary alone is not a binding", "buffer_redo", "", "^B ="}, // ^B = is bound to bare buffer_redo
		// ^Z runs a chain, not buffer_undo exactly, so it is not a candidate; the
		// alias "^Z" shares only "^" with the real candidates, so it ties and
		// last-configured (^B -) wins.
		{"a chain is never a candidate", "buffer_undo", "^Z", "^B -"},
		{"unbound -> documented alias", "nonexistent", "^X", "^X"},
		{"unbound, no alias -> action name", "nonexistent", "", "nonexistent"},
	}
	for _, c := range cases {
		if got := e.keyBindingDisplay(c.action, c.preferred); got != c.want {
			t.Errorf("%s: keyBindingDisplay(%q,%q) = %q, want %q", c.name, c.action, c.preferred, got, c.want)
		}
	}
}

// With no provenance (built-in keymap, every key at precedence 0), ties fall
// back to a deterministic stand-in for "last": the greater sequence text.
func TestKeyBindingDisplayBuiltinTieIsDeterministic(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keys.NewSequenceProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{
		"^/": "buffer_undo",
		"^_": "buffer_undo",
	})
	// e.mappingOrigins stays nil: both keys resolve as System/precedence 0.
	for i := 0; i < 20; i++ { // map iteration order varies; result must not
		if got := e.keyBindingDisplay("buffer_undo", ""); got != "^_" {
			t.Fatalf("builtin tie should deterministically pick ^_ (greater), got %q", got)
		}
	}
}
