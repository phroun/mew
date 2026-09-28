//go:build mew

package trinkets

import "testing"

// The mew Editor's Undo and Redo enable from mew's own answer about the
// focused buffer (WithUndoState), and a read-only buffer greys both.
func TestMewEditorUndoFollowsMew(t *testing.T) {
	e := NewEditor()
	defer e.Close()

	if e.UndoEnabled() || e.RedoEnabled() {
		t.Error("before mew says anything, Undo and Redo should be off")
	}
	e.canUndo.Store(true)
	if !e.UndoEnabled() || e.RedoEnabled() {
		t.Errorf("mew can undo: Undo %v, Redo %v, want Undo only", e.UndoEnabled(), e.RedoEnabled())
	}
	e.canRedo.Store(true)
	if !e.RedoEnabled() {
		t.Error("mew can redo, and Redo is off")
	}
	e.readOnlyFocused.Store(true)
	if e.UndoEnabled() || e.RedoEnabled() {
		t.Error("a read-only focused buffer should grey Undo and Redo")
	}

	// With no running session (unbound port) both are safe no-ops.
	e.Undo()
	e.Redo()
}
