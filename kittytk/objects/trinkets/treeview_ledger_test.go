package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Scheme ledger/header defaults: cyan on very dark gray (odd), silver
// on very dark teal (even), silver on dark yellow (header).
func TestSchemeLedgerHeaderDefaults(t *testing.T) {
	s := &style.Scheme{}
	if got := s.GetLedgerOdd(); got.Fg != style.ColorCyan || got.Bg != style.RGB(26, 26, 26) {
		t.Errorf("LedgerOdd default = %v/%v", got.Fg, got.Bg)
	}
	if got := s.GetLedgerEven(); got.Fg != style.ColorWhite || got.Bg != style.RGB(12, 44, 44) {
		t.Errorf("LedgerEven default = %v/%v", got.Fg, got.Bg)
	}
	if got := s.GetHeader(); got.Fg != style.ColorWhite || got.Bg != style.ColorYellow {
		t.Errorf("Header default = %v/%v", got.Fg, got.Bg)
	}
}

// ellipsizeText replaces a cut tail with an ellipsis, rune-safely, and
// leaves fitting text alone.
func TestEllipsizeText(t *testing.T) {
	b, _ := raster.New(320, 32)
	d := NewDesktop()
	d.SetBackend(b)
	font := d.EffectiveFont()

	if got := ellipsizeText(font, "short", 400); got != "short" {
		t.Errorf("fitting text changed: %q", got)
	}
	long := "a rather long caption"
	got := ellipsizeText(font, long, 60)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("graphical ellipsis missing: %q", got)
	}
	if font.MeasureText(got) > 60 {
		t.Errorf("ellipsized text still overflows: %q", got)
	}
	// Rune safety: multibyte text must not split mid-rune.
	got = ellipsizeText(font, "ααααααααααααααα", 40)
	for _, r := range got {
		if r == '�' {
			t.Errorf("mid-rune split: %q", got)
		}
	}
}

// Pixel surfaces reserve NO divider cell: spans run edge to edge, with
// the hairline on the boundary. The TUI keeps its one-cell divider.
func TestTreeColumnNoDividerCellOnPixels(t *testing.T) {
	b, _ := raster.New(640, 240)
	d := NewDesktop()
	d.SetBackend(b)
	tv := newColumnsTree(60, 10)
	tv.SetParent(d) // desktop backend => smooth positioning

	lay := tv.columnLayout()
	for i := 0; i+1 < len(lay.spans); i++ {
		if got := lay.spans[i+1].x - (lay.spans[i].x + lay.spans[i].w); got != 0 {
			t.Errorf("span %d..%d gap = %d units, want 0 on pixels", i, i+1, got)
		}
		if lay.spans[i].divX != lay.spans[i].x+lay.spans[i].w {
			t.Errorf("divider %d not on the boundary", i)
		}
	}

	// Same tree without a smooth ancestor: one divider cell between spans.
	tv2 := newColumnsTree(60, 10)
	lay2 := tv2.columnLayout()
	cw := tv2.EffectiveCellMetrics().CellWidth
	if got := lay2.spans[1].x - (lay2.spans[0].x + lay2.spans[0].w); got != cw {
		t.Errorf("TUI divider gap = %d, want one cell (%d)", got, cw)
	}
}

// The last pinned-left column's boundary hairline must survive the
// horizontal-scroll edge fade: the left fade starts exactly on that
// hairline, so it is repainted OVER the fade - otherwise scrolling
// erases it and the pinned column visually merges with the scrolled
// content.
func TestTreePinnedDividerAboveFade(t *testing.T) {
	b, _ := raster.New(640, 240)
	d := NewDesktop()
	d.SetBackend(b)
	tv := newColumnsTree(60, 10)
	tv.SetParent(d)
	tv.SetFitWidth(false)
	tv.SetKeyWidth(15)
	tv.SetFixedColumns(1, 0)
	tv.scrollHorizontally(4) // panned right: the left fade paints
	b.Clear(style.DefaultStyle())
	tv.Paint(core.NewPainter(b))

	lay := tv.columnLayout()
	if !lay.spans[0].fixed || lay.spans[0].divX != lay.scrollL {
		t.Fatalf("precondition: pinned key divider at scrollL (divX=%d scrollL=%d)",
			lay.spans[0].divX, lay.scrollL)
	}
	// Sample the divider pixel in a plain row band (row 2; row 0 holds
	// the selection): the fade there is the list background, and the
	// hairline must still stand out from it.
	bgR, bgG, bgB := tv.GetScheme().GetListBG().RGBComponents()
	c := b.Image().RGBAAt(int(lay.spans[0].divX), 16+2*16+8)
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}
	diff := abs(int(c.R)-int(bgR)) + abs(int(c.G)-int(bgG)) + abs(int(c.B)-int(bgB))
	if diff < 20 {
		t.Errorf("pinned divider vanished under the fade: pixel %d,%d,%d ~= list bg %d,%d,%d",
			c.R, c.G, c.B, bgR, bgG, bgB)
	}
}

