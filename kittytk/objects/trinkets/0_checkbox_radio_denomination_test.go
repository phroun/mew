package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

const boxCaption = "Enable the experimental feature that reticulates splines"

var boxOuter = core.CellMetrics{CellWidth: 8, CellHeight: 16}

var boxDenominations = []core.CellMetrics{
	{CellWidth: 8, CellHeight: 16},
	{CellWidth: 16, CellHeight: 32},
	{CellWidth: 32, CellHeight: 64},
	{CellWidth: 4, CellHeight: 8},
}

// captionRow lays a checkbox and a radio button side by side in a horizontal
// box, which is the layout that hands a child the width its SizeHint asks for.
// The panel's own bounds stay in the default denomination; the panel converts
// into the interior one, so a press is given in the same units at every
// denomination.
func captionRow(interior core.CellMetrics) (*Panel, *Checkbox, *RadioButton) {
	p := NewPanel()
	p.SetLayoutManager(layout.NewBoxLayout(core.Horizontal))
	c := NewCheckbox(boxCaption)
	r := NewRadioButton(boxCaption)
	p.AddChild(c)
	p.AddChild(r)
	p.SetCellMetrics(&interior)
	p.SetBounds(core.UnitRect{Width: 1200, Height: 16})
	p.Layout()
	return p, c, r
}

// A checkbox asks for the width of its indicator plus its caption. The
// indicator is three cells and a space, so it followed the denomination; the
// caption went through Font.MeasureText and came back in default units. At an
// interior denomination of 16 the caption counted for half what the layout
// spent on it, and the trinket was laid out narrower than the words it paints.
//
// The width is stated here in the default denomination so the three renders
// are comparable: the same caption fills the same physical width whatever the
// trinket counts in.
func TestCaptionTrinketsAskForTheSameWidthAtEveryDenomination(t *testing.T) {
	var baseC, baseR core.Unit
	for _, interior := range boxDenominations {
		_, c, r := captionRow(interior)
		gotC := core.ExchangeX(c.SizeHint().Width, interior, boxOuter)
		gotR := core.ExchangeX(r.SizeHint().Width, interior, boxOuter)

		if baseC == 0 {
			baseC, baseR = gotC, gotR
			continue
		}
		if gotC != baseC {
			t.Errorf("checkbox at %dx%d asks for %d units at 8x16, want %d",
				interior.CellWidth, interior.CellHeight, gotC, baseC)
		}
		if gotR != baseR {
			t.Errorf("radio button at %dx%d asks for %d units at 8x16, want %d",
				interior.CellWidth, interior.CellHeight, gotR, baseR)
		}
	}
}

// Neither trinket measures anything when a press arrives -- a press anywhere
// inside toggles -- so the clickable area is exactly the width the layout gave
// it, and a caption laid out narrower than it paints leaves its own right-hand
// words dead. This presses three quarters of the way across each trinket,
// which is inside the caption and past the point a half-counted one reaches.
func TestCaptionTrinketsTakeAPressOnTheirRightHalf(t *testing.T) {
	// Where to press, read off the default-denomination layout and then used
	// unchanged at every denomination: the same physical spot each time.
	_, c0, r0 := captionRow(boxOuter)
	pressC := c0.Bounds().X + c0.Bounds().Width*3/4
	pressR := r0.Bounds().X + r0.Bounds().Width*3/4

	for _, interior := range boxDenominations {
		p, c, r := captionRow(interior)

		press := func(x core.Unit) {
			p.HandleMousePress(core.MousePressEvent{
				X: x, Y: 8, Button: core.LeftButton,
			})
		}

		press(pressC)
		if !c.IsChecked() {
			t.Errorf("interior %dx%d: a press at %d missed the checkbox, which spans %d..%d",
				interior.CellWidth, interior.CellHeight, pressC,
				core.ExchangeX(c.Bounds().X, interior, boxOuter),
				core.ExchangeX(c.Bounds().X+c.Bounds().Width, interior, boxOuter))
		}

		press(pressR)
		if !r.IsChecked() {
			t.Errorf("interior %dx%d: a press at %d missed the radio button, which spans %d..%d",
				interior.CellWidth, interior.CellHeight, pressR,
				core.ExchangeX(r.Bounds().X, interior, boxOuter),
				core.ExchangeX(r.Bounds().X+r.Bounds().Width, interior, boxOuter))
		}
	}
}
