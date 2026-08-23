//go:build mew

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A mew editor takes the keyboard: mew has a keymap of its own, so the
// toolkit's table must not answer for keys mew means to handle. ^Q is mew's
// quit while the editor has the focus, not the host's.
func TestMewEditorCapturesTheKeyboard(t *testing.T) {
	ed := NewEditor()
	if got := core.FindKeyRegistry(ed); got.Name() != "captured" {
		t.Fatalf("registry = %q, want captured", got.Name())
	}

	win := window.NewWindow("Host")
	win.SetContent(ed)
	win.FocusManager().SetFocusedTrinket(ed)

	for _, key := range []string{"^Q", "^W", "M-F4", "Return", "^A"} {
		if cmd := win.KeyCommand(key); cmd != "" {
			t.Errorf("%s resolved to %q above the editor; it is mew's key", key, cmd)
		}
	}
}

// ...but not the way back OUT. The menu and help keys reach the host's own
// chrome, and would be unreachable from the keyboard otherwise.
func TestMewEditorKeepsTheMenuAndHelpKeys(t *testing.T) {
	ed := NewEditor()
	reg := core.FindKeyRegistry(ed)

	for cmd, want := range map[string]string{
		core.CmdAppMenu: core.DefaultKeyRegistry().KeyForCommand(core.CmdAppMenu),
		core.CmdAppHelp: core.DefaultKeyRegistry().KeyForCommand(core.CmdAppHelp),
	} {
		if got := reg.KeyForCommand(cmd); got != want {
			t.Errorf("%s advertises %q while mew has the keyboard, want the host's own %q", cmd, got, want)
		}
	}

	// Every spelling the host had, not just the advertised one: F2 opens the
	// menu as well as F10, and it has to keep doing so over mew.
	for _, key := range core.DefaultKeyRegistry().KeysFor(core.CmdAppMenu) {
		if cmd := reg.BuildContext([]string{core.CmdAppMenu}).Resolve(key); cmd != core.CmdAppMenu {
			t.Errorf("%s resolved to %q over mew, want %s", key, cmd, core.CmdAppMenu)
		}
	}
}

// The capture is about where the FOCUS is, not about the editor existing: a
// window whose editor is not focused resolves its own keys as usual.
func TestMewEditorOnlyCapturesWhileFocused(t *testing.T) {
	ed := NewEditor()
	win := window.NewWindow("Host")
	win.SetContent(ed)

	win.FocusManager().SetFocusedTrinket(ed)
	if cmd := win.KeyCommand("^W"); cmd != "" {
		t.Fatalf("^W resolved to %q while the editor held focus", cmd)
	}

	win.FocusManager().SetFocusedTrinket(nil)
	if cmd := win.KeyCommand("^W"); cmd != core.CmdWindowClose {
		t.Errorf("^W resolved to %q with the editor unfocused, want %s", cmd, core.CmdWindowClose)
	}
}
