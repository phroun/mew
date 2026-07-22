//go:build mew

package trinkets

import "testing"

// The mew-backed Editor satisfies the desktop's Edit-menu standard
// (editActor) with mew semantics, OVERRIDING the embedded PurfecTerm's
// terminal semantics: Cut is enabled (a document block can be cut, unlike
// terminal output), and HasSelection defers to mew (always true here, so
// the desktop never second-guesses with its own "nothing selected" notice —
// mew reports "No block marked" itself).
func TestMewEditorEditActions(t *testing.T) {
	type editActions interface {
		Cut()
		Copy()
		Paste()
		SelectAll()
	}
	var _ editActions = (*Editor)(nil)

	e := NewEditor()
	defer e.Close()

	if !e.CutEnabled() {
		t.Error("mew Editor must report Cut enabled (PurfecTerm's false overridden)")
	}
	if !e.HasSelection() {
		t.Error("mew Editor must defer selection state to mew (always true)")
	}

	// With no running session (unbound port) every action is a safe no-op.
	e.Cut()
	e.Copy()
	e.Paste()
	e.SelectAll()
}

// The right-click menu mirrors the TextInput control's menu: the same
// items in the same order, each the matching Edit-menu action.
func TestMewEditorContextMenuItems(t *testing.T) {
	e := NewEditor()
	defer e.Close()

	items := e.mewContextMenuItems()
	want := []struct {
		label     string
		separator bool
	}{
		{"Cut", false},
		{"Copy", false},
		{"Paste", false},
		{"", true},
		{"Select All", false},
	}
	if len(items) != len(want) {
		t.Fatalf("menu has %d entries, want %d", len(items), len(want))
	}
	for i, w := range want {
		if items[i].separator != w.separator || (!w.separator && items[i].label != w.label) {
			t.Errorf("item %d = {%q sep=%v}, want {%q sep=%v}",
				i, items[i].label, items[i].separator, w.label, w.separator)
		}
		if !w.separator && items[i].action == nil {
			t.Errorf("item %d (%q) has no action", i, w.label)
		}
	}
}
