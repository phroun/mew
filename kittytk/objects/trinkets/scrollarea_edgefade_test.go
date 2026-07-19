package trinkets

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// solidFill is a test trinket that paints its whole bounds one color.
type solidFill struct {
	core.TrinketBase
	col  style.Color
	w, h core.Unit
}

func newSolidFill(col style.Color, w, h core.Unit) *solidFill {
	f := &solidFill{col: col, w: w, h: h}
	f.TrinketBase = *core.NewTrinketBase()
	f.Init(f)
	return f
}
func (f *solidFill) SizeHint() core.UnitSize { return core.UnitSize{Width: f.w, Height: f.h} }
func (f *solidFill) Paint(p *core.Painter) {
	b := f.Bounds()
	p.FillRect(core.UnitRect{Width: b.Width, Height: b.Height}, ' ',
		style.DefaultStyle().WithBg(f.col))
}

func dist(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}

// A graphical ScrollArea fades its content toward the scroll-area background
// on every edge that has more content beyond it: opaque at the outer edge,
// transparent (content shows through) a row/two-columns inward. When scrolled
// to the top-left, only the bottom and right edges fade.
func TestScrollAreaEdgeFades(t *testing.T) {
	b, err := raster.New(400, 400)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)

	content := style.RGB(20, 220, 20) // distinct from any dark default bg
	sa := NewScrollArea()
	sa.SetParent(d)
	sa.SetContent(newSolidFill(content, 960, 960))
	sa.SetBounds(core.UnitRect{Width: 320, Height: 320})

	p := core.NewPainter(b)
	viewport := sa.viewportBounds()
	vw := p.UnitSpanPxX(0, viewport.Width)
	vh := p.UnitSpanPxY(0, viewport.Height)

	bgR, bgG, bgB := sa.EffectiveBackgroundColor().RGBComponents()
	bg := color.RGBA{bgR, bgG, bgB, 255}
	cc := color.RGBA{20, 220, 20, 255}

	paint := func() { b.Clear(style.DefaultStyle()); sa.Paint(p) }
	at := func(x, y int) color.RGBA { return b.Image().RGBAAt(x, y) }
	nearBg := func(x, y int, what string) {
		if px := at(x, y); dist(px, bg) >= dist(px, cc) {
			t.Errorf("%s: pixel (%d,%d)=%v not close to bg %v (content %v)", what, x, y, px, bg, cc)
		}
	}
	isContent := func(x, y int, what string) {
		if px := at(x, y); dist(px, cc) > 6 {
			t.Errorf("%s: pixel (%d,%d)=%v not content %v", what, x, y, px, cc)
		}
	}

	// Scrolled to the middle: all four edges have more content -> all fade.
	sa.SetScrollX(sa.hScrollBar.Maximum() / 2)
	sa.SetScrollY(sa.vScrollBar.Maximum() / 2)
	paint()

	cx, cy := vw/2, vh/2
	// Outer edge is opaque bg; a bit inward is content again.
	nearBg(cx, 0, "top outer")
	isContent(cx, vh/3, "top inner")
	nearBg(cx, vh-1, "bottom outer")
	isContent(cx, vh*2/3, "bottom inner")
	nearBg(0, cy, "left outer")
	isContent(vw/3, cy, "left inner")
	nearBg(vw-1, cy, "right outer")
	isContent(vw*2/3, cy, "right inner")

	// Outer corner pixel is opaque bg where two fades meet (miter).
	nearBg(0, 0, "top-left corner")
	nearBg(vw-1, vh-1, "bottom-right corner")

	// Scrolled to the top-left: nothing above or left, so those edges do NOT
	// fade (outer pixels are content), while bottom/right still do.
	sa.SetScrollX(0)
	sa.SetScrollY(0)
	paint()
	isContent(cx, 0, "top edge (no fade at origin)")
	isContent(0, cy, "left edge (no fade at origin)")
	nearBg(cx, vh-1, "bottom outer (at origin)")
	nearBg(vw-1, cy, "right outer (at origin)")
}