// While the column chooser is popped down, the selected grid row shows
// the NON-focused selection color - otherwise the grid row and the
// menu's focused item would both read as "the focus" at once.
func TestTreeChooserDimsSelection(t *testing.T) {
	b, _ := raster.New(480, 160)
	d := NewDesktop()
	d.SetBackend(b)
	tv := NewTreeView()
	tv.SetParent(d)
	tv.SetShowHeader(true)
	tv.AddColumn(NewTreeColumn("size", "Size", 10))
	for _, name := range []string{"aaa", "bbb"} {
		tv.AddRootItem(NewTreeItem(name))
	}
	tv.SetCurrentIndex(0)
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	tv.SetFocus()
	// Focus lands on the header bar stop; Down moves the internal
	// zone into the content, where the selection carries the focus.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"})

	paintRowBG := func() (uint8, uint8, uint8) {
		b.Clear(style.DefaultStyle())
		tv.Paint(core.NewPainter(b))
		c := b.Image().RGBAAt(300, 16+8) // row 0, away from text
		return c.R, c.G, c.B
	}
	// Prime once so the desktop's theme is applied, THEN read the
	// scheme colors the row is expected to use.
	paintRowBG()
	scheme := tv.GetScheme()
	fR, fG, fB := scheme.GetFocusedListItem().Bg.RGBComponents()
	sR, sG, sB := scheme.GetSelectedListItem().Bg.RGBComponents()

	if r, g, bl := paintRowBG(); r != fR || g != fG || bl != fB {
		t.Fatalf("focused selection bg = %d,%d,%d want %d,%d,%d", r, g, bl, fR, fG, fB)
	}
	tv.chooserOpen = true
	if r, g, bl := paintRowBG(); r != sR || g != sG || bl != sB {
		t.Errorf("selection bg with chooser open = %d,%d,%d want non-focused %d,%d,%d",
			r, g, bl, sR, sG, sB)
	}
	tv.chooserOpen = false
	if r, g, bl := paintRowBG(); r != fR || g != fG || bl != fB {
		t.Errorf("selection bg after chooser close = %d,%d,%d want focused %d,%d,%d",
			r, g, bl, fR, fG, fB)
	}

	// While the cell editor is up, the row UNDER it wears the
	// FocusedListRow band - distinct from the editor floating over it
	// and from a plain resting selection. (Editable column added here
	// so Enter opens the editor.)
	tv.ColumnByID("size").Editable = true
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"})
	if !tv.rowEditing {
		t.Fatal("precondition: row editor open")
	}
	// Sample row 0 at x=150 (inside the key column, past its text and
	// outside the Size cell the editor covers).
	rowR, rowG, rowB := scheme.GetFocusedListRow().Bg.RGBComponents()
	b.Clear(style.DefaultStyle())
	tv.Paint(core.NewPainter(b))
	c := b.Image().RGBAAt(150, 16+8)
	if c.R != rowR || c.G != rowG || c.B != rowB {
		t.Errorf("row band while editing = %d,%d,%d want FocusedListRow %d,%d,%d",
			c.R, c.G, c.B, rowR, rowG, rowB)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})
	// Back out of edit mode the editable grid's focused row carries
	// the plain selection bar (the Enter-target Size cell alone would
	// wear FocusedListItem); x=150 sits in the key column - the band.
	if r, g, bl := paintRowBG(); r != sR || g != sG || bl != sB {
		t.Errorf("selection bg after editor close = %d,%d,%d want SelectedListItem %d,%d,%d",
			r, g, bl, sR, sG, sB)
	}
}

