package display

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// The host's edit actions are the Edit menu's own acts, done on what the menu
// would do them on: do undo and do redo reach the focused field.
func TestDoUndoAndRedoReachTheFocus(t *testing.T) {
	d := trinkets.NewDesktop()
	d.SetBackend(quietBackend{})
	ti := trinkets.NewTextInput()
	win := window.NewWindow("host")
	win.SetContent(ti)
	d.WindowManager().AddWindow(win)
	d.WindowManager().ActivateWindow(win)
	ti.SetFocus()
	for _, k := range []string{"a", "b"} {
		ti.HandleKeyPress(core.KeyPressEvent{Key: k})
	}

	h := newHostObject(&conn{server: &Server{desktop: d}}, 0)
	if err := h.Do(DoUndo, nil); err != nil {
		t.Fatalf("do undo: %v", err)
	}
	if ti.Text() != "" {
		t.Errorf("do undo left %q", ti.Text())
	}
	if err := h.Do(DoRedo, nil); err != nil {
		t.Fatalf("do redo: %v", err)
	}
	if ti.Text() != "ab" {
		t.Errorf("do redo left %q, want ab", ti.Text())
	}
}
