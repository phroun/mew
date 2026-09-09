package trinkets

// What a trinket draws when it is given less room than its text needs.
//
// The test asks the backend what was drawn rather than looking at pixels: the
// question is which STRING each trinket put on the surface, and that is the
// thing the elision decides.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// elideRecorder remembers every string drawn, through either of the two ways a
// trinket puts text on a surface.
type elideRecorder struct {
	core.RenderBackend
	texts []string
}

func (r *elideRecorder) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	r.texts = append(r.texts, text)
	return r.RenderBackend.DrawText(x, y, text, s, font)
}

func (r *elideRecorder) DrawTextAligned(bounds core.UnitRect, text string, hSide core.HSide, vAlign core.VAlign, s style.CellStyle, font *core.Font) {
	r.texts = append(r.texts, text)
	r.RenderBackend.DrawTextAligned(bounds, text, hSide, vAlign, s, font)
}

// painted draws w at the given width and returns every string it drew.
func painted(t *testing.T, w core.Trinket, width core.Unit) []string {
	t.Helper()
	px, err := raster.New(900, 96)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	rec := &elideRecorder{RenderBackend: px}
	w.SetBounds(core.UnitRect{Width: width, Height: 48})
	w.Paint(core.NewPainter(rec))
	return rec.texts
}

// drewCut reports the first drawn string holding the ellipsis.
func drewCut(texts []string) (string, bool) {
	for _, s := range texts {
		if strings.Contains(s, core.Ellipsis) {
			return s, true
		}
	}
	return "", false
}

const longCaption = "Automatically Approve Loopback Connections"

// Every trinket whose size is its text says so when it is given less: it draws
// a cut caption rather than running its last words through whatever stands
// beside it.
func TestTextTooLongForItsRoomIsCutShort(t *testing.T) {
	for _, c := range []struct {
		name string
		make func() core.Trinket
	}{
		{"label", func() core.Trinket { return NewLabel(longCaption) }},
		{"button", func() core.Trinket { return NewButton(longCaption) }},
		{"checkbox", func() core.Trinket { return NewCheckbox(longCaption) }},
		{"radiobutton", func() core.Trinket { return NewRadioButton(longCaption) }},
		{"combobox", func() core.Trinket {
			c := NewComboBox()
			c.AddItem(longCaption)
			c.SetCurrentIndex(0)
			return c
		}},
	} {
		texts := painted(t, c.make(), 160)
		cut, ok := drewCut(texts)
		if !ok {
			t.Errorf("%s drew %q with nothing to say the caption was cut", c.name, texts)
			continue
		}
		if strings.Contains(cut, "Connections") {
			t.Errorf("%s drew %q, which is the whole caption in a fraction of its "+
				"room", c.name, cut)
		}
	}
}

// Given room enough, nothing is cut: a caption that fits is drawn as written.
func TestTextThatFitsIsDrawnWhole(t *testing.T) {
	for _, c := range []struct {
		name string
		make func() core.Trinket
	}{
		{"label", func() core.Trinket { return NewLabel(longCaption) }},
		{"button", func() core.Trinket { return NewButton(longCaption) }},
		{"checkbox", func() core.Trinket { return NewCheckbox(longCaption) }},
		{"radiobutton", func() core.Trinket { return NewRadioButton(longCaption) }},
	} {
		texts := painted(t, c.make(), 880)
		if cut, ok := drewCut(texts); ok {
			t.Errorf("%s cut %q with room to spare", c.name, cut)
		}
		var drewWhole bool
		for _, s := range texts {
			if s == longCaption {
				drewWhole = true
			}
		}
		if !drewWhole {
			t.Errorf("%s drew %q, not the caption itself", c.name, texts)
		}
	}
}

// Where the cut goes is the trinket's to say. A fingerprint read from the
// middle keeps both ends, which is what tells two of them apart.
func TestTheCutGoesWhereTheTrinketSays(t *testing.T) {
	const fingerprint = "sha256:d223d09d37f7a426b58eff67d4bb3b7bd57blahblahblahtail"
	for _, c := range []struct {
		mode      core.ElideMode
		keepsHead bool
		keepsTail bool
	}{
		{core.ElideEnd, true, false},
		{core.ElideMiddle, true, true},
		{core.ElideStart, false, true},
	} {
		l := NewLabel(fingerprint)
		l.SetElideMode(c.mode)
		cut, ok := drewCut(painted(t, l, 200))
		if !ok {
			t.Errorf("%v drew nothing cut", c.mode)
			continue
		}
		if head := strings.HasPrefix(cut, "sha256:"); head != c.keepsHead {
			t.Errorf("%v gave %q; keeps the head = %v, want %v", c.mode, cut, head, c.keepsHead)
		}
		if tail := strings.HasSuffix(cut, "tail"); tail != c.keepsTail {
			t.Errorf("%v gave %q; keeps the tail = %v, want %v", c.mode, cut, tail, c.keepsTail)
		}
	}
}

// Off is off: the whole text is drawn and the surface clips what does not fit,
// which is what a trinket doing its own cutting asks for.
func TestElidingCanBeTurnedOff(t *testing.T) {
	l := NewLabel(longCaption)
	l.SetElideMode(core.ElideOff)
	texts := painted(t, l, 160)
	if cut, ok := drewCut(texts); ok {
		t.Errorf("a label told not to elide drew %q", cut)
	}
	var whole bool
	for _, s := range texts {
		if s == longCaption {
			whole = true
		}
	}
	if !whole {
		t.Errorf("the label drew %q rather than its whole text", texts)
	}
}

// The text itself is untouched: only what is drawn of it is cut, so anything
// reading the trinket -- a rename, an accessibility name, a tooltip -- still
// has the whole of it.
func TestCuttingDoesNotChangeTheText(t *testing.T) {
	l := NewLabel(longCaption)
	painted(t, l, 120)
	if l.Text() != longCaption {
		t.Errorf("the label's own text came back as %q", l.Text())
	}
}
