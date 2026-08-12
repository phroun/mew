package core

import "testing"

// The home row's key and the keypad's are two PHYSICAL keys with two names --
// keyseq deliberately does not alias them -- and direct-key-handler, whose
// vocabulary the toolkit's key names are written in, calls the home row's
// "Return". Binding only "Enter" left the home-row key meaning nothing under
// any backend that names it correctly.
func TestHomeRowReturnActivates(t *testing.T) {
	r := DefaultKeyRegistry()
	ctx0 := r.BuildContext([]string{CmdTrinketActivate})
	if got := ctx0.Resolve("Return"); got != CmdTrinketActivate {
		t.Errorf("Return -> %q, want %q", got, CmdTrinketActivate)
	}
	// The keypad's key is a DIFFERENT physical key and is not aliased to it.
	// Nothing binds it by default; a keymap that wants it says so.
	ctxKP := r.BuildContext([]string{CmdTrinketActivate})
	if got := ctxKP.Resolve("Enter"); got != "" {
		t.Errorf("keypad Enter -> %q, want no match by default", got)
	}
	// ^M is the control spelling of the home-row key, and keyseq aliases it.
	ctx := r.BuildContext([]string{CmdTrinketActivate})
	if got := ctx.Resolve("^M"); got != CmdTrinketActivate {
		t.Errorf("^M -> %q, want %q", got, CmdTrinketActivate)
	}
}
