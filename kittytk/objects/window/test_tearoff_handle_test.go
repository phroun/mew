package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The tear handle sits in the title focus order between the maximize
// button and the title, and only when the window is tearable.
func TestTearHandleFocusOrder(t *testing.T) {
	win := NewWindow("w")
	win.SetTearable(true)

	if got := win.nextTitleFocus(TitleFocusMaximize); got != TitleFocusTear {
		t.Errorf("after maximize = %v, want Tear", got)
	}
	if got := win.nextTitleFocus(TitleFocusTear); got != TitleFocusTitle {
		t.Errorf("after tear = %v, want Title", got)
	}
	if got := win.prevTitleFocus(TitleFocusTitle); got != TitleFocusTear {
		t.Errorf("before title = %v, want Tear", got)
	}

	// Not tearable: the handle is skipped.
	plain := NewWindow("p")
	if got := plain.nextTitleFocus(TitleFocusMaximize); got != TitleFocusTitle {
		t.Errorf("non-tearable after maximize = %v, want Title", got)
	}
}

// The handle occupies the button-width slot after [x][.][^]; clicks
// there hit-test to the tear button and activate the tear callback.
func TestTearHandleHitTestAndActivate(t *testing.T) {
	win := NewWindow("w")
	win.SetTearable(true)
	win.SetBounds(core.UnitRect{Width: 300, Height: 120})

	// The handle floats immediately left of the centered title; probe
	// the same slot buttonAtPosition/paintTearHandle compute.
	metrics := core.DefaultCellMetrics()
	buttonWidth := metrics.TextWidth(3)
	controlsRight := metrics.CellWidth + buttonWidth*3
	titleW := win.EffectiveFont().MeasureText("w")
	handleX := tearHandleSlotX(win.Bounds().Width, controlsRight, titleW, buttonWidth)
	if got := win.buttonAtPosition(handleX+buttonWidth/2, 4); got != TitleButtonTear {
		t.Errorf("hit-test at handle = %v, want TitleButtonTear", got)
	}
	if got := win.buttonAtPosition(64, 4); got == TitleButtonTear {
		t.Error("maximize slot hit-tested as tear")
	}

	// Keyboard activation on the focused handle fires the callback.
	torn := false
	win.SetOnTearRequest(func() { torn = true })
	win.SetTitleFocus(TitleFocusTear)
	win.handleTitleBarKey(core.KeyPressEvent{Key: "Enter"})
	if !torn {
		t.Error("Enter on the focused handle did not request tear")
	}

	// Detached state flips the reported glyph state.
	win.SetDetached(true)
	if !win.IsDetached() {
		t.Error("SetDetached not reflected")
	}
}

// The tear-off halo shows while the handle is grabbed or the tear
// button holds keyboard focus, and never for a non-tearable window.
func TestTearIndicatorActive(t *testing.T) {
	win := NewWindow("w")
	win.SetTearable(true)

	if win.TearIndicatorActive() {
		t.Error("indicator active with no press or focus")
	}
	win.SetTearHighlight(true)
	if !win.TearIndicatorActive() {
		t.Error("indicator not active while handle grabbed")
	}
	win.SetTearHighlight(false)
	if win.TearIndicatorActive() {
		t.Error("indicator stayed active after release")
	}
	win.SetTitleFocus(TitleFocusTear)
	if !win.TearIndicatorActive() {
		t.Error("indicator not active while tear button focused")
	}
	win.SetTitleFocus(TitleFocusNone)

	// A non-tearable window never shows the halo, even if forced.
	plain := NewWindow("p")
	plain.SetTearHighlight(true)
	plain.SetTitleFocus(TitleFocusTear)
	if plain.TearIndicatorActive() {
		t.Error("non-tearable window reported indicator active")
	}
}

// While the title is focused (showing < >), the tear handle is omitted, so
// it isn't hittable anywhere in the title bar; it returns on focus change.
func TestTearHandleHiddenWhileTitleFocused(t *testing.T) {
	w := NewWindow("Some Title")
	w.SetTearable(true)
	w.SetBounds(core.UnitRect{Width: 400, Height: 100})

	hittable := func() bool {
		for x := core.Unit(0); x < 400; x += 2 {
			if w.buttonAtPosition(x, 4) == TitleButtonTear {
				return true
			}
		}
		return false
	}

	w.SetTitleFocus(TitleFocusNone)
	if !hittable() {
		t.Fatal("tear handle should be hittable when the title is not focused")
	}

	w.SetTitleFocus(TitleFocusTitle)
	if hittable() {
		t.Error("tear handle should not be hittable while the title is focused")
	}
}
