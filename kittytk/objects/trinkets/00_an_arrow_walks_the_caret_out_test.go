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

// A drag past the edge walks the TEXT, and which way through it the pointer
// reaches is settled at the press.
//
// In a right-to-left run the text further left is the text further ON, so the
// two directions are opposites there. Settling it once is what keeps a drag
// from giving back what it has already taken: the range only ever grows, even
// as the caret crosses into a run that reads the other way.
func TestADragKeepsTheDirectionItStartedIn(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	// Long enough to scroll, and reading right to left throughout.
	hebrew := strings.Repeat("שלום", 20)
	ti := scrolledField(t, hebrew, 40)
	roomLo, roomHi := ti.room()

	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: (roomLo + roomHi) / 2})
	if !ti.dragTurned {
		t.Fatal("a drag begun in a right-to-left run did not turn over")
	}
	at := ti.cursorPos
	ti.HandleMouseMove(core.MouseMoveEvent{X: roomLo - 1, Buttons: core.LeftButton})
	if ti.cursorPos <= at {
		t.Errorf("dragging LEFT in a right-to-left run took the caret to %d, want past "+
			"%d -- the text further left is the text further on", ti.cursorPos, at)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// The other way about, in English.
	english := strings.Repeat("abcdefghij", 8)
	ti = scrolledField(t, english, 40)
	roomLo, roomHi = ti.room()
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: (roomLo + roomHi) / 2})
	if ti.dragTurned {
		t.Fatal("a drag begun in a left-to-right run turned over")
	}
	at = ti.cursorPos
	ti.HandleMouseMove(core.MouseMoveEvent{X: roomLo - 1, Buttons: core.LeftButton})
	if ti.cursorPos >= at {
		t.Errorf("dragging LEFT in a left-to-right run took the caret to %d, want before %d",
			ti.cursorPos, at)
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	// Crossing into a run that reads the other way does not turn the drag over
	// mid-way: the range keeps growing rather than starting to shrink.
	//
	// Begun in the Hebrew and dragged RIGHT, which in a right-to-left run walks
	// BACK through the text -- out of the Hebrew and into the English before it,
	// where left and right mean the opposite things.
	mixed := strings.Repeat("abcde ", 6) + strings.Repeat("שלום ", 8)
	ti = scrolledField(t, mixed, 50)
	roomLo, roomHi = ti.room()
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: (roomLo + roomHi) / 2})
	if !ti.dragTurned {
		t.Fatal("the drag did not begin in the Hebrew")
	}
	turned := ti.dragTurned
	anchor := ti.selStart
	span := 0
	crossed := false
	for i := 0; i < 30; i++ {
		ti.HandleMouseMove(core.MouseMoveEvent{X: roomHi, Buttons: core.LeftButton})
		if ti.cursorPos < 36 {
			crossed = true // out of the Hebrew and into the English
		}
		if ti.dragTurned != turned {
			t.Fatalf("the drag turned over at step %d", i)
		}
		lo, hi := anchor, ti.cursorPos
		if lo > hi {
			lo, hi = hi, lo
		}
		if hi-lo < span {
			t.Fatalf("at step %d the chosen range shrank from %d to %d characters",
				i, span, hi-lo)
		}
		span = hi - lo
	}
	if span == 0 {
		t.Error("thirty steps of dragging chose nothing")
	}
	if !crossed {
		t.Error("the drag never reached the English, so it never crossed a turn")
	}
	ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
}

