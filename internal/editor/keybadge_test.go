package editor

import (
	"testing"

	"github.com/phroun/key-sequence-processor/keyseq"
	"github.com/phroun/mew/internal/config"
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

// Every modifier in the vocabulary spells out. A prefix missing from the
// peeling loop is not merely unspelled — it survives into the base key and is
// printed raw ("C-x"), which is exactly what this helper exists to avoid.
func TestVerboseKeySequenceSpellsEveryModifier(t *testing.T) {
	none := func(string) bool { return false }
	cases := []struct{ seq, want string }{
		{"C-x", "Ctrl+X"},          // the long spelling of ^
		{"G-€", "Glyph+€"},         // a Glyph chord carries its own character
		{"m-pgup", "Meta+Page Up"}, // reads friendly: nothing else claims the word
		{"H-fdel", "Hyper+FDel"},
		{"M-m-home", "Mega+Micro+Home"}, // both keys at once: one word cannot do
		{"C-S-home", "Ctrl+Shift-Home"}, // Shift still glues to the base
		{"^C-x", "Ctrl+X"},              // both spellings of one modifier: said once
	}
	for _, c := range cases {
		if got := verboseKeySequence(c.seq, none); got != c.want {
			t.Errorf("verboseKeySequence(%q) = %q, want %q", c.seq, got, c.want)
		}
	}
}

// The keypad reads as a place, not as a held key.
//
// A badge is prose — it is what someone would say out loud — and nobody says
// "P plus Home". They say "Keypad Home". Both cases of the prefix get the same
// word: the lowercase form marks WHICH of two duplicated pad keys a keyboard
// sent, which is a keymap's business and means nothing to someone reading a
// menu. Without the prefix in the vocabulary at all, the badge would have shown
// the raw token, "P-home", which is the one thing a spelled-out badge exists
// not to do.
func TestVerboseKeySequenceSpellsTheKeypad(t *testing.T) {
	none := func(string) bool { return false }
	for _, c := range []struct{ seq, want string }{
		{"P-home", "Keypad Home"},
		{"p-home", "Keypad Home"}, // the duplicate reads the same to a reader
		{"P-begin", "Keypad Begin"},
		{"P-return", "Keypad Return"},
		{"C-P-pgup", "Ctrl+Keypad Page Up"},
		{"P-7", "Keypad 7"},
		// "Meta+", not "Mega+": with only one of the pair bound the friendly
		// word is still usable, which is the rule everywhere else here.
		{"M-P-del", "Meta+Keypad Delete"},
		// The keys with no American character are proper names, and a badge
		// that showed mew's lowercase token would read as a typo.
		{"zig", "Zig"},
		{"C-hanja", "Ctrl+Hanja"},
		{"power", "Power"},
	} {
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
	e.KeyProcessor = keyseq.NewProcessor(nil)
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
	e.KeyProcessor = keyseq.NewProcessor(nil)
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
	e.KeyProcessor = keyseq.NewProcessor(nil)
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
	e.KeyProcessor = keyseq.NewProcessor(nil)
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

// A binding written with a (capture)/(override) level word is filed under that
// RAW spelling, while the badge shows the key as PRESSED. Provenance is looked
// up by the raw spelling, so a levelled binding keeps the precedence it was configured
// with: here the user's `(capture) ^/` outranks the built-in ^_ and wins the
// "last configured" tie-break, even though the two are shown identically to
// how they are pressed.
//
// Looking provenance up by the displayed key instead missed every levelled
// binding: it read as System/precedence 0 - a built-in - so a binding written
// to outrank one could lose to it.
func TestKeyBindingDisplayHonorsPrefixedProvenance(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keyseq.NewProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{
		"^_":           "buffer_undo", // built-in, no origin recorded
		"(capture) ^/": "buffer_undo", // the user's, at a capture level
	})
	e.mappingOrigins = map[string]config.MappingOrigin{
		"(capture) ^/": {Precedence: 7, Author: config.AuthorCustomized},
	}

	for i := 0; i < 20; i++ { // map iteration order varies; the answer must not
		if got := e.keyBindingDisplay("buffer_undo", ""); got != "^/" {
			t.Fatalf("badge = %q, want ^/ - the configured binding outranks the built-in", got)
		}
	}
}

// ...and the level word itself is never shown: the badge is the key as pressed.
func TestKeyBindingDisplayNeverShowsAPrefix(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keyseq.NewProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{"(override) ^B S": "buffer_save"})

	if got := e.keyBindingDisplay("buffer_save", ""); got != "^B S" {
		t.Errorf("badge = %q, want ^B S with no level word", got)
	}
}

// A binding hinted for THIS machine is what the badge shows, even against a
// binding configured later. The environment is a statement about the machine
// rather than about the file, so it outranks load order — and it decides
// nothing else: both keys are bound, and either one pressed still works.
func TestKeyBindingDisplayPrefersThisEnvironmentsSpelling(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keyseq.NewProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{
		"s-c": "clipboard_copy",
		"^C":  "clipboard_copy",
	})
	// ^C is configured LATER, so load order alone would show it.
	e.mappingOrigins = map[string]config.MappingOrigin{
		"s-c": {Precedence: 1, EnvWeight: 1},
		"^C":  {Precedence: 2},
	}
	for i := 0; i < 20; i++ { // map iteration order varies; the answer must not
		if got := e.keyBindingDisplay("clipboard_copy", ""); got != "s-c" {
			t.Fatalf("badge = %q, want s-c - this machine's own spelling outranks the later one", got)
		}
	}

	// ...and somewhere the hint does not hold, the same table shows the other.
	e.mappingOrigins = map[string]config.MappingOrigin{
		"s-c": {Precedence: 1, EnvWeight: -1},
		"^C":  {Precedence: 2},
	}
	if got := e.keyBindingDisplay("clipboard_copy", ""); got != "^C" {
		t.Errorf("badge = %q, want ^C - the Mac spelling is demoted off a Mac", got)
	}
}

