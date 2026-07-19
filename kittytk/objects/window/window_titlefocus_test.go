package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A click below the title bar clears title-bar keyboard focus, so
// Tab traversal resumes from the clicked content control rather than
// the title-bar element that Shift+Tab had landed on.
func TestContentClickClearsTitleFocus(t *testing.T) {
	win := NewWindow("focustest")
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120})
	content := newClickTrinket()
	win.SetContent(content)
	win.Layout()

	// Tab landed on a title-bar control.
	win.SetTitleFocus(TitleFocusMaximize)
	if win.TitleFocus() != TitleFocusMaximize {
		t.Fatal("title focus not set")
	}

	// Click in the content area (well below the title row).
	win.HandleMousePress(core.MousePressEvent{X: 40, Y: 60, Button: core.LeftButton})

	if win.TitleFocus() != TitleFocusNone {
		t.Errorf("title focus still %v after content click, want None", win.TitleFocus())
	}
	if content.clicks == 0 {
		t.Error("content did not receive the click")
	}
}

// A title-bar click does NOT clear title focus (still title-bar
// territory).
func TestTitleBarClickKeepsTitleContext(t *testing.T) {
	win := NewWindow("focustest")
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120})
	win.SetContent(newClickTrinket())
	win.Layout()
	win.SetTitleFocus(TitleFocusMaximize)

	// Click on the title bar, away from the control buttons.
	win.HandleMousePress(core.MousePressEvent{X: 150, Y: 0, Button: core.LeftButton})
	if win.TitleFocus() != TitleFocusMaximize {
		t.Errorf("title focus cleared by a title-bar click: %v", win.TitleFocus())
	}
}
