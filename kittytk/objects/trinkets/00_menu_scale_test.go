package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// [window] menu_scale sizes the menu bar, the dropdowns it opens and context
// menus the way titlebar_scale sizes a title bar: the row, the cell pitch the
// gutters and arrows count in, and the face, all at one fraction.
//
// The scale quantizes onto the denomination's integer unit grid, so what 0.9
// realizes depends on how finely the menu counts: 15/16 of a row at the
// default row_units, and nothing at all across an 8-unit column, where 0.9
// ceils back to 8. A menu counted more finely realizes it more exactly.

// menuScaleBar builds a desktop with a menu bar carrying one menu, at the
// given scale.
func menuScaleBar(t *testing.T, scale float64, graphical bool) (*Desktop, *MenuBar, *Menu) {
	t.Helper()
	b, err := raster.NewScaled(800, 600, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	core.SetMenuScale(scale)

	d := NewDesktop()
	if graphical {
		d.SetBackend(b)
	} else {
		// A real cell surface, not a flag: the bar asks its ancestry what it
		// is sitting on, so faking the cached value proves nothing.
		d.SetBackend(&nullBackend{})
	}
	d.SetBounds(core.UnitRect{Width: 800, Height: 600})

	bar := NewMenuBar()
	d.AddChild(bar)

	file := NewMenu("&File")
	file.AddItem(NewMenuItem("New"))
	file.AddSeparator()
	file.AddItem(NewMenuItem("Save As..."))
	bar.AddMenu(file)
	bar.SetBounds(core.UnitRect{Width: 800, Height: bar.menuMetrics().RowH})

	file.inheritDisplayContext(bar.EffectiveCellMetrics(), bar.EffectiveFont())
	file.setGraphicalHint(graphical)
	return d, bar, file
}

// At 1.0 every measurement is the one the unscaled code produced: the bar is
// a full cell row, the face is the caller's own, and the dropdown is three
// rows and a separator band.
func TestMenuScaleOneChangesNothing(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })
	d, bar, file := menuScaleBar(t, 1, true)

	cell := bar.EffectiveCellMetrics()
	mm := bar.menuMetrics()
	if mm.RowH != cell.UnitsPerCellHeight || mm.CellW != cell.UnitsPerCellWidth {
		t.Errorf("at 1.0 the row is %dx%d, want the cell's %dx%d",
			mm.CellW, mm.RowH, cell.UnitsPerCellWidth, cell.UnitsPerCellHeight)
	}
	if mm.Font != bar.EffectiveFont() {
		t.Error("at 1.0 the body face is a copy; it should be the caller's own pointer")
	}
	if got, want := d.MenuBarHeight(), cell.UnitsPerCellHeight; got != want {
		t.Errorf("desktop reserves %d for the bar, want the full cell %d", got, want)
	}
	if got, want := file.calculateSize().Height,
		cell.UnitsPerCellHeight*2+separatorBandUnits(cell.UnitsPerCellHeight); got != want {
		t.Errorf("dropdown height %d, want %d", got, want)
	}
}

// At 0.9 the row shortens, the face shrinks with it, and the separator band
// keeps its proportion of the row it now sits in.
func TestMenuScaleShortensTheRowAndTheFace(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })
	_, bar, file := menuScaleBar(t, 0.9, true)

	cell := bar.EffectiveCellMetrics()
	mm := bar.menuMetrics()
	if mm.RowH >= cell.UnitsPerCellHeight {
		t.Errorf("row %d did not shorten below the cell's %d", mm.RowH, cell.UnitsPerCellHeight)
	}
	if base := bar.EffectiveFont(); mm.Font.Size >= base.Size {
		t.Errorf("body face %dpt did not shrink below the base %dpt", mm.Font.Size, base.Size)
	}
	// The dropdown's rows are the bar's, and its separator a proportion of
	// those -- not of the cell the menu no longer fills.
	if got, want := file.calculateSize().Height,
		mm.RowH*2+separatorBandUnits(mm.RowH); got != want {
		t.Errorf("dropdown height %d, want two scaled rows plus a scaled band (%d)", got, want)
	}
}

