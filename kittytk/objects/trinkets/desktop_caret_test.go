package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// caretAsker requests the platform text caret from inside a desktop-composited
// window — what a focused terminal trinket does.
type caretAsker struct {
	core.TrinketBase
	style int
}

func newCaretAsker(style int) *caretAsker {
	c := &caretAsker{style: style}
	c.TrinketBase = *core.NewTrinketBase()
	return c
}

func (c *caretAsker) Paint(p *core.Painter) { p.RequestTextCaret(4, 6, c.style) }

// The DESKTOP composites every window into one surface, so it owns the frame
// and must apply the caret request itself. Without this a focused terminal
// asks for the platform caret and nothing places it — which is exactly a
// terminal host showing no cursor at all.
func TestDesktopFrameAppliesTextCaret(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	surf := &msSurface{size: core.UnitSize{Width: 800, Height: 240}}
	d.surface = surf
	h := &desktopSurfaceHandler{d: d}

	h.Frame(core.NewPainter(px))
	if surf.caretVisible {
		t.Fatal("no window asked for the caret: it should stay hidden")
	}

	win := window.NewWindow("W")
	win.SetContent(newCaretAsker(5))
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 800, Height: 240})
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
	win.Layout()

	h.Frame(core.NewPainter(px))
	if !surf.caretVisible {
		t.Fatal("a window's caret request should reach the surface")
	}
	if surf.caretStyle != 5 {
		t.Fatalf("caret style = %d, want the requested 5", surf.caretStyle)
	}
}
