package core

import "testing"

// A terminal's scrollback keys reach the terminal and NOTHING else. That is
// the whole reason they have their own command family: the same six keys mean
// something different (or nothing) to every other trinket, and binding them to
// the trinket movement would have taken them away from lists and trees to give
// them to a terminal's history.
func TestTerminalScrollbackKeysAreTerminalOnly(t *testing.T) {
	terminal := []string{
		CmdTerminalScrollUp, CmdTerminalScrollDown,
		CmdTerminalScrollPagePrior, CmdTerminalScrollPageNext,
		CmdTerminalScrollBeg, CmdTerminalScrollEnd,
	}
	r := DefaultKeyRegistry()
	for key, want := range map[string]string{
		"S-Up":       CmdTerminalScrollUp,
		"S-Down":     CmdTerminalScrollDown,
		"S-PageUp":   CmdTerminalScrollPagePrior,
		"S-PageDown": CmdTerminalScrollPageNext,
		"S-Home":     CmdTerminalScrollBeg,
		"S-End":      CmdTerminalScrollEnd,
	} {
		ctx := r.BuildContext(terminal)
		if got := ctx.Resolve(key); got != want {
			t.Errorf("terminal: %s -> %q, want %q", key, got, want)
		}
	}

	// The same keys, asked by a list: unchanged from before the terminal
	// commands existed. S-Up/S-Down still size a focused title bar, S-Home
	// and the paging keys still mean nothing here.
	list := []string{
		CmdTrinketItemUp, CmdTrinketItemDown, CmdTrinketItemPrior, CmdTrinketItemNext,
		CmdTrinketScrollUp, CmdTrinketScrollDown,
		CmdTrinketPagePrior, CmdTrinketPageNext,
		CmdTrinketBeg, CmdTrinketEnd,
	}
	for _, key := range []string{"S-Up", "S-Down", "S-PageUp", "S-PageDown", "S-Home", "S-End"} {
		ctx := r.BuildContext(list)
		if got := ctx.Resolve(key); got != "" {
			t.Errorf("list: %s -> %q, want no match", key, got)
		}
	}
}
