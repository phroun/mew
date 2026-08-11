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
