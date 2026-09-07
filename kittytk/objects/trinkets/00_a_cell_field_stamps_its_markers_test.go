package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A cell target is handed strings and stamps them. So whatever the field
// works out about a line has to arrive there as characters in the ink: a
// marker the field decided on and then did not hand over is a marker nobody
// sees.
func TestACellFieldStampsItsMarkers(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const mixed = "abc שלום xyz"

	draw := func(show, focus bool) string {
		px, err := raster.New(600, 200)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		form := NewPanel()
		ti := NewTextInput()
		form.AddChild(ti)
		ti.SetText(mixed)
		ti.SetShowBidiControls(show)
		ti.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
		if focus {
			ti.SetFocus()
		}
		ti.Paint(core.NewPainter(ink))
		return strings.Join(ink.texts, "")
	}

	// Focused and asked for: the marks are in the ink.
	got := draw(true, true)
	for _, m := range []string{"<", ">", "|"} {
		if !strings.Contains(got, m) {
			t.Errorf("focused and asked, the field stamped %q, with no %q in it", got, m)
		}
	}

	// Asked for but not focused, and asked for nothing at all: the plain text.
	for _, c := range []struct {
		name        string
		show, focus bool
	}{
		{"unfocused", true, false},
		{"not asked", false, true},
	} {
		got := draw(c.show, c.focus)
		for _, m := range []string{"<", ">", "|"} {
			if strings.Contains(got, m) {
				t.Errorf("%s, the field stamped %q, which carries the mark %q",
					c.name, got, m)
			}
		}
	}
}

// noteInk records what was stamped and in which colours, so a test can say the
// notation is told apart from the text by the ink and not only by position.
type noteInk struct {
	core.RenderBackend
	texts  []string
	styles []style.CellStyle
}

func (n *noteInk) DrawText(x, y core.Unit, text string, s style.CellStyle, f *core.Font) core.Unit {
	n.texts = append(n.texts, text)
	n.styles = append(n.styles, s)
	return n.RenderBackend.DrawText(x, y, text, s, f)
}

// The marks and the stand-ins wear the input method's active colours, which is
// what says they are the field talking about the text rather than the text.
func TestTheFieldsNotationWearsItsOwnColour(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	px, err := raster.New(600, 200)
	if err != nil {
		t.Fatal(err)
	}
	ink := &noteInk{RenderBackend: px}
	form := NewPanel()
	ti := NewTextInput()
	form.AddChild(ti)
	ti.SetText("abc שלום\tx") // a turn, and a tab that cannot be drawn as itself
	ti.SetShowBidiControls(true)
	ti.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
	ti.SetFocus()
	ti.Paint(core.NewPainter(ink))

	want := ti.GetScheme().GetFocusedEditBoxIMEActiveClause()
	marked := map[string]bool{}
	for i, s := range ink.texts {
		if ink.styles[i].Fg == want.Fg && ink.styles[i].Bg == want.Bg {
			marked[s] = true
		}
	}
	for _, m := range []string{"<", ">", "|", "^I"} {
		if !marked[m] {
			t.Errorf("%q was not stamped in the field's notation colour (stamped: %v)",
				m, ink.texts)
		}
	}
	// And the text itself is not.
	for i, s := range ink.texts {
		if s == "^I" || len([]rune(s)) == 1 && strings.ContainsAny(s, "<>|") {
			continue
		}
		if ink.styles[i].Fg == want.Fg && ink.styles[i].Bg == want.Bg {
			t.Errorf("%q was stamped in the notation colour", s)
		}
	}
}
