package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

// A field, a bar and a combo box are one line of text tall and cannot be more.
//
// Put beside something three rows deep they sit in the row rather than
// stretching to it -- filled, they took the whole of it, so a one-line field
// beside a list came out three lines of edit box with the text at the top.
//
// Across is another matter: they take the width they are given, which is why
// only the vertical axis is fixed.
func TestOneLineTrinketsNeverGrowTaller(t *testing.T) {
	p := NewPanel()
	p.SetLayoutManager(layout.NewBoxLayout(core.Horizontal))

	field := NewTextInput()
	bar := NewProgressBar()
	combo := NewComboBox()
	tall := NewListView() // three rows of real height beside them
	for _, k := range []core.Trinket{field, bar, combo, tall} {
		p.AddChild(k)
	}

	m := core.FindEffectiveCellMetrics(p.Self())
	row := m.UnitsPerCellHeight
	p.SetBounds(core.UnitRect{Width: 800, Height: row * 3})

	for _, c := range []struct {
		name string
		w    core.Trinket
	}{{"text field", field}, {"progress bar", bar}, {"combo box", combo}} {
		b := c.w.Bounds()
		if b.Height != row {
			t.Errorf("in a three-row row the %s is %d tall, want one line (%d)",
				c.name, b.Height, row)
		}
		// Centred, so it lines up with a button's cap in the same row.
		if b.Y != row {
			t.Errorf("the %s starts at y=%d, want the middle row (%d)", c.name, b.Y, row)
		}
		if b.Width <= 0 {
			t.Errorf("the %s came out %d wide; only the height is fixed", c.name, b.Width)
		}
	}
	// The one with real height keeps it.
	if got := tall.Bounds().Height; got != row*3 {
		t.Errorf("the list is %d tall, want the whole row (%d)", got, row*3)
	}
}

// A VERTICAL progress bar is the same rule the other way round: it is the width
// that cannot grow, and the length runs down the page.
func TestAVerticalProgressBarFixesTheOtherAxis(t *testing.T) {
	bar := NewProgressBar()
	if got := bar.SizePolicy(); got.Vertical != core.SizeFixed {
		t.Errorf("a horizontal bar's vertical policy is %v, want fixed", got.Vertical)
	}

	bar.SetOrientation(core.Vertical)
	got := bar.SizePolicy()
	if got.Horizontal != core.SizeFixed {
		t.Errorf("a vertical bar's horizontal policy is %v, want fixed", got.Horizontal)
	}
	if got.Vertical == core.SizeFixed {
		t.Error("a vertical bar cannot grow along its own length")
	}
}
