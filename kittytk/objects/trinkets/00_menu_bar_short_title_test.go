package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// shortTitleBar builds a desktop carrying a menu bar of one menu, on a pixel
// surface so the bar has its frame indent and its strokes.
func shortTitleBar(t *testing.T, title string) (*MenuBar, *raster.Backend) {
	return shortTitleBarAt(t, title, 1)
}

// shortTitleBarAt is shortTitleBar at a given device scale. A unit is a device
// pixel only at scale 1, and the pin is a width in units, so what it does at
// the other scales is the whole question.
func shortTitleBarAt(t *testing.T, title string, scale int) (*MenuBar, *raster.Backend) {
	t.Helper()
	b, err := raster.NewScaled(400*scale, 200*scale, scale)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 400, Height: 200})
	bar := NewMenuBar()
	d.AddChild(bar)
	menu := NewMenu(title)
	menu.AddItem(NewMenuItem("Something"))
	bar.AddMenu(menu)
	bar.SetBounds(core.UnitRect{Width: 400, Height: bar.menuMetrics().RowH})
	return bar, b
}

// A title of one glyph takes the width of the dropdown's gutter, and no title
// is narrower than that.
//
// Stated as the widths, since a caller reading menuTitleWidth is what the
// paint, the hit test and the dropdown's placement all do.
func TestShortMenuTitlesArePinnedToTheGutter(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, c := range []struct {
		title  string
		pinned bool
		why    string
	}{
		{"Ψ", true, "one glyph"},
		{"☰", true, "one glyph"},
		{"il", true, "narrower than the gutter"},
		{"File", false, "wider than the gutter on its own"},
		{"Format", false, "wider than the gutter on its own"},
	} {
		bar, _ := shortTitleBar(t, c.title)
		mm := bar.menuMetrics()
		want := mm.GutterWidth()
		got := bar.menuTitleWidth(c.title)

		if c.pinned {
			if got != want {
				t.Errorf("%q (%s): width %d, want the gutter's %d",
					c.title, c.why, got, want)
			}
			continue
		}
		if got != mm.CellW*2+mm.TextWidth(c.title) {
			t.Errorf("%q (%s): width %d, want the title and its two pads (%d)",
				c.title, c.why, got, mm.CellW*2+mm.TextWidth(c.title))
		}
		if got <= want {
			t.Errorf("%q was meant to be wider than the pin (%d) on its own, but is %d",
				c.title, want, got)
		}
	}
}

// The point of the pin, read off the paint: the line down the right of an open
// item and the rule down the right of its dropdown's gutter are one line, so
// the item's right edge runs on down through the menu.
//
// AT EVERY DEVICE SCALE, which is the half of this that a single scale cannot
// see. The pin is a width in UNITS and a unit is a device pixel only at scale
// 1; a pin of the gutter less a unit lined the two up perfectly at scale 1 and
// came up a pixel short at scale 2 and two pixels short at scale 3, since what
// it took off was a unit at each of them.
//
// Both columns are found by looking at what was drawn. Computing either from
// the widths would agree with any pinning rule, including one a unit out.
func TestPinnedItemEdgeMeetsTheGutterRule(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, scale := range []int{1, 2, 3} {
		bar, b := shortTitleBarAt(t, "\u03a8", scale)
		bar.OpenMenu(0)
		// A ground nothing in the chrome uses, so every pixel read below is
		// something the bar or the menu actually drew.
		b.Clear(style.DefaultStyle().WithBg(style.RGB(0, 255, 0)))
		p := core.NewPainter(b)
		bar.Paint(p)
		bar.PaintDropdown(p)

		mm := bar.menuMetrics()
		img := b.Image()
		rowPx := p.UnitSpanPxY(0, mm.RowH)
		popupPx := p.UnitSpanPxX(0, bar.activeMenu.popupX)
		gutterPx := p.UnitSpanPxX(0, mm.GutterWidth())

		// The item's right line: follow its fill from a pixel inside the left
		// edge and stop at the first column that is not fill, which is the
		// line drawn over the fill's last column. Never from the width, which
		// is half of what is under test. Read along the top of the row, which
		// is clear of the glyph.
		barY := 0
		fill := img.RGBAAt(popupPx+1, barY)
		line := -1
		for x := popupPx + 1; x < popupPx+gutterPx*2; x++ {
			c := img.RGBAAt(x, barY)
			if abs8(c.R, fill.R)+abs8(c.G, fill.G)+abs8(c.B, fill.B) > 24 {
				line = x
				break
			}
		}
		if line < 0 {
			t.Fatalf("scale %d: the open item's fill runs on with no line ending it", scale)
		}

		// The gutter rule: the darkest column inside the gutter, past the
		// dropdown's own left stroke and short of the label.
		rule, darkest := -1, 1<<30
		menuY := rowPx + rowPx/2
		for x := popupPx + 1; x < popupPx+gutterPx+2; x++ {
			c := img.RGBAAt(x, menuY)
			if v := int(c.R) + int(c.G) + int(c.B); v < darkest {
				rule, darkest = x, v
			}
		}
		if rule < 0 {
			t.Fatalf("scale %d: no gutter rule found in the dropdown's row", scale)
		}

		if line != rule {
			t.Errorf("scale %d: the open item's right line is at pixel column %d and the gutter rule at %d; they should be one line",
				scale, line, rule)
		}
	}
}

