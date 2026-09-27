package trinkets

// A placeholder is the field saying what it is for. Entered text is the
// reader's own.

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

const longPlaceholder = "Type the fingerprint of the host you mean to trust here"

// narrowField is a field too narrow for its placeholder, painted once.
func narrowField(t *testing.T) *TextInput {
	t.Helper()
	px, err := raster.New(900, 96)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	in := NewTextInput()
	in.SetPlaceholder(longPlaceholder)
	in.SetBounds(core.UnitRect{Width: 120, Height: 16})
	in.Paint(core.NewPainter(px))
	return in
}

// A placeholder does not scroll -- there is no caret in it to follow -- so
// arrows saying the run goes on would point somewhere the reader cannot get
// to. It is cut to the field instead, and offers the whole of itself.
func TestAPlaceholderIsCutRatherThanScrolled(t *testing.T) {
	in := narrowField(t)

	if in.moreLeft || in.moreRight {
		t.Error("the field is offering to scroll a placeholder nobody can move through")
	}
	text, at, ok := in.TooltipAt(core.UnitPoint{X: 4, Y: 4})
	if !ok {
		t.Fatal("a placeholder too long for its field offered nothing")
	}
	if text != longPlaceholder {
		t.Errorf("it offered %q", text)
	}
	if at.Width != in.Bounds().Width {
		t.Errorf("it named a rect %d wide, not the field's %d", at.Width, in.Bounds().Width)
	}
}

// A placeholder the field has room for is not missing anything.
func TestAPlaceholderThatFitsOffersNothing(t *testing.T) {
	px, err := raster.New(900, 96)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	in := NewTextInput()
	in.SetPlaceholder("Name")
	in.SetBounds(core.UnitRect{Width: 400, Height: 16})
	in.Paint(core.NewPainter(px))

	if _, _, ok := in.TooltipAt(core.UnitPoint{X: 4, Y: 4}); ok {
		t.Error("a placeholder with room to be read offered itself anyway")
	}
}

// Text the reader entered scrolls under the caret, and the field says so with
// its arrows. A note repeating it would answer a question nobody asked.
func TestEnteredTextIsNotOffered(t *testing.T) {
	in := narrowField(t)
	in.SetText("a value far longer than this narrow field can ever show at once")

	px, err := raster.New(900, 96)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	in.Paint(core.NewPainter(px))

	if text, _, ok := in.TooltipAt(core.UnitPoint{X: 4, Y: 4}); ok {
		t.Errorf("the field offered the reader their own text back: %q", text)
	}
}
