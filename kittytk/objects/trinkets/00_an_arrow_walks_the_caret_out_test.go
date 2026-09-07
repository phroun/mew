package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// A field showing the middle of a long run, with an arrow at each end.
func scrolledField(t *testing.T, text string, at int) *TextInput {
	t.Helper()
	form := NewPanel()
	ti := NewTextInput()
	form.AddChild(ti)
	ti.SetText(text)
	ti.SetBounds(core.UnitRect{Width: 20 * 8, Height: 16})
	ti.SetFocus()
	ti.SetCursorPosition(at)
	ti.ensureCursorVisible()
	return ti
}

// The end-of-run arrows are chrome, not text: a press on one walks the caret
// toward that end rather than putting it where the pointer is.
//
// The first press goes as far that way as the field ALREADY shows -- the
// outermost position in view, and nothing scrolls. Pressed again from there it
// reaches half the room's width further on and the view comes with it, because
// the view is worked out from where the caret is.
func TestAnArrowWalksTheCaretOut(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 8)
	ti := scrolledField(t, long, 40)
	if !ti.moreLeft || !ti.moreRight {
		t.Fatalf("a caret in the middle of a long run hides text both ways; "+
			"left=%v right=%v", ti.moreLeft, ti.moreRight)
	}

	roomLo, roomHi := ti.room()
	before := ti.scroll
	caret := ti.cursorPos

	// The press lands on the left arrow.
	if got := ti.arrowAt(roomLo - 1); got != -1 {
		t.Fatalf("the cell left of the room is arrow %d, want the left one", got)
	}
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 0})
	if ti.cursorPos >= caret {
		t.Errorf("the first press left the caret at %d, want it further left than %d",
			ti.cursorPos, caret)
	}
	if ti.scroll != before {
		t.Errorf("the first press scrolled from %d to %d; it should reach only as far "+
			"as the field already shows", before, ti.scroll)
	}
	atEdge := ti.cursorPos
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// Pressed again from there, it reaches further and the view follows.
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 0})
	if ti.cursorPos >= atEdge {
		t.Errorf("the second press left the caret at %d, want it past %d", ti.cursorPos, atEdge)
	}
	if ti.scroll >= before {
		t.Errorf("the second press left the scroll at %d, want it left of %d", ti.scroll, before)
	}
	// Half the room, near enough -- the caret lands on a whole character, and
	// the look-ahead margin sits between it and the edge.
	if moved := before - ti.scroll; moved <= 0 || moved > roomHi-roomLo {
		t.Errorf("the second press moved the view %d, want between one character and "+
			"the room's %d", moved, roomHi-roomLo)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// And the right arrow is its mirror. Walking the caret to the LEFT edge
	// first is what leaves it room to cross without the view moving -- a caret
	// already at the edge it is being sent toward has nowhere to go but off,
	// which is the reaching case above.
	ti = scrolledField(t, long, 40)
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 0})
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
	before, caret = ti.scroll, ti.cursorPos

	if got := ti.arrowAt(ti.Bounds().Width - 1); got != 1 {
		t.Fatalf("the cell right of the room is arrow %d, want the right one", got)
	}
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: ti.Bounds().Width - 1})
	if ti.cursorPos <= caret {
		t.Errorf("the first press on the right arrow left the caret at %d, want past %d",
			ti.cursorPos, caret)
	}
	if ti.scroll != before {
		t.Errorf("the first press on the right arrow scrolled from %d to %d; it should "+
			"reach only as far as the field already shows", before, ti.scroll)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// From the edge it now sits on, the next press reaches and the view follows.
	atEdge = ti.cursorPos
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: ti.Bounds().Width - 1})
	if ti.cursorPos <= atEdge {
		t.Errorf("the second press on the right arrow left the caret at %d, want past %d",
			ti.cursorPos, atEdge)
	}
	if ti.scroll <= before {
		t.Errorf("the second press on the right arrow left the scroll at %d, want right "+
			"of %d", ti.scroll, before)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
}

// A press on an arrow is not a press in the text: it places no caret under the
// pointer, arms no drag, and does not count toward a multi-click run.
func TestAnArrowIsNotAClickInTheText(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 8)
	ti := scrolledField(t, long, 40)

	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 0})
	if ti.selecting {
		t.Error("a press on an arrow armed a drag selection")
	}
	if ti.HasSelection() {
		t.Error("a press on an arrow left a selection behind")
	}
	if ti.clickStreak != 0 {
		t.Errorf("a press on an arrow counted %d toward a multi-click run", ti.clickStreak)
	}
	// Two fast presses are two presses, not a word to select.
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 0})
	if ti.HasSelection() {
		t.Error("a fast second press on an arrow selected a word")
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// The pointer says so before the press: chrome wears the plain arrow, the
	// text wears the I-beam.
	roomLo, roomHi := ti.room()
	if got := ti.CursorShapeAt(0, 0); got != core.CursorDefault {
		t.Errorf("over the left arrow the pointer is %v, want the plain one", got)
	}
	if got := ti.CursorShapeAt(ti.Bounds().Width-1, 0); got != core.CursorDefault {
		t.Errorf("over the right arrow the pointer is %v, want the plain one", got)
	}
	if got := ti.CursorShapeAt((roomLo+roomHi)/2, 0); got != core.CursorText {
		t.Errorf("over the text the pointer is %v, want the I-beam", got)
	}
}

