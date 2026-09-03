package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A menu's two rules -- the divider down the gutter's edge and the separator
// between groups of items -- ink at MenuSeparatorAlpha over the background
// they sit on, rather than at full strength. Drawn solid they read as lines
// ruled through the menu; blended they read as divisions in it.
func TestMenuRulesInkOverTheirBackground(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.NewScaled(300, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 300, Height: 200})
	bar := NewMenuBar()
	d.AddChild(bar)
	m := NewMenu("File")
	m.AddItem(NewMenuItem("Undo"))
	m.AddSeparator()
	m.AddItem(NewMenuItem("Redo"))
	bar.AddMenu(m)
	m.inheritDisplayContext(bar.EffectiveCellMetrics(), bar.EffectiveFont())
	m.setGraphicalHint(true)
	m.Show(0, 0)
	b.Clear(style.DefaultStyle())
	m.Paint(core.NewPainter(b))

	sch := m.GetScheme()
	hr, hg, hb := sch.GetMenuSeparator().Fg.RGBComponents()
	mm := m.menuMetrics()
	p := core.NewPainter(b)
	img := b.Image()

	// What a quarter of the rule's colour over a given background comes to.
	//
	// A QUARTER, written out rather than read from MenuSeparatorAlpha: taken
	// from the constant, the expectation moves with the code and the test
	// agrees with any strength at all, including full.
	const wantAlpha = 0.25
	blend := func(bg, ink uint8) int {
		return int(float64(bg)*(1-wantAlpha) + float64(ink)*wantAlpha + 0.5)
	}
	near := func(got uint8, want int) bool { d := int(got) - want; return d >= -2 && d <= 2 }

	// The gutter divider: the gutter's own last pixel column, on a row that
	// is not the focused one (whose fill spans the gutter instead).
	xDiv := p.UnitSpanPxX(0, mm.CellW*3) - 1
	y := p.UnitSpanPxY(0, mm.RowH) / 4
	// The gutter AS PAINTED, read from beside the divider: the gutter lays
	// its own colour over the menu background at MenuGutterAlpha, so what the
	// divider inks over is not the scheme's gutter colour.
	beside := img.RGBAAt(xDiv-3, y)
	gr, gg, gb := beside.R, beside.G, beside.B
	c := img.RGBAAt(xDiv, y)
	if c.R == hr && c.G == hg && c.B == hb {
		t.Errorf("the gutter divider is drawn solid (%d,%d,%d), not inked over the gutter", c.R, c.G, c.B)
	}
	if !near(c.R, blend(gr, hr)) || !near(c.G, blend(gg, hg)) || !near(c.B, blend(gb, hb)) {
		t.Errorf("gutter divider = %d,%d,%d; a quarter of %d,%d,%d over the gutter's %d,%d,%d is %d,%d,%d",
			c.R, c.G, c.B, hr, hg, hb, gr, gg, gb,
			blend(gr, hr), blend(gg, hg), blend(gb, hb))
	}

	// The separator: its hairline, in the middle of the content area of the
	// band between the two items.
	sepTop := m.itemTopY(1)
	bandPx := p.UnitSpanPxY(sepTop, sepTop+m.rowHeightAt(1, true, mm.RowH))
	sy := p.UnitSpanPxY(0, sepTop) + (bandPx-1)/2
	sx := p.UnitSpanPxX(0, mm.CellW*3) + 8
	cr, cg, cb := sch.GetMenuSeparator().Bg.RGBComponents()
	s := img.RGBAAt(sx, sy)
	if s.R == hr && s.G == hg && s.B == hb {
		t.Errorf("the separator rule is drawn solid (%d,%d,%d), not inked over the band", s.R, s.G, s.B)
	}
	if !near(s.R, blend(cr, hr)) || !near(s.G, blend(cg, hg)) || !near(s.B, blend(cb, hb)) {
		t.Errorf("separator = %d,%d,%d; a quarter of %d,%d,%d over the band's %d,%d,%d is %d,%d,%d",
			s.R, s.G, s.B, hr, hg, hb, cr, cg, cb,
			blend(cr, hr), blend(cg, hg), blend(cb, hb))
	}
}

