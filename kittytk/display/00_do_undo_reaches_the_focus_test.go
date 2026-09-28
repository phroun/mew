package display

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
)

// do host undo and do host redo act on the focused trinket, as the clipboard
// actions do.
func TestDoUndoAndRedoReachTheFocus(t *testing.T) {
	ti := trinkets.NewTextInput()
	for _, k := range []string{"a", "b"} {
		ti.HandleKeyPress(core.KeyPressEvent{Key: k})
	}
	editAction(ti, DoUndo)
	if ti.Text() != "" {
		t.Errorf("undo left %q", ti.Text())
	}
	editAction(ti, DoRedo)
	if ti.Text() != "ab" {
		t.Errorf("redo left %q, want ab", ti.Text())
	}
}
