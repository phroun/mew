package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// iconMenu builds a one-item menu carrying the given icon, on the given
// backend, and paints it.
func iconMenu(t *testing.T, b core.RenderBackend, icon *style.TextIcon) *Menu {
	t.Helper()
	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 300, Height: 200})
	bar := NewMenuBar()
	d.AddChild(bar)
	m := NewMenu("File")
	item := NewMenuItem("Open Recent")
	item.SetIcon(icon)
	m.AddItem(item)
	m.AddItem(NewMenuItem("Close"))
	bar.AddMenu(m)
	m.inheritDisplayContext(bar.EffectiveCellMetrics(), bar.EffectiveFont())
	m.Show(0, 0)
	m.Paint(core.NewPainter(b))
	return m
}

// An icon gets the whole gutter, which is three cells across -- the frame,
// the mark, and the space before the label -- so a small text icon (3x1, the
// standard size) spans it exactly rather than showing only its first glyph.
func TestMenuGutterIconSpansTheGutter(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	rec := &cellRecorder{nullBackend: &nullBackend{}}

	icon := style.NewSmallTextIcon("<->", style.DefaultStyle())
	m := iconMenu(t, rec, &icon)
	mm := m.menuMetrics()

	for i, want := range []rune{'<', '-', '>'} {
		x := m.popupX + core.Unit(i)*mm.CellW
		found := false
		for _, c := range rec.cells {
			if c.ch == want && c.x == x {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the icon's glyph %q was not drawn at x=%d, cell %d of the gutter", want, x, i)
		}
	}
}

// A narrower icon centres on whole cells, which puts a single-cell one on the
// middle cell -- where a checkmark goes, and where an icon has always gone.
func TestMenuGutterIconCentresWhenNarrow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	rec := &cellRecorder{nullBackend: &nullBackend{}}

	icon := style.NewTextIcon(1, 1)
	icon.Set(0, 0, '*', style.DefaultStyle())
	m := iconMenu(t, rec, &icon)
	mm := m.menuMetrics()

	want := m.popupX + mm.CellW // the middle of three cells
	for _, c := range rec.cells {
		if c.ch != '*' {
			continue
		}
		if c.x != want {
			t.Errorf("a one-cell icon was drawn at x=%d, want the gutter's middle cell at %d", c.x, want)
		}
		return
	}
	t.Fatal("the icon was not drawn at all")
}

// A TRANSPARENT icon cell takes the gutter's ground; anything else the icon
// asked for is the icon's, a picture's background being its own to choose.
//
// On a cell surface the gutter's ground is its flat colour: a terminal cell
// holds one background attribute, so a transparent one is not the gutter
// showing through -- it is the terminal's own default, and the icon would sit
// in a hole in the gutter.
func TestMenuGutterIconGroundOnACellSurface(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// Transparent: the gutter's colour is what the cell gets.
	rec := &cellRecorder{nullBackend: &nullBackend{}}
	clear := style.NewSmallTextIcon("<->", style.DefaultStyle().WithBg(style.ColorTransparent))
	m := iconMenu(t, rec, &clear)

	want := m.GetScheme().GetMenuGutter().Bg
	found := 0
	for _, c := range rec.cells {
		if c.ch != '<' && c.ch != '-' && c.ch != '>' {
			continue
		}
		found++
		if c.st.Bg == style.ColorTransparent {
			t.Errorf("icon glyph %q sits on a transparent cell, which is the terminal's own background, not the gutter's", c.ch)
		}
		if c.st.Bg != want {
			t.Errorf("icon glyph %q has cell background %v, want the gutter's %v", c.ch, c.st.Bg, want)
		}
	}
	if found == 0 {
		t.Fatal("no icon glyph was drawn")
	}

	// A background the icon actually asked for is drawn as asked.
	rec = &cellRecorder{nullBackend: &nullBackend{}}
	red := style.RGB(255, 0, 0)
	painted := style.NewSmallTextIcon("<->", style.DefaultStyle().WithBg(red))
	iconMenu(t, rec, &painted)

	found = 0
	for _, c := range rec.cells {
		if c.ch != '<' && c.ch != '-' && c.ch != '>' {
			continue
		}
		found++
		if c.st.Bg != red {
			t.Errorf("icon glyph %q has cell background %v, want the icon's own %v", c.ch, c.st.Bg, red)
		}
	}
	if found == 0 {
		t.Fatal("no icon glyph was drawn")
	}
}

// The same on the graphical path, read off the paint: a transparent icon cell
// leaves the gutter's blend standing, and a cell that asked for a colour gets
// it.
//
// The blend is the gutter's colour laid over the menu beneath it, so a cell of
// flat background stamps it back out in a square around the glyph -- which is
// what the checkmark was fixed for and what an icon must not do uninvited.
func TestMenuGutterIconGroundOnAPixelSurface(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// The top pixel row of a menu row, which is above where any glyph inks,
	// so what is read there is the ground and nothing else.
	groundRow := func(icon style.TextIcon) (row []struct{ R, G, B uint8 }, blend struct{ R, G, B uint8 }) {
		b, err := raster.NewScaled(300, 200, 1)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(b)
		b.Clear(style.DefaultStyle().WithBg(style.RGB(255, 255, 255)))

		m := iconMenu(t, b, &icon)
		mm := m.menuMetrics()
		p := core.NewPainter(b)
		rowPx := p.UnitSpanPxY(0, mm.RowH)
		gutterPx := p.UnitSpanPxX(0, mm.GutterWidth())
		img := b.Image()

		// The gutter's blend, from the SECOND row, which carries no icon.
		c := img.RGBAAt(gutterPx/2, rowPx+rowPx/2)
		blend = struct{ R, G, B uint8 }{c.R, c.G, c.B}
		if blend.R == 255 && blend.G == 255 && blend.B == 255 {
			t.Fatal("precondition: the gutter should be a shade of its own, not the menu's white")
		}
		// Short of the gutter's last pixel column, which carries the divider
		// rule and is not ground.
		for x := 0; x < gutterPx-1; x++ {
			c := img.RGBAAt(x, 1)
			row = append(row, struct{ R, G, B uint8 }{c.R, c.G, c.B})
		}
		return row, blend
	}

	// Transparent: every pixel across the icon's gutter is the blend.
	row, blend := groundRow(style.NewSmallTextIcon("<->", style.DefaultStyle().WithBg(style.ColorTransparent)))
	for x, c := range row {
		if c != blend {
			t.Fatalf("a transparent icon stamped %v over the gutter's blend %v at column %d", c, blend, x)
		}
	}

	// A colour the icon asked for: painted, and covering the blend.
	row, blend = groundRow(style.NewSmallTextIcon("<->", style.DefaultStyle().WithBg(style.RGB(255, 0, 0))))
	painted := 0
	for _, c := range row {
		if c.R > 200 && c.G < 80 && c.B < 80 {
			painted++
		}
	}
	if painted != len(row) {
		t.Errorf("the icon asked for a red ground and got it on %d of the gutter's %d columns (the blend is %v)",
			painted, len(row), blend)
	}
}
