package core

import "testing"

type focusProbe struct {
	TrinketBase
	focused bool
}

func (f *focusProbe) HandleKeyPress(KeyPressEvent) bool { return false }

// Walking the focus chain is a BINDING, not a constant. The manager used to
// match "Tab" and two spellings of its shifted form -- one of which nothing
// emits -- and read the Shift bit out of the event to tell them apart.
func TestFocusManagerWalksOnTheBoundCommands(t *testing.T) {
	r := DefaultKeyRegistry()
	ctx := r.BuildContext([]string{CmdFocusNext, CmdFocusPrior})
	if got := ctx.Resolve("Tab"); got != CmdFocusNext {
		t.Errorf("Tab -> %q, want %q", got, CmdFocusNext)
	}
	ctx = r.BuildContext([]string{CmdFocusNext, CmdFocusPrior})
	if got := ctx.Resolve("S-Tab"); got != CmdFocusPrior {
		t.Errorf("S-Tab -> %q, want %q", got, CmdFocusPrior)
	}
}

// Rebinding the keymap moves focus navigation with it. Nothing in the manager
// knows the word "Tab".
func TestFocusManagerFollowsARebind(t *testing.T) {
	r := DefaultKeyRegistry()
	before := r.KeysFor(CmdFocusNext)
	r.Bind("^N", CmdFocusNext)
	defer func() {
		r.Bind("^N", "")
		for _, k := range before {
			r.AddBinding(k, CmdFocusNext)
		}
	}()

	ctx := r.BuildContext([]string{CmdFocusNext, CmdFocusPrior})
	if got := ctx.Resolve("^N"); got != CmdFocusNext {
		t.Errorf("after rebinding, ^N -> %q, want %q", got, CmdFocusNext)
	}
}
