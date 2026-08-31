package editor

import (
	"testing"

	"github.com/phroun/key-sequence-processor/keyseq"
)

func newDefaultsEditor(macOption bool) *Editor {
	e := &Editor{KeyProcessor: keyseq.NewProcessor(nil)}
	e.KeyProcessor.SetMacOptionInsert(macOption)
	return e
}

// The floor an unbound key falls to, in mew's own command vocabulary. These
// moved out of the key sequence processor when it became a library: the
// resolution rules are general, but what a key MEANS is mew's.
func TestDefaultCommandForKey(t *testing.T) {
	e := newDefaultsEditor(false)
	cases := []struct{ key, want string }{
		{"space", "insert ' '"},
		{"del", "nav_history_prior false|del_char_prior"},
		{"back", "nav_history_prior false|del_char_prior"},
		{"return", "nav_follow false|accept|insert_newline"},
		{"^C", "cancel|viewport_close"},
		{"esc", "cmd"},
		{"q", "insert 'q'"},
		{"€", "insert '€'"},
		// A Glyph chord unrolls to the character it carries, so AltGr typing
		// still works when nothing binds it.
		{"G-€", "insert '€'"},
		// Named and modified keys have no default: they do nothing unbound.
		{"F1", ""},
		{"pgup", ""},
		{"M-x", ""},
	}
	for _, c := range cases {
		if got := e.defaultCommandForKey(c.key); got != c.want {
			t.Errorf("defaultCommandForKey(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// A typed quote or backslash must not break out of the insert command it is
// spelled into.
func TestDefaultCommandEscapesLiterals(t *testing.T) {
	e := newDefaultsEditor(false)
	cases := []struct{ key, want string }{
		{"'", `insert '\''`},
		{"\\", `insert '\\'`},
	}
	for _, c := range cases {
		if got := e.defaultCommandForKey(c.key); got != c.want {
			t.Errorf("defaultCommandForKey(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// With the Option layer on, an unbound Meta key types the character Option
// composes — the processor holds the table, mew spells the command.
func TestDefaultCommandMacOptionLayer(t *testing.T) {
	on := newDefaultsEditor(true)
	if got, want := on.defaultCommandForKey("M-d"), "insert '∂'"; got != want {
		t.Errorf("M-d with the layer on = %q, want %q", got, want)
	}
	off := newDefaultsEditor(false)
	if got := off.defaultCommandForKey("M-d"); got != "" {
		t.Errorf("M-d with the layer off = %q, want no default", got)
	}
}

// mew renames keys on the way in, so the processor's default fallback groups
// (which speak direct-key-handler's Tab/Return/Escape) do not fit: without
// mew's own groups the control spellings silently stop resolving — `^M` no longer
// reaches a `return` binding, and nothing errors, because a binding that fails
// to match just does nothing.
func TestMewFallbackGroupsResolveControlSpellings(t *testing.T) {
	p := keyseq.NewProcessor(nil)
	p.SetFallbackGroups(mewFallbackGroups)
	p.SetMappings(map[string]string{
		"return": "accept",
		"back":   "erase",
		"esc":    "cmd",
		"tab":    "indent",
		"fdel":   "del_forward",
	})
	cases := []struct{ pressed, want string }{
		{"return", "accept"}, {"^M", "accept"}, {"enter", "accept"},
		{"back", "erase"}, {"^H", "erase"}, {"backspace", "erase"},
		{"esc", "cmd"}, {"^[", "cmd"}, {"escape", "cmd"}, {"^3", "cmd"},
		{"tab", "indent"}, {"^I", "indent"},
		{"fdel", "del_forward"}, // "delete" is deliberately NOT a spelling for it
	}
	for _, c := range cases {
		p.ClearActiveSequence()
		if got := p.ProcessKey(c.pressed).Command; got != c.want {
			t.Errorf("%q -> %q, want %q", c.pressed, got, c.want)
		}
	}
}

// The punctuation spellings let a keymap name a key the binding syntax would
// otherwise fight over: `-` is the modifier separator, so `M--` reads badly and
// `^-` cannot even show where the modifier stops. Nothing arrives under these
// names — they exist for the keymap side only.
func TestMewSpellingsNamePunctuation(t *testing.T) {
	p := keyseq.NewProcessor(nil)
	p.SetFallbackGroups(mewFallbackGroups)
	p.SetMappings(map[string]string{
		"minus":        "shrink",
		"M-equals":     "grow",
		"^backslash":   "split",
		"^K semicolon": "comment",
		"pipe x":       "chain",
	})
	cases := []struct {
		pressed []string
		want    string
	}{
		{[]string{"-"}, "shrink"},
		{[]string{"M-="}, "grow"},
		{[]string{"^\\"}, "split"},
		{[]string{"^K", ";"}, "comment"},
		{[]string{"|", "x"}, "chain"},
	}
	for _, c := range cases {
		p.ClearActiveSequence()
		var got string
		for _, k := range c.pressed {
			got = p.ProcessKey(k).Command
		}
		if got != c.want {
			t.Errorf("pressed %v -> %q, want %q", c.pressed, got, c.want)
		}
	}
}

// A chord no binding claimed types what the HOST watched this keyboard produce
// for it — every chord, a plain letter as much as a modified one.
//
// Asked first. What mew can derive from a chord's own NAME is a good guess and
// only a guess: right whenever a key types what it is called, with nothing in
// the name to say when that stops being true. The host saw both halves of the
// keystroke and mew sees neither.
func TestUnboundChordTypesWhatTheHostObserved(t *testing.T) {
	e, _ := newTestEditor(t, "")
	e.Config.KeyChordText = func(chord string) (string, bool) {
		switch chord {
		case "s-q":
			return "œ", true // a chord this layout does something with
		case "a":
			return "ä", true // and a plain key it does something with too
		case "G-€":
			return "€", true
		}
		return "", false
	}
	// The floor asks the PROCESSOR, which holds the precedence; the host's
	// lookup reaches it the way the editor installs it.
	e.KeyProcessor.SetKeyChordText(e.Config.KeyChordText)

	for _, c := range []struct{ key, want string }{
		{"s-q", "insert 'œ'"},
		{"a", "insert 'ä'"},
		{"G-€", "insert '€'"},
	} {
		if got := e.defaultCommandForKey(c.key); got != c.want {
			t.Errorf("%s -> %q, want %q", c.key, got, c.want)
		}
	}

	// A chord the host never saw falls to what the name can carry.
	if got := e.defaultCommandForKey("b"); got != "insert 'b'" {
		t.Errorf("an unobserved plain key -> %q, want its own character", got)
	}
	if got := e.defaultCommandForKey("G-µ"); got != "insert 'µ'" {
		t.Errorf("an unobserved glyph -> %q, want the glyph the token carries", got)
	}
	if got := e.defaultCommandForKey("s-z"); got != "" {
		t.Errorf("an unobserved chord with nothing to derive -> %q, want nothing", got)
	}
}

// The observation answers whatever the switch says.
//
// macOptionKeys governs the TABLE — a guess about a keyboard nobody looked at
// — and turning it off says stop guessing, not stop typing. A terminal with the
// layer off types the character anyway, because the character is what arrives
// there; a graphical host that fell silent instead would be the odd one out.
func TestObservationIsNotGatedByTheOptionSwitch(t *testing.T) {
	e, _ := newTestEditor(t, "")
	e.Config.MacOptionKeys = "false"
	e.applyMacOptionKeys()
	e.Config.KeyChordText = func(chord string) (string, bool) {
		if chord == "M-a" {
			return "å", true
		}
		return "", false
	}
	// The floor asks the PROCESSOR, which holds the precedence; the host's
	// lookup reaches it the way the editor installs it.
	e.KeyProcessor.SetKeyChordText(e.Config.KeyChordText)

	if got := e.defaultCommandForKey("M-a"); got != "insert 'å'" {
		t.Errorf("M-a -> %q, want the character this keyboard was seen typing", got)
	}
}

// A modifier pressed by itself never reaches the keymap.
//
// The key layer reports these under the kitty protocol so something watching
// the keyboard can see which cap went down. mew is not that something: no
// binding is written against one, and the sequence processor must never see one
// — it would count as the next key of a multi-key sequence, so holding Shift
// mid-chord would end the chord.
func TestBareModifierPressesAreNotKeystrokes(t *testing.T) {
	for _, key := range []string{
		"LMod:S", "RMod:C", "LMod:M", "RMod:m", "LMod:s", "Mod:H",
	} {
		if !isBareModifierKey(key) {
			t.Errorf("%s was not recognized as a bare modifier", key)
		}
		e, w := newTestEditor(t, "")
		e.dispatchKey(key)
		if got := docContent(w); got != "" {
			t.Errorf("%s put %q in the document", key, got)
		}
	}

	// Chords and ordinary keys are untouched — the test is the prefix, and
	// nothing else may match it.
	for _, key := range []string{"M-a", "S-Tab", "a", "^K", "Mode", "Modem"} {
		if isBareModifierKey(key) {
			t.Errorf("%s was mistaken for a bare modifier", key)
		}
	}
}

// It is dropped before the sequence processor, so a chord in progress survives
// the modifier being pressed part way through it.
func TestBareModifierDoesNotBreakASequence(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.KeyProcessor.MapKey("^K X", "insert 'chord'")

	e.dispatchKey("^K")
	e.dispatchKey("LMod:S") // Shift goes down mid-chord
	e.dispatchKey("X")

	if got := docContent(w); got != "chord" {
		t.Errorf("document = %q, want the chord to have completed", got)
	}
}

// A chord the host watched type NOTHING types nothing, and the table is not
// asked.
//
// A dead key is the case: on macOS, Option+i arms a circumflex for the next
// keystroke and produces no character of its own. The table says "ˆ", because
// a table can only say what such a chord types on the keyboard it was written
// from. Asked anyway, it inserted an accent this keyboard did not produce, and
// the composed character arrived behind it — Option+i, u came out "ˆû".
//
// Observed-and-empty is a real answer. Only never-observed falls to the table.
func TestAnObservedChordThatTypesNothingBeatsTheTable(t *testing.T) {
	e, _ := newTestEditor(t, "")
	e.Config.KeyChordText = func(chord string) (string, bool) {
		if chord == "M-i" {
			return "", true // watched; it typed nothing
		}
		return "", false
	}
	// The floor asks the PROCESSOR, which holds the precedence; the host's
	// lookup reaches it the way the editor installs it.
	e.KeyProcessor.SetKeyChordText(e.Config.KeyChordText)

	if got := e.defaultCommandForKey("M-i"); got != "" {
		t.Errorf("M-i defaulted to %q; the host watched it type nothing", got)
	}
}
