package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/style"
)

// A focused scroll area shows it on its scrollbar THUMBS.
//
// A scroll area is a container: it draws no text of its own, holds no
// selection, and paints no focus ring. Its bars are the whole of its chrome,
// so with them in the resting colour a keyboard user tabbing through a form
// has nothing at all to say where they are -- which is the gap the splitter's
// focused handle already closes for the splitter.
//
// The thumb only. The track and the corner where two bars meet stay put: the
// thumb is the part that reads as the control.
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

	// A point ON the vertical bar, well clear of the corner square where the
	// two bars meet -- that corner is the AREA's own paint, and sampling it
	// says nothing about whether the bars themselves followed.
	s.Paint(core.NewPainter(px))
	// The bar paints at 0,0 through an offset painter, so where it lands is
	// the viewport's right edge, not its own bounds. Unscrolled, the thumb is
	// at the top of the track; the far end of the track is bare.
	viewport := s.viewportBounds()
	bar := s.vScrollBar.Bounds()
	x := viewport.Width + bar.Width/2
	thumb := core.UnitPoint{X: x, Y: 4}
	track := core.UnitPoint{X: x, Y: viewport.Height - 8}

	rgb := func(at core.UnitPoint) [3]int {
		s.Paint(core.NewPainter(px))
		r, g, bl, _ := px.Image().At(int(at.X), int(at.Y)).RGBA()
		return [3]int{int(r >> 8), int(g >> 8), int(bl >> 8)}
	}

	// How far apart two samples are on their furthest channel. A thumb has to
	// stand off its track by a wide margin to be a thumb at all; "it changed"
	// is not the property -- a thumb that goes black on a black track has
	// changed, and has vanished at the moment it is most wanted.
	apart := func(a, b [3]int) int {
		d := 0
		for i := 0; i < 3; i++ {
			if v := a[i] - b[i]; v > d {
				d = v
			} else if -v > d {
				d = -v
			}
		}
		return d
	}
	const visible = 60

	thumb0, track0 := rgb(thumb), rgb(track)
	if got := apart(thumb0, track0); got < visible {
		t.Fatalf("unfocused, the thumb %v and the track %v are %d apart; the samples are not on what they say",
			thumb0, track0, got)
	}

	s.SetFocus()
	if !s.HasFocus() {
		t.Fatal("the scroll area did not take focus")
	}
	thumb1, track1 := rgb(thumb), rgb(track)

	if thumb1 == thumb0 {
		t.Errorf("the thumb is %v either way; focus does not show", thumb0)
	}
	if got := apart(thumb1, track1); got < visible {
		t.Errorf("focused, the thumb %v is only %d from the track %v; it has gone invisible",
			thumb1, got, track1)
	}
	if track1 != track0 {
		t.Errorf("the track moved from %v to %v; only the thumb should", track0, track1)
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
	if scheme.GetFocusedScrollbarThumb() == scheme.GetScrollbarThumb() {
		t.Error("the focused thumb is the resting one; the list test proves nothing")
	}
}