// With editing available, the focused selected row wears the
// SelectedListItem band while the Enter-target cell alone carries
// FocusedListItem; while the editor is up the row band switches to
// FocusedListRow; the header's internal focus stop wears the
// FocusedListButton face (it is a control, not a list item).
func TestTreeFocusedListRowAndTarget(t *testing.T) {
	b, _ := raster.New(480, 160)
	d := NewDesktop()
	d.SetBackend(b)
	tv := NewTreeView()
	tv.SetParent(d)
	tv.SetShowHeader(true)
	size := NewTreeColumn("size", "Size", 10)
	size.Editable = true
	tv.AddColumn(size)
	for _, name := range []string{"aaa", "bbb"} {
		it := NewTreeItem(name)
		it.SetValue("size", "1 KB")
		tv.AddRootItem(it)
	}
	tv.SetCurrentIndex(0)
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	tv.SetFocus()
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"}) // bar -> content

	paint := func() {
		b.Clear(style.DefaultStyle())
		tv.Paint(core.NewPainter(b))
	}
	paint() // prime the theme
	scheme := tv.GetScheme()
	selR, selG, selB := scheme.GetSelectedListItem().Bg.RGBComponents()
	rowR, rowG, rowB := scheme.GetFocusedListRow().Bg.RGBComponents()
	itR, itG, itB := scheme.GetFocusedListItem().Bg.RGBComponents()

	paint()
	lay := tv.columnLayout()
	var sizeSp colSpan
	for _, sp := range lay.spans {
		if sp.col == size {
			sizeSp = sp
		}
	}
	// Key column (not the target): the plain selection bar.
	c := b.Image().RGBAAt(150, 16+8)
	if c.R != selR || c.G != selG || c.B != selB {
		t.Errorf("row band = %d,%d,%d want SelectedListItem %d,%d,%d",
			c.R, c.G, c.B, selR, selG, selB)
	}
	// Inside the Size cell (the Enter target): FocusedListItem.
	c = b.Image().RGBAAt(int(sizeSp.x+sizeSp.w/2), 16+8)
	if c.R != itR || c.G != itG || c.B != itB {
		t.Errorf("Enter-target cell = %d,%d,%d want FocusedListItem %d,%d,%d",
			c.R, c.G, c.B, itR, itG, itB)
	}
	// While EDITING, the row under the editor wears FocusedListRow.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Enter"}) // edit Size
	paint()
	c = b.Image().RGBAAt(150, 16+8) // key column, outside the editor
	if c.R != rowR || c.G != rowG || c.B != rowB {
		t.Errorf("row band while editing = %d,%d,%d want FocusedListRow %d,%d,%d",
			c.R, c.G, c.B, rowR, rowG, rowB)
	}
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Escape"})

	// Header bar focus stop: the FocusedListButton face.
	tv.HandleKeyPress(core.KeyPressEvent{Key: "S-Tab"}) // content -> bar
	paint()
	btnR, btnG, btnB := scheme.GetFocusedListButton().Bg.RGBComponents()
	c = b.Image().RGBAAt(300, 8)
	if c.R != btnR || c.G != btnG || c.B != btnB {
		t.Errorf("header focus stop = %d,%d,%d want FocusedListButton %d,%d,%d",
			c.R, c.G, c.B, btnR, btnG, btnB)
	}
}

