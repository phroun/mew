package core

import "testing"

// The plain and the shifted arrow must reach DIFFERENT commands for a tree.
// An editable grid spends its plain arrows walking the edit-target column, so
// the shifted ones carry the classic collapse-or-walk-out; before these names
// existed both spellings resolved to trinket_item_left and nothing but the
// Shift bit could tell them apart.
func TestTreeArrowsResolveDistinctly(t *testing.T) {
	// The movement subset a TreeView declares.
	declared := []string{
		CmdTrinketItemLeft, CmdTrinketItemRight,
		CmdTrinketItemPrior, CmdTrinketItemNext,
		CmdTrinketCollapseOrEnclosing, CmdTrinketExpandOrDescend,
		CmdTrinketCollapse, CmdTrinketExpand,
	}
	r := DefaultKeyRegistry()
	for key, want := range map[string]string{
		"Left":    CmdTrinketItemLeft,
		"Right":   CmdTrinketItemRight,
		"S-Left":  CmdTrinketCollapseOrEnclosing,
		"S-Right": CmdTrinketExpandOrDescend,
		"Minus":   CmdTrinketCollapse,
		"Plus":    CmdTrinketExpand,
	} {
		ctx := r.BuildContext(declared)
		if got := ctx.Resolve(key); got != want {
			t.Errorf("%s -> %q, want %q", key, got, want)
		}
	}
}

// A trinket that does not offer the tree movement is unaffected: the shifted
// arrow still means what it always did there. A text field extends its
// selection, and a splitter resizes.
func TestShiftArrowsUnchangedElsewhere(t *testing.T) {
	r := DefaultKeyRegistry()
	for _, c := range []struct {
		name     string
		declared []string
		key      string
		want     string
	}{
		{"text field", []string{CmdTrinketSelLeft, CmdTrinketItemLeft}, "S-Left", CmdTrinketSelLeft},
		{"splitter", []string{CmdWindowSizeFineLeft}, "S-Left", CmdWindowSizeFineLeft},
		{"tab strip", []string{CmdTrinketItemLeft}, "S-Left", CmdTrinketItemLeft},
	} {
		ctx := r.BuildContext(c.declared)
		if got := ctx.Resolve(c.key); got != c.want {
			t.Errorf("%s: %s -> %q, want %q", c.name, c.key, got, c.want)
		}
	}
}
