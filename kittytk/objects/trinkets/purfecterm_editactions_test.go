package trinkets

import "testing"

// The Edit-menu interface the desktop dispatches through.
type editActions interface {
	Cut()
	Copy()
	Paste()
	SelectAll()
}

// PurfecTerm satisfies the edit-action interface (so the Edit menu
// reaches a focused terminal) but reports Cut disabled - terminal
// output can't be cut.
func TestPurfecTermEditActions(t *testing.T) {
	var _ editActions = (*PurfecTerm)(nil)

	term := NewPurfecTerm()
	if cq, ok := interface{}(term).(interface{ CutEnabled() bool }); !ok {
		t.Fatal("PurfecTerm does not report CutEnabled")
	} else if cq.CutEnabled() {
		t.Error("PurfecTerm reports Cut enabled; want disabled")
	}
	// Cut is a no-op and must not panic without a running terminal.
	term.Cut()
}

// TextInput satisfies the same interface and does not force Cut off
// (no CutEnabled method -> the menu leaves Cut enabled).
func TestTextInputEditActionsInterface(t *testing.T) {
	var _ editActions = (*TextInput)(nil)
	ti := NewTextInput()
	if _, ok := interface{}(ti).(interface{ CutEnabled() bool }); ok {
		t.Error("TextInput unexpectedly forces a CutEnabled state")
	}
}