// A held arrow that reaches the end of the run STOPS there.
//
// The step that goes "as far as the field already shows" has to be the same
// answer twice running, or the hold has two places to alternate between and the
// caret twinkles for as long as the button is down. At the end of the run the
// caret stands inside the look-ahead margin -- there is nothing beyond it to
// look ahead at -- so a margin subtracted from the room refuses that position
// and sends the caret back, and the next step brings it forward again.
func TestAHeldArrowStopsAtTheEnd(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 8)
	n := len([]rune(long))

	for _, c := range []struct {
		name string
		dir  int
		want int
	}{
		{"the left end", -1, 0},
		{"the right end", 1, n},
	} {
		t.Run(c.name, func(t *testing.T) {
			ti := scrolledField(t, long, n/2)
			// Hold it until it arrives, then keep holding.
			for i := 0; i < 400 && ti.cursorPos != c.want; i++ {
				ti.arrowStep(c.dir, false, false)
			}
			if ti.cursorPos != c.want {
				t.Fatalf("holding never reached %d; stopped at %d", c.want, ti.cursorPos)
			}
			at, scroll := ti.cursorPos, ti.scroll
			for i := 0; i < 20; i++ {
				ti.arrowStep(c.dir, false, false)
				if ti.cursorPos != at {
					t.Fatalf("held at the end, step %d moved the caret from %d to %d",
						i, at, ti.cursorPos)
				}
				if ti.scroll != scroll {
					t.Fatalf("held at the end, step %d moved the view from %d to %d",
						i, scroll, ti.scroll)
				}
			}
		})
	}
}

// And a press at the end is the same: pressing an arrow that has nowhere left
// to go changes nothing rather than shuffling the caret about.
func TestAPressAtTheEndChangesNothing(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 8)
	ti := scrolledField(t, long, len([]rune(long)))
	if ti.moreRight {
		t.Fatal("a caret at the very end still hides text to its right")
	}
	at, scroll := ti.cursorPos, ti.scroll
	for i := 0; i < 5; i++ {
		ti.arrowStep(1, true, false)
	}
	if ti.cursorPos != at || ti.scroll != scroll {
		t.Errorf("pressing at the end moved the caret to %d and the view to %d, "+
			"want %d and %d", ti.cursorPos, ti.scroll, at, scroll)
	}
}

// A field is ONE line, so a drag leaving it upward or downward is leaving the
// text altogether -- there is no next line to reach for, and the only thing
// further that way is the rest of what is here. So the selection runs to that
// end of the content at once.
func TestADragOffTheTopOrBottomTakesEverythingThatWay(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 8)
	n := len([]rune(long))

	for _, c := range []struct {
		name string
		y    core.Unit
		want int
	}{
		{"above", -1, 0},
		{"below", 16, n},
	} {
		t.Run(c.name, func(t *testing.T) {
			ti := scrolledField(t, long, n/2)
			roomLo, roomHi := ti.room()
			ti.HandleMousePress(core.MousePressEvent{
				Button: core.LeftButton, X: (roomLo + roomHi) / 2})
			anchor := ti.selStart

			ti.HandleMouseMove(core.MouseMoveEvent{
				X: (roomLo + roomHi) / 2, Y: c.y, Buttons: core.LeftButton})
			if ti.cursorPos != c.want {
				t.Errorf("dragged %s, the caret is at %d, want %d", c.name, ti.cursorPos, c.want)
			}
			if ti.selStart != anchor || ti.selEnd != c.want {
				t.Errorf("dragged %s, the selection is [%d,%d), want [%d,%d)",
					c.name, ti.selStart, ti.selEnd, anchor, c.want)
			}

			// It is an answer, not a walk: nothing is left stepping afterwards.
			if ti.scrollDir != 0 {
				t.Errorf("dragged %s, an autoscroll is still running (%d)", c.name, ti.scrollDir)
			}

			// Out past a CORNER is the same answer: the end that way wins over
			// the sideways reach, which has nothing left to reach for.
			ti = scrolledField(t, long, n/2)
			ti.HandleMousePress(core.MousePressEvent{
				Button: core.LeftButton, X: (roomLo + roomHi) / 2})
			ti.HandleMouseMove(core.MouseMoveEvent{
				X: -20, Y: c.y, Buttons: core.LeftButton})
			if ti.cursorPos != c.want {
				t.Errorf("dragged out past the %s-left corner, the caret is at %d, want %d",
					c.name, ti.cursorPos, c.want)
			}
			if ti.scrollDir != 0 {
				t.Errorf("dragged out past the %s-left corner, an autoscroll is running", c.name)
			}

			// Coming back inside, the drag tracks the pointer again.
			ti.HandleMouseMove(core.MouseMoveEvent{
				X: (roomLo + roomHi) / 2, Y: 8, Buttons: core.LeftButton})
			if ti.cursorPos == c.want {
				t.Errorf("back inside the field, the caret stayed at the %s end", c.name)
			}
			ti.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
		})
	}
}
