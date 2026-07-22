package editor

import (
	"testing"

	"github.com/phroun/mew/internal/keys"
)

func TestKeysRefAction(t *testing.T) {
	cases := []struct {
		target string
		want   string
		ok     bool
	}{
		{"keys#go_page_prior", "go_page_prior", true},
		{"keys#buffer_undo", "buffer_undo", true},
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

func TestKludgeKeyText(t *testing.T) {
	for in, want := range map[string]string{
		"^P":    "^P",
		"|":     ".",
		"&":     ",",
		"^|":    "^.",
		"a|b&c": "a.b,c",
	} {
		if got := kludgeKeyText(in); got != want {
			t.Errorf("kludgeKeyText(%q) = %q, want %q", in, got, want)
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
		"^Z": "buffer_redo|buffer_undo", // a fallback chain, NOT a match for buffer_undo
		"|":  "some_action",             // a literal-pipe key binding
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
		// Chain command (^Z) must NOT count as a buffer_undo binding.
		{"buffer_redo", "", "^Z"},
		// Unbound action -> falls back to the documented alias.
		{"nonexistent", "^X", "^X"},
		// Literal-pipe key binding is kludged in the display.
		{"some_action", "", "."},
	}
	for _, c := range cases {
		if got := e.keyBindingDisplay(c.action, c.preferred); got != c.want {
			t.Errorf("keyBindingDisplay(%q,%q) = %q, want %q", c.action, c.preferred, got, c.want)
		}
	}
}
