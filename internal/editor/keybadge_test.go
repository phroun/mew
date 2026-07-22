package editor

import (
	"testing"

	"github.com/phroun/mew/internal/keys"
)

// keysRefAction extracts and DECODES the action from a keys#... anchor: "."
// decodes to "|" and "," to "&", because a dokuwiki anchor cannot carry those
// literally (a fallback-chain command name contains "|").
func TestKeysRefAction(t *testing.T) {
	cases := []struct {
		target string
		want   string
		ok     bool
	}{
		{"keys#go_page_prior", "go_page_prior", true},
		{"keys#buffer_undo", "buffer_undo", true},
		{"keys#buffer_redo.buffer_undo", "buffer_redo|buffer_undo", true}, // . -> |
		{"keys#a,b", "a&b", true},                                         // , -> &
		{"keys# spaced ", "spaced", true},
		{"keys#", "", false},
		{"help:keys#x", "", false}, // must be the bare "keys" page
		{"go_page_prior", "", false},
	}
	for _, c := range cases {
		got, ok := keysRefAction(c.target)
		if got != c.want || ok != c.ok {
			t.Errorf("keysRefAction(%q) = (%q,%v), want (%q,%v)", c.target, got, ok, c.want, c.ok)
		}
	}
}

func TestKeyBindingDisplay(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keys.NewSequenceProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{
		"^/": "buffer_undo",
		"^_": "buffer_undo",
		"^P": "go_page_prior",
		"^Z": "buffer_redo|buffer_undo", // a fallback chain
	})

	cases := []struct {
		action, preferred, want string
	}{
		// Preferred alias is bound -> honored.
		{"buffer_undo", "^/", "^/"},
		// Preferred not bound to this action -> list the live bindings, sorted.
		{"buffer_undo", "^Q", "^/, ^_"},
		// Single binding, no preferred.
		{"go_page_prior", "", "^P"},

		{"buffer_redo", "", "buffer_redo"}, // primary alone is NOT an exact binding -> unbound fallback (action name)
		// The full decoded chain command matches ^Z exactly.
		{"buffer_redo|buffer_undo", "", "^Z"},
		// buffer_undo is ^Z's FALLBACK, not its purpose -> ^Z excluded.
		{"buffer_undo", "", "^/, ^_"},
		// Unbound action -> falls back to the documented alias.
		{"nonexistent", "^X", "^X"},
	}
	for _, c := range cases {
		if got := e.keyBindingDisplay(c.action, c.preferred); got != c.want {
			t.Errorf("keyBindingDisplay(%q,%q) = %q, want %q", c.action, c.preferred, got, c.want)
		}
	}
}