// A drag reaching an arrow is a drag reaching for text that is off the end, so
// it keeps coming. The arrows sit INSIDE the field and the run gives up their
// cells, so the pointer arriving at one is already past everything on screen --
// waiting for it to leave the whole trinket stalls the selection exactly where
// it is trying to grow.
func TestADragReachingAnArrowKeepsGoing(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 8)
	ti := scrolledField(t, long, 40)
	roomLo, roomHi := ti.room()
	if roomLo <= 0 || roomHi >= ti.Bounds().Width {
		t.Fatalf("the field shows no arrows to drag onto: room [%d,%d) of %d",
			roomLo, roomHi, ti.Bounds().Width)
	}

	// Arm a drag, then move onto the left arrow -- still inside the field.
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: (roomLo + roomHi) / 2})
	if !ti.selecting {
		t.Fatal("a press in the text did not arm a drag")
	}
	at := ti.cursorPos
	ti.HandleMouseMove(core.MouseMoveEvent{X: roomLo - 1, Buttons: core.LeftButton})
	if ti.scrollDir != -1 {
		t.Errorf("a drag onto the left arrow set autoscroll %d, want -1", ti.scrollDir)
	}
	if ti.cursorPos >= at {
		t.Errorf("a drag onto the left arrow left the caret at %d, want past %d",
			ti.cursorPos, at)
	}
	if !ti.HasSelection() {
		t.Error("a drag onto the left arrow stopped extending the selection")
	}

	// And the right one.
	ti.HandleMouseMove(core.MouseMoveEvent{X: roomHi, Buttons: core.LeftButton})
	if ti.scrollDir != 1 {
		t.Errorf("a drag onto the right arrow set autoscroll %d, want 1", ti.scrollDir)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
	if ti.scrollDir != 0 {
		t.Error("release did not stop the autoscroll")
	}
}

// The arrows walk the line, not the text. Toward the left is toward the left on
// a line that turns over too, where the character left of a Hebrew letter is
// the one AFTER it in the text -- so a step that way can raise the caret's
// index rather than lower it.
func TestAnArrowWalksTheLineNotTheText(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום" // drawn ם ו ל ש: the first letter is the rightmost
	form := NewPanel()
	ti := NewTextInput()
	form.AddChild(ti)
	ti.SetShowBidiControls(false)
	ti.SetText(shalom)
	ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
	g := ti.runGeometry([]rune(shalom), ti.EffectiveFont(), false, false, 0)
	blank := ti.blankWidth()

	// From the first letter -- the RIGHTMOST on screen -- a step left is a step
	// to the next letter along in the text.
	p, ok := g.nextVisual(0, blank, true)
	if !ok {
		t.Fatal("no position left of the first letter")
	}
	if p != 1 {
		t.Errorf("one step left of letter 0 is %d, want 1 -- the next letter in the "+
			"text is the next one leftward on the line", p)
	}
	lo0, _, _ := g.boxOf(0)
	lo1, _, _ := g.boxOf(1)
	if lo1 >= lo0 {
		t.Errorf("letter 1 sits at %d and letter 0 at %d; the step did not go left",
			lo1, lo0)
	}

	// And a step right from the last letter goes back toward the start.
	q, ok := g.nextVisual(3, blank, false)
	if !ok {
		t.Fatal("no position right of the last letter")
	}
	if q != 2 {
		t.Errorf("one step right of letter 3 is %d, want 2", q)
	}
}

// Shift on an arrow extends the selection instead of collapsing it: the anchor
// stays where it was and only the moving end follows, which is what shift does
// to every other way of moving the caret here.
func TestShiftOnAnArrowExtendsTheSelection(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 8)
	ti := scrolledField(t, long, 40)
	anchor := ti.cursorPos

	ti.HandleMousePress(core.MousePressEvent{
		Button: core.LeftButton, X: 0, Modifiers: core.ShiftModifier})
	if !ti.HasSelection() {
		t.Fatal("shift on the left arrow selected nothing")
	}
	if ti.selStart != anchor {
		t.Errorf("the anchor moved to %d, want the caret's old place at %d",
			ti.selStart, anchor)
	}
	if ti.selEnd != ti.cursorPos || ti.cursorPos >= anchor {
		t.Errorf("the moving end is %d with the caret at %d, want both left of %d",
			ti.selEnd, ti.cursorPos, anchor)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// A second shift press carries the same anchor further.
	reached := ti.cursorPos
	ti.HandleMousePress(core.MousePressEvent{
		Button: core.LeftButton, X: 0, Modifiers: core.ShiftModifier})
	if ti.selStart != anchor {
		t.Errorf("the second press moved the anchor to %d, want %d", ti.selStart, anchor)
	}
	if ti.cursorPos >= reached {
		t.Errorf("the second press left the caret at %d, want past %d", ti.cursorPos, reached)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// And without shift it collapses again.
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 0})
	if ti.HasSelection() {
		t.Error("a plain press on the arrow kept the selection")
	}
}
