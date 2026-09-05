package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A button answers for its face and the shadow beside and beneath it, and for
// nothing else. Room a layout grants beyond that footprint -- a grid cell, a
// stretched row -- is inert.
//
// The hit region followed the button's intrinsic height at its centred offset
// but took the WHOLE of the width it was given, so a button drawn small in a
// large cell answered clicks from the far side of the cell, where it does not
// appear to be.
func TestAButtonAnswersOnlyWhereItIsDrawn(t *testing.T) {
	b := NewButton("ok")
	// Far more room than the button will use, on both axes.
	b.SetBounds(core.UnitRect{Width: 400, Height: 120})

	foot := b.SizeHint()
	if foot.Width >= 400 {
		t.Fatalf("test setup: the button wants %d of the 400 it was given", foot.Width)
	}

	press := func(x, y core.Unit) bool {
		b.pressed = false
		return b.HandleMousePress(core.MousePressEvent{X: x, Y: y, Button: core.LeftButton})
	}

	// On the face.
	if !press(foot.Width/2, b.vInset()) {
		t.Error("a press on the button's face was not taken")
	}
	// The last unit of the footprint, which is the shadow's column.
	if !press(foot.Width-1, b.vInset()) {
		t.Error("a press on the button's shadow column was not taken")
	}
	// One unit past it, and far beyond it: room the layout gave, not button.
	if press(foot.Width, b.vInset()) {
		t.Error("a press one unit past the button's shadow was taken")
	}
	if press(360, b.vInset()) {
		t.Error("a press at the far side of the allocation was taken")
	}

	// The same on the vertical axis, which already worked and must keep to it.
	if press(foot.Width/2, 0) && b.vInset() > 0 {
		t.Error("a press above the centred button was taken")
	}
	if press(foot.Width/2, 118) {
		t.Error("a press below the button was taken")
	}
}

// A button given less room than it wants answers across what it actually has,
// rather than reporting a region reaching outside its own bounds.
func TestASqueezedButtonAnswersWithinItsBounds(t *testing.T) {
	b := NewButton("a long caption")
	b.SetBounds(core.UnitRect{Width: 24, Height: 32})

	if b.SizeHint().Width <= 24 {
		t.Fatal("test setup: the button was not squeezed")
	}
	r := b.hitRect()
	if r.Width != 24 {
		t.Errorf("a squeezed button answers across %d, want its whole 24", r.Width)
	}
	if !b.HandleMousePress(core.MousePressEvent{X: 23, Y: 0, Button: core.LeftButton}) {
		t.Error("a press at the last unit of a squeezed button was not taken")
	}
}
