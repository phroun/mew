package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A trinket asks for the platform caret from its own paint, where all it knows
// is its own state. So one that goes on believing itself focused -- a terminal
// told by its host to behave as focused, and never told otherwise -- asks for
// the caret in a frame where focus has moved to a button.
//
// Focus overrules the request. Whether there is an insertion point on this
// surface at all is a question about focus, and it has already been asked: if
// what holds focus does not type, there is nothing for anything to have asked
// for, and a caret left standing points at where typing used to go.
func TestACaretGoesOutWithTheFocus(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	surf := &msSurface{size: core.UnitSize{Width: 800, Height: 240}}
	d.surface = surf
	h := &desktopSurfaceHandler{d: d}

	win := window.NewWindow("W")
	panel := NewPanel()
	asker := newCaretAsker(5) // asks every frame, focused or not
	button := NewButton("Go")
	panel.AddChild(asker)
	panel.AddChild(button)
	win.SetContent(panel)
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 800, Height: 240})
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
	win.Layout()

	// The asker holds focus and types: its caret is placed.
	win.FocusManager().SetFocusedTrinket(asker)
	h.Frame(core.NewPainter(px))
	if !surf.caretVisible {
		t.Fatal("the focused trinket's caret request did not reach the surface")
	}

	// Focus moves to a button, which does not type. The asker still asks --
	// and the caret goes out with the focus.
	win.FocusManager().SetFocusedTrinket(button)
	h.Frame(core.NewPainter(px))
	if surf.caretVisible {
		t.Errorf("focus moved to a button and the caret stayed at (%v,%v)",
			surf.caretX, surf.caretY)
	}

	// Back again, and it returns.
	win.FocusManager().SetFocusedTrinket(asker)
	h.Frame(core.NewPainter(px))
	if !surf.caretVisible {
		t.Error("the caret did not come back with the focus")
	}
}
