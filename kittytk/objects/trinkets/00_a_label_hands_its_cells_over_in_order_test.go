package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// cellInk records what a CELL target is handed: the strings, in the order they
// were drawn. It installs no text measurer, which is what makes it a cell
// target -- a pixel one measures glyphs and orders its own paragraphs.
type cellInk struct {
	core.RenderBackend
	texts []string
	cells []rune // the glyphs stamped one at a time, in the order they went down
}

func (c *cellInk) DrawCell(x, y core.Unit, ch rune, s style.CellStyle) {
	c.cells = append(c.cells, ch)
	c.RenderBackend.DrawCell(x, y, ch, s)
}

func (c *cellInk) DrawText(x, y core.Unit, text string, s style.CellStyle, f *core.Font) core.Unit {
	c.texts = append(c.texts, text)
	return c.RenderBackend.DrawText(x, y, text, s, f)
}

func (c *cellInk) DrawTextAligned(b core.UnitRect, text string, h core.HSide, v core.VAlign, s style.CellStyle, f *core.Font) {
	c.texts = append(c.texts, text)
	c.RenderBackend.DrawTextAligned(b, text, h, v, s, f)
}

// A cell target stamps what it is handed, one cell at a time, left to right --
// so a Hebrew caption has to arrive turned over. It is the trinket that knows
// which way its text reads, and the last thing it does before drawing is say
// so.
func TestALabelHandsItsCellsOverInOrder(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	draw := func(text string, dir core.Direction) []string {
		px, err := raster.New(600, 200)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		form := NewPanel()
		form.SetDirection(dir)
		l := NewLabel(text)
		form.AddChild(l)
		l.SetBounds(core.UnitRect{Width: 40 * 8, Height: 3 * 16}) // room for more than one line
		l.Paint(core.NewPainter(ink))
		return ink.texts
	}

	const shalom = "שלום"
	turned := string([]rune{'ם', 'ו', 'ל', 'ש'})

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		got := draw(shalom, dir)
		if len(got) != 1 {
			t.Fatalf("%v: the label drew %d runs, want one", dir, len(got))
		}
		if got[0] != turned {
			t.Errorf("%v: the label handed over %q, want %q -- the run reads the other way "+
				"from the cells it is stamped into", dir, got[0], turned)
		}
	}

	// English is handed over as it stands, in either kind of line.
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		if got := draw("Ready", dir); got[0] != "Ready" {
			t.Errorf("%v: the label handed over %q, want it untouched", dir, got[0])
		}
	}

	// Every line of a wrapped or multi-line caption gets the same treatment,
	// each one turned over on its own.
	got := draw(shalom+"\n"+shalom, core.DirRTL)
	if len(got) != 2 {
		t.Fatalf("the two-line label drew %d runs, want two", len(got))
	}
	if got[0] != turned || got[1] != turned {
		t.Errorf("the two-line label handed over %q, want both lines turned over",
			strings.Join(got, "|"))
	}
}

// The same for every trinket whose whole job is to show a caption. Each one
// prepares its own, because each one knows which way its own text reads.
func TestACaptionedTrinketHandsItsCellsOverInOrder(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום"
	turned := string([]rune{'ם', 'ו', 'ל', 'ש'})

	draw := func(name string, make func() core.Trinket) {
		t.Helper()
		for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
			px, err := raster.New(600, 200)
			if err != nil {
				t.Fatal(err)
			}
			ink := &cellInk{RenderBackend: px}
			form := NewPanel()
			form.SetDirection(dir)
			w := make()
			form.AddChild(w)
			w.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
			w.Paint(core.NewPainter(ink))

			found := false
			for _, s := range ink.texts {
				if s == turned {
					found = true
				}
			}
			if !found {
				t.Errorf("%s %v: handed over %q, want the turned-over caption %q",
					name, dir, ink.texts, turned)
			}
		}
	}

	draw("button", func() core.Trinket { return NewButton(shalom) })
	draw("checkbox", func() core.Trinket { return NewCheckbox(shalom) })
	draw("radio button", func() core.Trinket { return NewRadioButton(shalom) })
}

// The rest of the chrome that shows a caption: a combo box's value, a
// separator's and a splitter's inline title, a dock entry.
func TestTheRestOfTheChromeHandsItsCellsOverInOrder(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום"
	turned := string([]rune{'ם', 'ו', 'ל', 'ש'})

	draw := func(name string, make func() core.Trinket) {
		t.Helper()
		for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
			px, err := raster.New(900, 400)
			if err != nil {
				t.Fatal(err)
			}
			ink := &cellInk{RenderBackend: px}
			form := NewPanel()
			form.SetDirection(dir)
			w := make()
			form.AddChild(w)
			w.SetBounds(core.UnitRect{Width: 60 * 8, Height: 5 * 16})
			w.Paint(core.NewPainter(ink))

			found := strings.Contains(string(ink.cells), turned)
			for _, s := range ink.texts {
				if s == turned {
					found = true
				}
			}
			if !found {
				t.Errorf("%s %v: handed over %q / %q, want the turned-over caption %q",
					name, dir, ink.texts, string(ink.cells), turned)
			}
		}
	}

	// A separator spreads its title across the cells of its rule one at a
	// time, so the run has to be turned over BEFORE it is spread.
	draw("separator", func() core.Trinket { return NewHSeparator(shalom) })
	draw("message box", func() core.Trinket { return NewMessageBox("Title", shalom, ButtonOK) })
	draw("combo box", func() core.Trinket {
		c := NewComboBox()
		c.AddItem(shalom)
		c.SetCurrentIndex(0)
		return c
	})
}