// Mega and Micro read as the familiar "Meta" until both are bound, and are told
// apart the moment they are.
//
// The rule tracks the key sequence processor rather than taste. M- and m- fall
// back to each other there: bind one and either press reaches it, bind both and
// they stay apart. So while only one is bound there is nothing a reader could
// do with the distinction — and "Micro+X" would name a key most keyboards do
// not have, when the one they do have works. Once both are bound the press
// decides which binding runs, and the badge has to say which.
func TestVerboseKeySequenceMetaDisambiguation(t *testing.T) {
	only := func(string) bool { return false }
	for _, c := range []struct{ seq, want, what string }{
		{"M-x", "Meta+X", "only Mega bound"},
		{"m-x", "Meta+X", "only Micro bound: the reachable key is still Meta"},
	} {
		if got := verboseKeySequence(c.seq, only); got != c.want {
			t.Errorf("%s: verboseKeySequence(%q) = %q, want %q", c.what, c.seq, got, c.want)
		}
	}

	// A keymap that binds both: the fallback no longer applies, so the words
	// have to separate.
	bound := map[string]bool{"M-x": true, "m-x": true}
	both := func(s string) bool { return bound[s] }
	for _, c := range []struct{ seq, want, what string }{
		{"M-x", "Mega+X", "both bound: Mega named"},
		{"m-x", "Micro+X", "both bound: Micro named"},
	} {
		if got := verboseKeySequence(c.seq, both); got != c.want {
			t.Errorf("%s: verboseKeySequence(%q) = %q, want %q", c.what, c.seq, got, c.want)
		}
	}

	// One token holding both is always disambiguated, whatever is bound:
	// "Meta+Meta+Home" would say one word for two keys held together.
	if got := verboseKeySequence("M-m-home", only); got != "Mega+Micro+Home" {
		t.Errorf("both modifiers in one token = %q, want %q", got, "Mega+Micro+Home")
	}

	// Disambiguation is per chord, not global: binding both forms of x says
	// nothing about y, which stays friendly.
	if got := verboseKeySequence("M-y", both); got != "Meta+Y" {
		t.Errorf("an unrelated chord = %q, want %q", got, "Meta+Y")
	}
}
