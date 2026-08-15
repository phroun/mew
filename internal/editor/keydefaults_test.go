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