// The gutter lays its own colour over the menu background at three quarters,
// so it reads as a shaded band of the menu rather than a panel butted against
// it -- graphical only, and never over a focused row, whose gutter carries
// the SELECTION's colour and is not a gutter colour to soften.
func TestMenuGutterInksOverTheMenuBackground(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.NewScaled(300, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 300, Height: 200})
	bar := NewMenuBar()
	d.AddChild(bar)
	m := NewMenu("File")
	m.AddItem(NewMenuItem("Undo"))
	m.AddItem(NewMenuItem("Redo"))
	bar.AddMenu(m)
	m.inheritDisplayContext(bar.EffectiveCellMetrics(), bar.EffectiveFont())
	m.setGraphicalHint(true)
	m.Show(0, 0)
	m.currentIndex = 1 // the second row is focused
	b.Clear(style.DefaultStyle())
	m.Paint(core.NewPainter(b))

	sch := m.GetScheme()
	mm := m.menuMetrics()
	p := core.NewPainter(b)
	img := b.Image()
	// Clear of the divider on the right and the checkmark cell's glyphs.
	x := p.UnitSpanPxX(0, mm.CellW*3) - 4
	rowPx := p.UnitSpanPxY(0, mm.RowH)

	// A THREE QUARTER lay, written out rather than read from
	// MenuGutterAlpha: taken from the constant the expectation moves with the
	// code and the test agrees with any strength at all.
	const wantAlpha = 0.75
	over := func(under, ink uint8) int {
		return int(float64(under)*(1-wantAlpha) + float64(ink)*wantAlpha + 0.5)
	}
	near := func(got uint8, want int) bool { d := int(got) - want; return d >= -2 && d <= 2 }

	gr, gg, gb := sch.GetMenuGutter().Bg.RGBComponents()
	mr, mg, mb := sch.GetMenuItemText().Bg.RGBComponents()
	c := img.RGBAAt(x, rowPx/4)
	if c.R == gr && c.G == gg && c.B == gb {
		t.Errorf("the gutter is laid solid (%d,%d,%d), not over the menu background", c.R, c.G, c.B)
	}
	if !near(c.R, over(mr, gr)) || !near(c.G, over(mg, gg)) || !near(c.B, over(mb, gb)) {
		t.Errorf("gutter = %d,%d,%d; three quarters of %d,%d,%d over the menu's %d,%d,%d is %d,%d,%d",
			c.R, c.G, c.B, gr, gg, gb, mr, mg, mb, over(mr, gr), over(mg, gg), over(mb, gb))
	}

	// The focused row's gutter is the selection, laid solid.
	fr, fg, fb := sch.GetFocusedMenuItemText().Bg.RGBComponents()
	f := img.RGBAAt(x, rowPx+rowPx/4)
	if f.R != fr || f.G != fg || f.B != fb {
		t.Errorf("the focused row's gutter is %d,%d,%d, want the selection's own %d,%d,%d",
			f.R, f.G, f.B, fr, fg, fb)
	}
}

// A checked item's tick draws OVER the gutter, rather than laying a cell of
// its own first.
//
// The gutter's background is already there, and on the graphical path it is
// the gutter colour blended over the menu -- so a cell of the flat gutter
// colour, which is what drawing a glyph with an opaque style lays, stamped
// that blend back out in a square around the tick.
func TestMenuCheckmarkDrawsOverTheGutter(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })

	for _, scale := range []float64{1, 0.9} {
		b, err := raster.NewScaled(300, 200, 1)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(b)
		core.SetMenuScale(scale)

		d := NewDesktop()
		d.SetBackend(b)
		d.SetBounds(core.UnitRect{Width: 300, Height: 200})
		bar := NewMenuBar()
		d.AddChild(bar)
		m := NewMenu("File")
		ticked := NewMenuItem("Word Wrap")
		ticked.SetCheckable(true)
		ticked.SetChecked(true)
		m.AddItem(ticked)
		m.AddItem(NewMenuItem("Plain"))
		bar.AddMenu(m)
		m.inheritDisplayContext(bar.EffectiveCellMetrics(), bar.EffectiveFont())
		m.setGraphicalHint(true)
		m.Show(0, 0)
		b.Clear(style.DefaultStyle())
		m.Paint(core.NewPainter(b))

		mm := m.menuMetrics()
		p := core.NewPainter(b)
		img := b.Image()

		// A corner of the tick's own cell, which the glyph does not ink, and
		// the same spot one row down where no tick is drawn at all.
		x := p.UnitSpanPxX(0, mm.CellW) + 1
		got := img.RGBAAt(x, 1)
		plain := img.RGBAAt(x, p.UnitSpanPxY(0, mm.RowH)+1)
		if got != plain {
			t.Errorf("scale %v: the gutter around the tick is %d,%d,%d where a plain row's is %d,%d,%d",
				scale, got.R, got.G, got.B, plain.R, plain.G, plain.B)
		}

		// And what it is NOT is the scheme's flat gutter colour, which is
		// what laying a cell would have put back.
		gr, gg, gb := m.GetScheme().GetMenuGutter().Bg.RGBComponents()
		if got.R == gr && got.G == gg && got.B == gb {
			t.Errorf("scale %v: the tick laid a cell of the flat gutter colour (%d,%d,%d)",
				scale, gr, gg, gb)
		}
	}
}
