package window

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// metricsHost is a bare container that declares a denomination, standing in
// for whatever holds a window. A top-level window's frame denomination is the
// desktop's, which is why this went unnoticed there; an MDI child's is its
// pane's, and the pane sits inside the window content the Denomination tab
// re-expresses, so a document window's chrome is drawn in whatever the host
// window was set to.
type metricsHost struct {
	core.TrinketBase
	kids []core.Trinket
}

func newMetricsHost(m core.CellMetrics) *metricsHost {
	h := &metricsHost{}
	h.TrinketBase = *core.NewTrinketBase()
	h.Init(h)
	h.SetCellMetrics(&m)
	return h
}

func (h *metricsHost) Children() []core.Trinket            { return h.kids }
func (h *metricsHost) AddChild(c core.Trinket)             { h.kids = append(h.kids, c); c.SetParent(h) }
func (h *metricsHost) RemoveChild(core.Trinket)            {}
func (h *metricsHost) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (h *metricsHost) Layout()                             {}
func (h *metricsHost) LayoutManager() core.LayoutManager   { return nil }
func (h *metricsHost) SetLayoutManager(core.LayoutManager) {}

const barTitle = "A Document Window"

var barOuter = core.CellMetrics{CellWidth: 8, CellHeight: 16}

var barFrames = []core.CellMetrics{
	{CellWidth: 8, CellHeight: 16},
	{CellWidth: 16, CellHeight: 32},
	{CellWidth: 32, CellHeight: 64},
	{CellWidth: 4, CellHeight: 8},
}

// borderFrames adds a square cell: 16x16 over an 8x16 surface is where one
// border count serving both axes stops working.
var borderFrames = []core.CellMetrics{
	{CellWidth: 8, CellHeight: 16},
	{CellWidth: 16, CellHeight: 32},
	{CellWidth: 32, CellHeight: 64},
	{CellWidth: 4, CellHeight: 8},
	{CellWidth: 16, CellHeight: 16},
}

// hostedWindow returns a tearable window whose frame counts in the given
// denomination, sized 40 cells by 5 rows in that same currency.
func hostedWindow(frame core.CellMetrics) *Window {
	host := newMetricsHost(frame)
	w := NewWindow(barTitle)
	w.SetFlags(w.Flags() | WindowFlagTearable)
	host.AddChild(w)
	w.SetBounds(core.UnitRect{
		Width:  40 * frame.CellWidth,
		Height: 5 * frame.CellHeight,
	})
	w.Layout()
	return w
}

// Every length a title bar places -- RowH, CellW, ButtonW, the control slots,
// the tear handle's slot -- is counted in the frame denomination, but the
// title and the control glyphs were measured with Font.MeasureText, which
// answers in the default one. The two only agreed at 8x16.
//
// A width is the same physical width whatever it is counted in, so stating
// each answer at 8x16 is what makes them comparable.
func TestTitleBarMeasuresInTheFrameDenomination(t *testing.T) {
	var baseTitle, baseGlyph core.Unit
	for _, frame := range barFrames {
		tm := TitleBarMetricsFor(frame, &core.Font{Name: "ui-text", Size: 12}, true)
		gotTitle := core.ExchangeX(tm.TitleWidth(barTitle), frame, barOuter)
		gotGlyph := core.ExchangeX(tm.GlyphWidth("[x]"), frame, barOuter)

		if baseTitle == 0 {
			baseTitle, baseGlyph = gotTitle, gotGlyph
			continue
		}
		if gotTitle != baseTitle {
			t.Errorf("frame %dx%d measures the title at %d units at 8x16, want %d",
				frame.CellWidth, frame.CellHeight, gotTitle, baseTitle)
		}
		if gotGlyph != baseGlyph {
			t.Errorf("frame %dx%d measures a control glyph at %d units at 8x16, want %d",
				frame.CellWidth, frame.CellHeight, gotGlyph, baseGlyph)
		}
	}
}

// The whole frame, painted onto one fixed surface. A window of the same
// physical size is the same picture whatever its frame counts in: the title
// centered in the same place, the tear handle in the same slot beside it, the
// controls' glyphs centered in their own.
func TestWindowFramePaintsTheSameAtEveryFrameDenomination(t *testing.T) {
	const W, H = 400, 120
	var base *raster.Backend

	for _, frame := range barFrames {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(barOuter)
		b.Clear(style.DefaultStyle())

		hostedWindow(frame).Paint(core.NewPainter(b).WithDenomination(barOuter, frame))

		if base == nil {
			base = b
			continue
		}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				if b.Image().RGBAAt(x, y) != base.Image().RGBAAt(x, y) {
					t.Errorf("frame %dx%d: pixel (%d,%d) is %v, want %v (the 8x16 picture)",
						frame.CellWidth, frame.CellHeight, x, y,
						b.Image().RGBAAt(x, y), base.Image().RGBAAt(x, y))
					y, x = H, W
				}
			}
		}
	}
}

