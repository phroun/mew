package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/style"
)

// capOuter is the denomination each render below is displayed through: at
// 8x16 one unit is one device pixel, so widths stated in it read as physical
// sizes. capDenominations is what the trinket's interior counts in.
var capOuter = core.CellMetrics{CellWidth: 8, CellHeight: 16}

var capDenominations = []core.CellMetrics{
	{CellWidth: 8, CellHeight: 16},
	{CellWidth: 16, CellHeight: 32},
	{CellWidth: 32, CellHeight: 64},
	{CellWidth: 4, CellHeight: 8},
}

// sameWidthAtEveryDenomination builds the trinket at each denomination and
// checks the width it asks for is the same physical width every time, stating
// each answer at 8x16 so they compare.
func sameWidthAtEveryDenomination(t *testing.T, what string, build func(core.CellMetrics) core.Trinket) {
	t.Helper()
	base := core.Unit(0)
	for _, interior := range capDenominations {
		got := core.ExchangeX(build(interior).SizeHint().Width, interior, capOuter)
		if base == 0 {
			base = got
			continue
		}
		if got != base {
			t.Errorf("%s at %dx%d asks for %d units at 8x16, want %d",
				what, interior.CellWidth, interior.CellHeight, got, base)
		}
	}
}

// A separator's width is its title plus the stubs of rule either side, and a
// combo box's is its widest item plus the dropdown arrow. Both measured their
// text with Font.MeasureText, which answers in default-denomination units,
// while the rest of each hint was counted in the trinket's own.
func TestSeparatorAsksForTheSameWidthAtEveryDenomination(t *testing.T) {
	sameWidthAtEveryDenomination(t, "separator", func(m core.CellMetrics) core.Trinket {
		s := NewHSeparator("Appearance")
		s.SetCellMetrics(&m)
		return s
	})
}

func TestComboBoxAsksForTheSameWidthAtEveryDenomination(t *testing.T) {
	sameWidthAtEveryDenomination(t, "combo box", func(m core.CellMetrics) core.Trinket {
		c := NewComboBox()
		c.AddItems([]string{"Monday", "Tuesday", "Wednesday afternoon"})
		c.SetCellMetrics(&m)
		return c
	})
}

// A combo box hands a press straight to whatever is under it, bounded by the
// width the layout gave it, so a hint that under-counts the items leaves the
// right of the control dead. This presses three quarters of the way across,
// which is past where a half-counted hint reaches.
func TestComboBoxTakesAPressOnItsRightHalf(t *testing.T) {
	row := func(interior core.CellMetrics) (*Panel, *ComboBox) {
		p := NewPanel()
		p.SetLayoutManager(layout.NewBoxLayout(core.Horizontal))
		c := NewComboBox()
		c.AddItems([]string{"Monday", "Tuesday", "Wednesday afternoon"})
		p.AddChild(c)
		p.AddChild(NewLabel("after"))
		p.SetCellMetrics(&interior)
		p.SetBounds(core.UnitRect{Width: 800, Height: 16})
		p.Layout()
		return p, c
	}

	_, c0 := row(capOuter)
	pressX := c0.Bounds().X + c0.Bounds().Width*3/4

	for _, interior := range capDenominations {
		p, c := row(interior)
		p.HandleMousePress(core.MousePressEvent{X: pressX, Y: 8, Button: core.LeftButton})
		if !c.HasFocus() {
			t.Errorf("interior %dx%d: a press at %d missed the combo box, which spans %d..%d",
				interior.CellWidth, interior.CellHeight, pressX,
				core.ExchangeX(c.Bounds().X, interior, capOuter),
				core.ExchangeX(c.Bounds().X+c.Bounds().Width, interior, capOuter))
		}
	}
}

// capInkColumns returns the device columns carrying anything the trinket
// painted, against the cleared surface.
func capInkColumns(b *raster.Backend, w, h int) []int {
	img := b.Image()
	empty := img.RGBAAt(w-1, h-1)
	var cols []int
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			if img.RGBAAt(x, y) != empty {
				cols = append(cols, x)
				break
			}
		}
	}
	return cols
}

