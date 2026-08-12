package core

import "testing"

// A letter's case is not part of which key it is, and a modifier prefix does
// not make it so. The toolkit's own keymap writes Cmd+M as "s-M"; a backend
// emits it as "s-m" (only Ctrl and Shift upper-case the letter), and the two
// have to meet.
func TestModifiedLetterKeymapIsCaseInsensitive(t *testing.T) {
	r := DefaultKeyRegistry()
	for _, key := range []string{"s-M", "s-m"} {
		ctx := r.BuildContext([]string{CmdAppMinimize})
		if got := ctx.Resolve(key); got != CmdAppMinimize {
			t.Errorf("%s -> %q, want %q", key, got, CmdAppMinimize)
		}
	}
}

// The same for the two spellings of Control, whose caret form writes its
// letter upper-case: every way of naming Ctrl+Q reaches the one binding.
func TestControlLetterSpellingsAllResolve(t *testing.T) {
	r := DefaultKeyRegistry()
	for _, key := range []string{"^Q", "^q", "C-Q", "C-q"} {
		ctx := r.BuildContext([]string{CmdAppQuit})
		if got := ctx.Resolve(key); got != CmdAppQuit {
			t.Errorf("%s -> %q, want %q", key, got, CmdAppQuit)
		}
	}
}
