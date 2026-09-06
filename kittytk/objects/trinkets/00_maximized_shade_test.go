package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// A bounded window maximized on the desktop, painted end to end.
//
// The shade is painted by the WINDOW, inside its own surface, because that
// surface is the whole room: on a compositing host each window is a layer of
// its own that repaints when the window changes, and the room around the
// frame changes at exactly the same moment. Painted on the desktop's layer
// instead it was at the mercy of a base-layer cache that had every reason
// not to repaint.
func TestTheMaximizedShadeIsPaintedOnScreen(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(1200, 800)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)
	d.SetBounds(core.UnitRect{Width: 1200, Height: 800})
	wm := d.WindowManager()

	win := window.NewWindow("Bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 280, Height: 160})
	wm.AddWindow(win)

	rgb := func(x, y int) (int, int, int) {
		r, g, b, _ := px.Image().At(x, y).RGBA()
		return int(r >> 8), int(g >> 8), int(b >> 8)
	}

	// Un-maximized, a point out at the left is bare desktop.
	wm.Paint(core.NewPainter(px))
	br, bg, bb := rgb(100, 400)

	wm.MaximizeWindow(win)
	room := wm.ClientArea()
	if got := win.Bounds(); got != room {
		t.Fatalf("maximized its surface is %v, want the whole room %v", got, room)
	}
	wm.Paint(core.NewPainter(px))
	sr, sg, sb := rgb(100, 400)

	if sr == br && sg == bg && sb == bb {
		t.Fatalf("the room beside the window is unchanged at %d,%d,%d after maximizing", br, bg, bb)
	}

	// And it is the window's own title colour taken a quarter toward black,
	// which is what makes it read as room the window declined.
	wr, wg, wb := renderedBg(t, win.GetScheme().GetWindowTitle(true))
	for _, c := range []struct {
		name       string
		got, whole int
	}{{"red", sr, wr}, {"green", sg, wg}, {"blue", sb, wb}} {
		if want := c.whole * 3 / 4; c.got < want-2 || c.got > want+2 {
			t.Errorf("%s is %d in the shade against %d in the frame, want about %d",
				c.name, c.got, c.whole, want)
		}
	}

	// The middle of the room is the window itself, not shade.
	fr := window.MaximizedFrameRect(win, room.Size())
	mr, mg, mb := rgb(int(room.X+fr.X+fr.Width/2), int(room.Y+fr.Y+fr.Height/2))
	if mr == sr && mg == sg && mb == sb {
		t.Errorf("the middle of the room is the same %d,%d,%d as the shade; "+
			"the frame is not being painted inside it", mr, mg, mb)
	}
}

// A compositing host clears a layer to TRANSPARENT before painting it, so the
// shade has to land as opaque pixels of its own. Left translucent it would
// let the wallpaper quad below show through and read as the bare desktop the
// shade exists to cover.
func TestTheShadeIsOpaqueOnATransparentLayer(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(1200, 800)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)
	d.SetBounds(core.UnitRect{Width: 1200, Height: 800})
	wm := d.WindowManager()

	win := window.NewWindow("Bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 280, Height: 160})
	wm.AddWindow(win)
	wm.MaximizeWindow(win)

	p := core.NewPainter(px)
	if !p.ClearTransparent() {
		t.Skip("this backend cannot clear to transparent")
	}
	wm.Paint(p)

	if _, _, _, a := px.Image().At(100, 400).RGBA(); a>>8 != 0xff {
		t.Errorf("the shade is %d/255 opaque, want it fully covering what is below", a>>8)
	}
}

// renderedBg fills a scratch raster with the given style and reads a pixel
// back, which is how a scheme colour is turned into the channels it renders
// as -- the only footing on which a shade can be compared to what it shades.
func renderedBg(t *testing.T, s style.CellStyle) (int, int, int) {
	t.Helper()
	probe, err := raster.New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	core.NewPainter(probe).FillRect(core.UnitRect{Width: 64, Height: 64}, ' ', s)
	r, g, b, _ := probe.Image().At(32, 32).RGBA()
	return int(r >> 8), int(g >> 8), int(b >> 8)
}