// The desktop reserves exactly the row the bar paints and hit-tests with.
// Reserving a full cell for a shortened bar leaves a strip below it that
// looks like the bar and answers to nothing.
func TestMenuScaleReservationFollowsTheBar(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })
	d, bar, _ := menuScaleBar(t, 0.9, true)

	if got, want := d.MenuBarHeight(), bar.menuMetrics().RowH; got != want {
		t.Errorf("desktop reserves %d for a bar %d tall", got, want)
	}
	// And the bar answers for its whole row and no further.
	row := bar.menuMetrics().RowH
	if bar.menuItemAt(bar.leftInset()+1, row-1) < 0 {
		t.Error("the bar's last row unit does not reach its titles")
	}
	if bar.menuItemAt(bar.leftInset()+1, row) >= 0 {
		t.Error("the bar answers a unit past its own row")
	}
}

// A dropdown's hit rows are the rows it paints. The row height feeds both
// itemTopY (paint) and hitRow (press), so a scale applied to one and not the
// other puts every item a little above where it answers.
func TestMenuScaleHitRowsMatchThePaintedRows(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })
	_, _, file := menuScaleBar(t, 0.9, true)
	file.Show(0, 0)

	for i := range file.Items() {
		top := file.itemTopY(i)
		h := file.rowHeightAt(i, true, file.menuMetrics().RowH)
		if kind, idx := file.hitRow(top); kind != 3 || idx != i {
			t.Errorf("item %d is painted at Y=%d, where a press finds kind %d item %d", i, top, kind, idx)
		}
		if kind, idx := file.hitRow(top + h - 1); kind != 3 || idx != i {
			t.Errorf("item %d's last unit (Y=%d) finds kind %d item %d", i, top+h-1, kind, idx)
		}
	}
}

// A terminal cannot subdivide a character cell, so a cell surface stays at
// 1.0 whatever the host asked for -- the same ruling the title bar follows.
func TestMenuScaleStandsDownOnCellSurfaces(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })
	_, bar, file := menuScaleBar(t, 0.9, false)

	cell := bar.EffectiveCellMetrics()
	if mm := bar.menuMetrics(); mm.Scale != 1 || mm.RowH != cell.UnitsPerCellHeight {
		t.Errorf("cell surface bar at scale %v, row %d; want 1.0 and the full cell %d",
			mm.Scale, mm.RowH, cell.UnitsPerCellHeight)
	}
	if mm := file.menuMetrics(); mm.Scale != 1 {
		t.Errorf("cell surface dropdown at scale %v, want 1.0", mm.Scale)
	}
}

// A detached window carries its menu bar as chrome, and reserves the row the
// bar states rather than a whole cell.
//
// The reservation happens at LAYOUT, before any paint, so a bar that answered
// from paint state answered for a surface it had not seen yet and gave back a
// full cell -- which is the arrangement mew runs in when its app is solo.
func TestMenuScaleWindowChromeReservesTheBarsRow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })
	d, bar, _ := menuScaleBar(t, 0.9, true)

	win := window.NewWindow("detached")
	d.WindowManager().AddWindow(win)
	win.SetWindowMenuBar(bar)
	win.SetMenuBarVisible(true)
	win.SetDetached(true)
	win.SetBounds(core.UnitRect{Width: 400, Height: 200})

	// The chrome comes off the top of the content area, so what the window
	// gave the bar is the difference the bar makes to it.
	withBar := win.ContentBounds()
	win.SetMenuBarVisible(false)
	win.Layout()
	without := win.ContentBounds()

	got := withBar.Y - without.Y
	if want := bar.MenuRowHeight(); got != want {
		t.Errorf("window chrome reserves %d for a bar whose row is %d", got, want)
	}
	if cell := bar.EffectiveCellMetrics(); got == cell.UnitsPerCellHeight {
		t.Errorf("chrome reserved the whole cell (%d) for a shortened bar", got)
	}
}

