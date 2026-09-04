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

// On a cell surface the icon's cells carry the gutter's background.
//
// A terminal cell holds ONE background attribute, so a transparent one is not
// the gutter showing through -- it is the terminal's own default cell, and
// the icon would sit in a hole in the gutter.
func TestMenuGutterIconTakesTheGutterGroundOnACellSurface(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	rec := &cellRecorder{nullBackend: &nullBackend{}}

	// An icon insisting on a background of its own: the gutter's is what it
	// gets, since a menu's gutter is not the icon's to repaint.
	icon := style.NewSmallTextIcon("<->", style.DefaultStyle().WithBg(style.RGB(255, 0, 0)))
	m := iconMenu(t, rec, &icon)

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
}

// On the graphical path the icon draws OVER the gutter rather than laying
// cells of its own.
//
// The gutter's colour there is blended over the menu beneath it, and a cell
// of flat background laid by the icon stamps that blend back out in a square.
// Read off the paint: the blend survives across the icon's row, and the
// icon's own background colour is nowhere in it.
func TestMenuGutterIconDoesNotStampOverTheBlend(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.NewScaled(300, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	b.Clear(style.DefaultStyle().WithBg(style.RGB(255, 255, 255)))

	icon := style.NewSmallTextIcon("<->", style.DefaultStyle().WithBg(style.RGB(255, 0, 0)))
	m := iconMenu(t, b, &icon)
	mm := m.menuMetrics()
	p := core.NewPainter(b)

	rowPx := p.UnitSpanPxY(0, mm.RowH)
	gutterPx := p.UnitSpanPxX(0, mm.GutterWidth())
	img := b.Image()

	// The gutter's blend, sampled from the SECOND row, which carries no icon.
	blend := img.RGBAAt(gutterPx/2, rowPx+rowPx/2)
	if blend.R == 255 && blend.G == 255 && blend.B == 255 {
		t.Fatal("precondition: the gutter should be a shade of its own, not the menu's white")
	}

	seenBlend := false
	for x := 1; x < gutterPx-1; x++ {
		for y := 1; y < rowPx-1; y++ {
			c := img.RGBAAt(x, y)
			if c.R > 200 && c.G < 80 && c.B < 80 {
				t.Fatalf("the icon stamped its own background over the gutter at (%d,%d)", x, y)
			}
			if c == blend {
				seenBlend = true
			}
		}
	}
	if !seenBlend {
		t.Error("the gutter's blend is nowhere in the icon's row; something painted over all of it")
	}
}
