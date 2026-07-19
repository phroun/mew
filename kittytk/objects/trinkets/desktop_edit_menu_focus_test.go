package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// Desktop.FocusedTrinket reaches through the active window to its
// focused trinket - the source the Edit menu commands use so they act
// on the same target as an edit box's context menu (crucial on cell
// surfaces, where the context menu doesn't exist).
func TestDesktopFocusedTrinketReachesActiveWindow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	d := NewDesktop()
	d.SetBackend(&nullBackend{})

	input := NewTextInput()
	input.SetText("hello world")
	win := window.NewWindow("host")
	win.SetContent(input)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120})
	win.Layout()
	d.WindowManager().ActivateWindow(win)
	input.SetFocus()

	if got := d.FocusedTrinket(); got != input.Self() && got != core.Trinket(input) {
		t.Fatalf("FocusedTrinket = %v, want the text input", got)
	}

	// The Edit-menu-style path: select all, then Copy through the
	// focused-trinket interface, must match the direct method.
	ea, ok := d.FocusedTrinket().(interface {
		Copy()
		SelectAll()
	})
	if !ok {
		t.Fatal("focused trinket does not expose the edit actions")
	}
	ea.SelectAll()
	if input.SelectedText() != "hello world" {
		t.Errorf("SelectAll via focused trinket selected %q", input.SelectedText())
	}
}
