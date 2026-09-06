package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/style"
)

// A focused scroll area shows it on its scrollbars.
//
// A scroll area is a container: it draws no text of its own, holds no
// selection, and paints no focus ring. Its bars are the whole of its chrome,
// so with them in the resting colour a keyboard user tabbing through a form
// has nothing at all to say where they are -- which is the gap the splitter's
// focused handle already closes for the splitter.
func TestAFocusedScrollAreaColoursItsScrollbars(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(200, 120)
	if err != nil {
		t.Fatal(err)
	}
	s := NewScrollArea()
	s.SetBounds(core.UnitRect{Width: 200, Height: 120})
	// Content taller and wider than the viewport, so the bars are drawn.
	content := NewPanel()
	content.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	for i := 0; i < 40; i++ {
		content.AddChild(NewLabel("a row long enough to overflow the viewport"))
	}
	s.SetContent(content)
	s.Layout()
	if !s.needsVScrollBar() {
		t.Fatal("the content did not overflow; there is no bar to look at")
	}

	// A point on the vertical bar's track, below the thumb.
	b := s.Bounds()
	at := core.UnitPoint{X: b.Width - 4, Y: b.Height - 12}
	rgb := func() (int, int, int) {
		s.Paint(core.NewPainter(px))
		r, g, bl, _ := px.Image().At(int(at.X), int(at.Y)).RGBA()
		return int(r >> 8), int(g >> 8), int(bl >> 8)
	}

	r0, g0, b0 := rgb()
	s.SetFocus()
	if !s.HasFocus() {
		t.Fatal("the scroll area did not take focus")
	}
	r1, g1, b1 := rgb()

	if r0 == r1 && g0 == g1 && b0 == b1 {
		t.Errorf("the bar is rgb(%d,%d,%d) either way; focus does not show", r0, g0, b0)
	}
}

// The thumb follows, and focus outranks hover the way the splitter's handle
// does: a pointer resting on the thumb of a focused area does not take the
// focus colour away.
func TestTheFocusedThumbOutranksHover(t *testing.T) {
	scheme := style.DefaultScheme()

	resting := scheme.GetScrollbarThumbState(false, false)
	hovered := scheme.GetScrollbarThumbState(false, true)
	focused := scheme.GetScrollbarThumbState(true, false)
	both := scheme.GetScrollbarThumbState(true, true)

	if hovered == resting {
		t.Error("hover does not change the thumb")
	}
	if focused == resting {
		t.Error("focus does not change the thumb")
	}
	if both != focused {
		t.Errorf("hovering a focused thumb gives %+v; want the focused %+v", both, focused)
	}
}

// A trinket that shows focus another way keeps its bars in the resting
// colour: a list or a tree says where the keyboard is with its selection, and
// a second signal on the bar beside it is noise.
func TestAListDoesNotColourItsScrollbarOnFocus(t *testing.T) {
	l := NewListView()
	l.SetBounds(core.UnitRect{Width: 160, Height: 48})
	for _, s := range []string{"one", "two", "three", "four", "five", "six"} {
		l.AddTextItem(s)
	}
	l.SetFocus()
	if !l.HasFocus() {
		t.Fatal("the list did not take focus")
	}

	scheme := l.GetScheme()
	if got := scheme.GetScrollbarThumbState(false, false); got != scheme.GetScrollbarThumb() {
		t.Errorf("the list's thumb is %+v; want the resting %+v", got, scheme.GetScrollbarThumb())
	}
}