// sameInkColumns paints onto a fixed 8x16 surface at each denomination and
// compares the columns that carry ink. Columns rather than pixels where the
// trinket centres something vertically: halving an odd number of local units
// lands a row differently at each denomination, which moves the whole caption
// a device pixel up or down without moving anything horizontally.
func sameInkColumns(t *testing.T, what string, paint func(*core.Painter, core.CellMetrics)) {
	t.Helper()
	const W, H = 400, 40
	var base []int
	for _, interior := range capDenominations {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(capOuter)
		b.Clear(style.DefaultStyle())
		paint(core.NewPainter(b).WithDenomination(capOuter, interior), interior)

		got := capInkColumns(b, W, H)
		if base == nil {
			base = got
			if len(base) == 0 {
				t.Fatalf("%s painted nothing at 8x16; the test reads nothing", what)
			}
			continue
		}
		if len(got) != len(base) {
			t.Errorf("%s at %dx%d painted %d ink columns, want %d (the 8x16 picture)",
				what, interior.CellWidth, interior.CellHeight, len(got), len(base))
			continue
		}
		for i := range base {
			if got[i] != base[i] {
				t.Errorf("%s at %dx%d: ink column %d at px %d, want %d",
					what, interior.CellWidth, interior.CellHeight, i, got[i], base[i])
				break
			}
		}
	}
}

// samePixels compares whole renders rather than columns. Restricted to the
// denominations given: at one coarser than the outer a local unit spans more
// than a device pixel, so a one-unit hairline is genuinely thicker there and
// there is nothing to compare it against.
func samePixels(t *testing.T, what string, denominations []core.CellMetrics, paint func(*core.Painter, core.CellMetrics)) {
	t.Helper()
	const W, H = 400, 40
	var base *raster.Backend
	for _, interior := range denominations {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(capOuter)
		b.Clear(style.DefaultStyle())
		paint(core.NewPainter(b).WithDenomination(capOuter, interior), interior)

		if base == nil {
			base = b
			continue
		}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				if b.Image().RGBAAt(x, y) != base.Image().RGBAAt(x, y) {
					t.Errorf("%s at %dx%d: pixel (%d,%d) is %v, want %v (the 8x16 picture)",
						what, interior.CellWidth, interior.CellHeight, x, y,
						b.Image().RGBAAt(x, y), base.Image().RGBAAt(x, y))
					y, x = H, W
				}
			}
		}
	}
}

// On a pixel surface both a separator and a splitter break their rule around
// a caption box: the caption is measured in screen space and converted into
// local units, but the padding either side of it was a bare count of local
// units, so it was a different physical width at every denomination and the
// gap in the rule moved with it.
func TestSeparatorPaintsTheSameAtEveryDenomination(t *testing.T) {
	sameInkColumns(t, "separator", paintCapSeparator)
}

func TestSplitterCaptionPaintsTheSameAtEveryDenomination(t *testing.T) {
	sameInkColumns(t, "splitter", paintCapSplitter)
	// The rule spans every column whatever the caption does, so columns alone
	// would not see the gap move. Pixels do.
	samePixels(t, "splitter", capDenominations[:3], paintCapSplitter)
}

// A dock entry is a fixed number of cells wide and elides its title to the
// interior between the brackets. The interior is counted in the dock's units
// and the title was measured in default ones, so at a doubled denomination
// the title was let run to twice the room it has. The entry paints its own
// background across every column it covers, so this reads pixels.
func TestDockEntryTitleElidesTheSameAtEveryDenomination(t *testing.T) {
	samePixels(t, "dock entry", capDenominations, paintCapDock)
}

func paintCapSeparator(p *core.Painter, m core.CellMetrics) {
	s := NewHSeparator("Appearance")
	s.SetCellMetrics(&m)
	s.SetBounds(core.UnitRect{Width: 300 * m.CellWidth / 8, Height: m.CellHeight})
	s.Paint(p)
}

func paintCapSplitter(p *core.Painter, m core.CellMetrics) {
	sp := NewSplitter(core.Vertical)
	sp.SetTitle("Panes")
	sp.SetCellMetrics(&m)
	sp.SetBounds(core.UnitRect{Width: 300 * m.CellWidth / 8, Height: 30 * m.CellHeight / 16})
	sp.Paint(p)
}

func paintCapDock(p *core.Painter, m core.CellMetrics) {
	d := NewDockRow()
	d.SetEntryWidth(16)
	d.AddEntry(&DockEntry{Title: "A window title too long for the slot", WindowID: 1})
	d.SetCellMetrics(&m)
	d.SetBounds(core.UnitRect{Width: 300 * m.CellWidth / 8, Height: m.CellHeight})
	d.Paint(p)
}
