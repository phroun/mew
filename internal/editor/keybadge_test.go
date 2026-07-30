package editor

import (
	"testing"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/keys"
	"github.com/phroun/mew/internal/plugins"
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

// verboseKeySequence spells a binding out for beginners: modifiers as words and
// chord keys joined with then / followed by / and finally. Letters case-fold, so
// no Shift is shown unless a token is written with S- or its case-flipped
// binding also exists (disambiguation).
func TestVerboseKeySequence(t *testing.T) {
	// Nothing else is bound, so no letter case is significant.
	none := func(string) bool { return false }
	cases := []struct{ seq, want string }{
		{"^B", "Ctrl+B"},
		{"^B O", "Ctrl+B then O"},
		{"^K F", "Ctrl+K then F"},
		{"M-b", "Meta+B"},
		{"M-B", "Meta+B"}, // uppercase but not disambiguated: case-folded, no Shift
		{"^C", "Ctrl+C"},
		{"S-tab", "Shift-Tab"}, // explicit Shift on a named key
		{"s-x", "Super+X"},
		{"^M-b", "Ctrl+Meta+B"},
		{"esc x", "Esc then X"},
		{"^B C D", "Ctrl+B then C followed by D"},
		{"^B C D E", "Ctrl+B then C followed by D and finally E"},
		{"a b c d e", "A then B followed by C then D and finally E"},
	}
	for _, c := range cases {
		if got := verboseKeySequence(c.seq, none); got != c.want {
			t.Errorf("verboseKeySequence(%q) = %q, want %q", c.seq, got, c.want)
		}
	}
}

// When both case variants of a key are bound, the case disambiguates them and
// Shift is shown for the uppercase one.
func TestVerboseKeySequenceShiftDisambiguation(t *testing.T) {
	// A keymap where both M-b/M-B and both ^c/^C exist.
	bound := map[string]bool{"M-b": true, "M-B": true, "^c": true, "^C": true}
	isBound := func(s string) bool { return bound[s] }
	cases := []struct{ seq, want string }{
		{"M-b", "Meta+B"},       // lowercase variant: no Shift
		{"M-B", "Meta+Shift-B"}, // uppercase variant: Shift (disambiguated)
		{"^c", "Ctrl+C"},        // lowercase Ctrl variant
		{"^C", "Ctrl+Shift-C"},  // uppercase Ctrl variant: Shift (disambiguated)
	}
	for _, c := range cases {
		if got := verboseKeySequence(c.seq, isBound); got != c.want {
			t.Errorf("verboseKeySequence(%q) = %q, want %q", c.seq, got, c.want)
		}
	}
}

// tfcKeyResolver resolves %keys#…% / %keys_verbose#…% TFC codes to live
// bindings, wrapped in the call site's ANSI, and returns ok=false for anything
// that is not a keys# reference (left verbatim by the engine).
func TestTFCKeyResolver(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keys.NewSequenceProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{"^B S": "buffer_save"})

	res := e.tfcKeyResolver("<", ">") // ANSI stand-ins
	cases := []struct {
		code, want string
		ok         bool
	}{
		{"keys#buffer_save", "<^B S>", true},                  // resolves + wraps
		{"keys#buffer_save|^K S", "<^B S>", true},             // bound wins over the alias
		{"keys#no_such_command|^K S", "<^K S>", true},         // unbound -> the alias
		{"keys_verbose#buffer_save", "<Ctrl+B then S>", true}, // spelled out
		{"FN", "", false}, // not a keys# code
		{"line:%d", "", false},
	}
	for _, c := range cases {
		got, ok := res(c.code)
		if got != c.want || ok != c.ok {
			t.Errorf("resolver(%q) = (%q,%v), want (%q,%v)", c.code, got, ok, c.want, c.ok)
		}
	}
}

// The TFC engine resolves %keys#…% through the editor's resolver end to end.
func TestExpandTFCResolvesKeysCode(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keys.NewSequenceProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{"^B S": "buffer_save"})
	got := plugins.ExpandTFC("Save with %keys_verbose#buffer_save%.", nil, e.tfcKeyResolver("", ""))
	if got != "Save with Ctrl+B then S." {
		t.Errorf("ExpandTFC = %q", got)
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