// menuRowInk paints a two-item dropdown at the given scale and reports the
// row's height in device pixels and where the first row's text ink actually
// lands inside it. Reading the paint, not the formula: the offset that put
// the text wrong agreed with itself perfectly.
func menuRowInk(t *testing.T, scale float64) (rowPx, inkTop, inkBottom int) {
	t.Helper()
	b, err := raster.NewScaled(400, 300, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	core.SetMenuScale(scale)

	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 400, Height: 300})
	bar := NewMenuBar()
	d.AddChild(bar)
	m := NewMenu("File")
	// Ascenders and descenders both, so the ink spans the face's whole box.
	m.AddItem(NewMenuItem("Egpy"))
	m.AddItem(NewMenuItem("Egpy"))
	bar.AddMenu(m)
	m.inheritDisplayContext(bar.EffectiveCellMetrics(), bar.EffectiveFont())
	m.setGraphicalHint(true)
	m.Show(0, 0)

	b.Clear(style.DefaultStyle())
	m.Paint(core.NewPainter(b))

	mm := m.menuMetrics()
	p := core.NewPainter(b)
	rowPx = p.UnitSpanPxY(0, mm.RowH)
	// The white CONTENT area only, clear of the gutter and its divider.
	x0 := p.UnitSpanPxX(0, mm.CellW*3) + 3
	x1 := p.UnitSpanPxX(0, m.calculateSize().Width) - 3

	img := b.Image()
	inkTop, inkBottom = -1, -1
	for y := 0; y < rowPx; y++ {
		for x := x0; x < x1; x++ {
			if c := img.RGBAAt(x, y); int(c.R)+int(c.G)+int(c.B) < 600 {
				if inkTop < 0 {
					inkTop = y
				}
				inkBottom = y
				break
			}
		}
	}
	if inkTop < 0 {
		t.Fatalf("scale %v: no text ink found in the first row", scale)
	}
	return rowPx, inkTop, inkBottom
}

// An item's text sits in its row the same way at every scale. The row and
// the face shrink together, so the space above the glyphs shrinks with them.
//
// The offset that centres the face in the row measured the face against the
// SCALED row, which applies the scale twice: it reported a box smaller than
// the glyphs are, read the difference as slack, and pushed the text down by
// it. At 0.5 that put the ink 2 device pixels into an 8-pixel row and cut
// the descenders off the bottom.
func TestMenuScaleKeepsTextWhereItSitsInTheRow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil); core.SetMenuScale(1) })

	fullRow, fullTop, fullBottom := menuRowInk(t, 1)
	for _, scale := range []float64{0.9, 0.5} {
		row, top, bottom := menuRowInk(t, scale)
		if row >= fullRow {
			t.Fatalf("scale %v: row %dpx did not shorten below %dpx", scale, row, fullRow)
		}
		// Where the ink starts, as a share of the row, is what must hold.
		want := fullTop * row / fullRow
		if d := top - want; d < -1 || d > 1 {
			t.Errorf("scale %v: text ink starts %dpx into a %dpx row; at 1.0 it starts %dpx into %dpx, so want about %dpx",
				scale, top, row, fullTop, fullRow, want)
		}
		// And it still reaches the baseline rather than being clipped short
		// of it -- the descenders are what the pushed-down text lost.
		if wantB := fullBottom * row / fullRow; bottom < wantB-1 {
			t.Errorf("scale %v: text ink ends at %dpx of a %dpx row, want about %dpx -- the bottom is clipped",
				scale, bottom, row, wantB)
		}
	}
}

// A context menu is a menu, so it takes the same scale.
//
// It is NOT a trinkets.Menu: PurfecTerm, the mew editor and TextInput share
// their own popup presentation (termMenuLayoutFrom), which measured straight
// off the cell and so kept full-size rows while the bar and its dropdowns
// shortened around it.
func TestMenuScaleReachesContextMenus(t *testing.T) {
	t.Cleanup(func() { core.SetMenuScale(1) })
	m := core.DefaultCellMetrics()
	font := core.DefaultFont()
	items := []termMenuItem{{label: "Copy"}, {separator: true}, {label: "Paste and match style"}}

	core.SetMenuScale(1)
	full := termMenuLayoutFrom(true, font, m, items)
	if full.font != nil || full.yOff != 0 {
		t.Errorf("at 1.0 a context menu draws in the painter's own face with no offset; got %v / %d",
			full.font, full.yOff)
	}

	core.SetMenuScale(0.5)
	half := termMenuLayoutFrom(true, font, m, items)
	if half.rowH >= full.rowH {
		t.Errorf("context menu row %d did not shorten below %d", half.rowH, full.rowH)
	}
	if half.width >= full.width {
		t.Errorf("context menu width %d did not shrink below %d", half.width, full.width)
	}
	if half.font == nil || half.font.Size >= font.Size {
		t.Errorf("context menu labels draw at %v, want a face smaller than %dpt", half.font, font.Size)
	}

	// A cell surface stands down, as everywhere else.
	if cell := termMenuLayoutFrom(false, font, m, items); cell.rowH != m.UnitsPerCellHeight {
		t.Errorf("cell-surface context menu row %d, want the full cell %d", cell.rowH, m.UnitsPerCellHeight)
	}
}