// A pinned item's title is centred in it. The width no longer comes from the
// title, so the cell of leading pad every other item draws with would sit the
// glyph off to one side of a box that was sized for the gutter, not for it.
func TestPinnedTitleIsCentredInItsItem(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	bar, b := shortTitleBar(t, "Ψ")
	b.Clear(style.DefaultStyle().WithBg(style.RGB(255, 255, 255)))
	p := core.NewPainter(b)
	bar.Paint(p)

	mm := bar.menuMetrics()
	itemX := bar.calculateMenuX(0)
	width := bar.menuTitleWidth("Ψ")
	left := p.UnitSpanPxX(0, itemX)
	right := p.UnitSpanPxX(0, itemX+width)

	// The item fills its own rect, so what counts as the title's ink is what
	// differs from that fill -- sampled at the item's top-left, above where
	// any glyph reaches.
	img := b.Image()
	fill := img.RGBAAt(left, 0)
	rowPx := p.UnitSpanPxY(0, mm.RowH)
	first, last := -1, -1
	for x := left; x < right; x++ {
		for y := 0; y < rowPx; y++ {
			c := img.RGBAAt(x, y)
			d := abs8(c.R, fill.R) + abs8(c.G, fill.G) + abs8(c.B, fill.B)
			if d > 24 {
				if first < 0 {
					first = x
				}
				last = x
				break
			}
		}
	}
	if first < 0 {
		t.Fatal("the pinned title left no ink inside its item")
	}

	// Centred within a pixel: the ink's own margins on either side match.
	if d := (first - left) - (right - 1 - last); d < -1 || d > 1 {
		t.Errorf("the title inks [%d,%d] in an item spanning [%d,%d): %d px of margin on the left against %d on the right",
			first, last, left, right, first-left, right-1-last)
	}
	// And it is inside the item, which is the other half of centring it.
	if first < left || last >= right {
		t.Errorf("the title inks [%d,%d], outside its item [%d,%d)", first, last, left, right)
	}
}

// A terminal has no strokes to line up and lays its widths in whole cells, so
// nothing is pinned there.
func TestShortMenuTitlesAreNotPinnedOnCellSurfaces(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	d.SetBounds(core.UnitRect{Width: 400, Height: 200})
	bar := NewMenuBar()
	d.AddChild(bar)
	bar.AddMenu(NewMenu("Ψ"))
	bar.SetBounds(core.UnitRect{Width: 400, Height: bar.menuMetrics().RowH})

	mm := bar.menuMetrics()
	if mm.Graphical {
		t.Fatal("precondition: this bar should be on a cell surface")
	}
	if got, want := bar.menuTitleWidth("Ψ"), mm.CellW*2+mm.TextWidth("Ψ"); got != want {
		t.Errorf("cell surface: one-glyph title width %d, want the title and its two pads (%d)", got, want)
	}
}

// abs8 is the distance between two colour components.
func abs8(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