// On the tree-apparatus column, the Enter-target highlight covers only
// the caption's CLICKABLE zone (displayed text, two-cell minimum) -
// the space right of the text stays in the SelectedListItem band.
func TestTreeTargetZoneOnTreeColumn(t *testing.T) {
	b, _ := raster.New(480, 160)
	d := NewDesktop()
	d.SetBackend(b)
	tv := NewTreeView()
	tv.SetParent(d)
	tv.SetShowHeader(true)
	tv.SetEditable(true) // only the key column is editable: it IS the target
	tv.AddColumn(NewTreeColumn("size", "Size", 10))
	for _, name := range []string{"aaa", "bbb"} {
		tv.AddRootItem(NewTreeItem(name))
	}
	tv.SetCurrentIndex(0)
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	tv.SetFocus()
	tv.HandleKeyPress(core.KeyPressEvent{Key: "Down"}) // bar -> content

	b.Clear(style.DefaultStyle())
	tv.Paint(core.NewPainter(b)) // prime the theme
	scheme := tv.GetScheme()
	rowR, rowG, rowB := scheme.GetSelectedListItem().Bg.RGBComponents()
	itR, itG, itB := scheme.GetFocusedListItem().Bg.RGBComponents()

	b.Clear(style.DefaultStyle())
	tv.Paint(core.NewPainter(b))
	lay := tv.columnLayout()
	item := tv.CurrentItem()
	zx, zw := tv.treeCellEditZone(lay.spans[0], item)

	c := b.Image().RGBAAt(int(zx+zw/2), 16+8) // inside the zone
	if c.R != itR || c.G != itG || c.B != itB {
		t.Errorf("zone = %d,%d,%d want FocusedListItem %d,%d,%d", c.R, c.G, c.B, itR, itG, itB)
	}
	c = b.Image().RGBAAt(int(zx+zw+16), 16+8) // right of the zone
	if c.R != rowR || c.G != rowG || c.B != rowB {
		t.Errorf("right of zone = %d,%d,%d want SelectedListItem %d,%d,%d", c.R, c.G, c.B, rowR, rowG, rowB)
	}
	c = b.Image().RGBAAt(2, 16+8) // apparatus, left of the text
	if c.R != rowR || c.G != rowG || c.B != rowB {
		t.Errorf("apparatus = %d,%d,%d want SelectedListItem %d,%d,%d", c.R, c.G, c.B, rowR, rowG, rowB)
	}
}

// Ledger banding: non-selected rows alternate LedgerOdd/LedgerEven,
// selection keeps the selection colors, and the blank area below the
// last row keeps the plain list background.
func TestTreeLedgerRows(t *testing.T) {
	b, _ := raster.New(480, 160)
	d := NewDesktop()
	d.SetBackend(b)
	tv := NewTreeView()
	tv.SetParent(d)
	tv.SetShowHeader(true)
	tv.AddColumn(NewTreeColumn("size", "Size", 10))
	for _, name := range []string{"aaa", "bbb", "ccc"} {
		tv.AddRootItem(NewTreeItem(name))
	}
	tv.SetLedger(true)
	tv.SetCurrentIndex(0) // rows 1 (even) and 2 (odd) show both bands
	tv.SetBounds(core.UnitRect{Width: 480, Height: 160})
	b.Clear(style.DefaultStyle())
	tv.Paint(core.NewPainter(b))

	scheme := tv.GetScheme()
	oddR, oddG, oddB := scheme.GetLedgerOdd().Bg.RGBComponents()
	evenR, evenG, evenB := scheme.GetLedgerEven().Bg.RGBComponents()
	listR, listG, listB := scheme.GetListBG().RGBComponents()

	// Sample a background pixel mid-row, away from text (x=300).
	at := func(y int) (uint8, uint8, uint8) {
		c := b.Image().RGBAAt(300, y)
		return c.R, c.G, c.B
	}
	// Row 0 is selected: NOT a ledger color.
	if r, g, bl := at(16 + 8); r == evenR && g == evenG && bl == evenB {
		t.Errorf("selected row painted in a ledger color (%d,%d,%d)", r, g, bl)
	}
	// Row 1 (index 1, 1-based row 2): ledger EVEN (dark gray).
	if r, g, bl := at(32 + 8); r != evenR || g != evenG || bl != evenB {
		t.Errorf("row 1 bg = %d,%d,%d want LedgerEven %d,%d,%d", r, g, bl, evenR, evenG, evenB)
	}
	// Row 2 (index 2, 1-based row 3): ledger ODD (black).
	if r, g, bl := at(48 + 8); r != oddR || g != oddG || bl != oddB {
		t.Errorf("row 2 bg = %d,%d,%d want LedgerOdd %d,%d,%d", r, g, bl, oddR, oddG, oddB)
	}
	// Blank space below the rows: the plain list background.
	if r, g, bl := at(120); r != listR || g != listG || bl != listB {
		t.Errorf("blank area bg = %d,%d,%d want list %d,%d,%d", r, g, bl, listR, listG, listB)
	}
	// Header band: the scheme Header background.
	hR, hG, hB := scheme.GetHeader().Bg.RGBComponents()
	if r, g, bl := at(8); r != hR || g != hG || bl != hB {
		t.Errorf("header bg = %d,%d,%d want Header %d,%d,%d", r, g, bl, hR, hG, hB)
	}
}