// The tear handle floats immediately left of the centered title, so where it
// can be grabbed is decided by the title's measured width. Measured in the
// wrong denomination it sat somewhere the picture does not show it.
//
// Positions are read off the 8x16 frame and replayed at each denomination in
// that frame's own currency: the same physical spot every time.
func TestTitleBarHitsWhatItPaintsAtEveryFrameDenomination(t *testing.T) {
	base := hostedWindow(barOuter)

	// Walk the bar and record what the default frame answers at each column,
	// then require every other denomination to answer the same at the same
	// physical column.
	for px := core.Unit(0); px < 40*barOuter.CellWidth; px += 2 {
		want := base.buttonAtPosition(px, barOuter.CellHeight/2)
		for _, frame := range barFrames[1:] {
			w := hostedWindow(frame)
			got := w.buttonAtPosition(
				core.ExchangeX(px, barOuter, frame),
				core.ExchangeY(barOuter.CellHeight/2, barOuter, frame),
			)
			if got != want {
				t.Fatalf("frame %dx%d: column %d hits %v, want %v (what 8x16 hits)",
					frame.CellWidth, frame.CellHeight, px, got, want)
			}
		}
	}
}

// framedHost plays the desktop's part as well: it reports the frame border
// in ITS OWN units, which is what Desktop.WindowFrameBorderUnits does (device
// pixels divided by that surface's pixels-per-unit).
type framedHost struct {
	metricsHost
	borderUnits core.Unit
}

func newFramedHost(m core.CellMetrics, border core.Unit) *framedHost {
	h := &framedHost{borderUnits: border}
	h.TrinketBase = *core.NewTrinketBase()
	h.Init(h)
	h.SetCellMetrics(&m)
	return h
}

func (h *framedHost) WindowFrameBorderUnits() core.Unit { return h.borderUnits }
func (h *framedHost) GraphicalWindowFrames() bool       { return true }
func (h *framedHost) SurfacePxPerUnit() float64         { return 1 }
func (h *framedHost) AddChild(c core.Trinket)           { h.kids = append(h.kids, c); c.SetParent(h) }

// hostedFramedWindow puts the window two levels down: a surface that reports
// the frame border in ITS units, then a pane that re-denominates. That is the
// MDI topology -- the desktop provides the border, the pane provides the
// denomination the child's chrome is drawn in.
func hostedFramedWindow(frame core.CellMetrics) *Window {
	surface := newFramedHost(barOuter, 2)
	pane := newMetricsHost(frame)
	surface.AddChild(pane)

	w := NewWindow(barTitle)
	w.SetFlags(w.Flags() | WindowFlagTearable)
	pane.AddChild(w)
	w.SetBounds(core.UnitRect{Width: 40 * frame.CellWidth, Height: 5 * frame.CellHeight})
	w.Layout()
	return w
}

// The border is one physical thickness, and every frame spends whatever its
// own denomination makes that. FindFrameBorderUnits answers in the PROVIDER's
// units, which for a top-level window is also the frame's -- so this stayed
// hidden there -- and for an MDI child is the desktop's while the frame counts
// in the pane's.
//
// Two counts, not one: a unit is square only where the cell is. Over an 8x16
// surface a 16x16 frame spends four units on the border across and two down.
func TestFrameBorderIsTheSameThicknessAtEveryFrameDenomination(t *testing.T) {
	var baseX, baseY core.Unit
	for _, frame := range borderFrames {
		bx, by := hostedFramedWindow(frame).frameBorder()
		gotX := core.ExchangeX(bx, frame, barOuter)
		gotY := core.ExchangeY(by, frame, barOuter)

		if baseX == 0 {
			baseX, baseY = gotX, gotY
			continue
		}
		if gotX != baseX || gotY != baseY {
			t.Errorf("frame %dx%d reserves %dx%d units at 8x16, want %dx%d",
				frame.CellWidth, frame.CellHeight, gotX, gotY, baseX, baseY)
		}
	}
}

// And the whole bordered frame, painted onto one surface: the border stroke,
// the rounded corners, the title bar inside them. The corner radius is NOT
// re-expressed -- the rounded-rect painter transforms the rectangle and passes
// the radius through, so that number is screen-space -- and this is what says
// so: denominating it too pulls the corners apart.
func TestBorderedFramePaintsTheSameAtEveryFrameDenomination(t *testing.T) {
	const W, H = 400, 120
	var base *raster.Backend

	for _, frame := range borderFrames {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(barOuter)
		b.Clear(style.DefaultStyle())

		hostedFramedWindow(frame).Paint(core.NewPainter(b).WithDenomination(barOuter, frame))

		if base == nil {
			base = b
			continue
		}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				if b.Image().RGBAAt(x, y) != base.Image().RGBAAt(x, y) {
					t.Errorf("frame %dx%d: pixel (%d,%d) is %v, want %v (the 8x16 picture)",
						frame.CellWidth, frame.CellHeight, x, y,
						b.Image().RGBAAt(x, y), base.Image().RGBAAt(x, y))
					y, x = H, W
				}
			}
		}
	}
}
