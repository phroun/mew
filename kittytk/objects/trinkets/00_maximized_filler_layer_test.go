package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// The shade a maximized window leaves around it is painted on the DESKTOP's
// layer, not inside the window paint loop.
//
// A GPU compositor takes each window as a layer of its own and never runs that
// loop, so a filler painted there showed up in the software path and nowhere
// else -- which is to say nowhere the demo actually runs.
func TestTheMaximizedFillerIsOnTheDesktopsOwnLayer(t *testing.T) {
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

	rgb := func() (int, int, int) {
		r, g, b, _ := px.Image().At(100, 400).RGBA()
		return int(r >> 8), int(g >> 8), int(b >> 8)
	}

	// Un-maximized, that point is bare desktop.
	d.Paint(core.NewPainter(px))
	br, bg, bb := rgb()

	wm.MaximizeWindow(win)
	if got := win.Bounds(); got.Width != 480 || got.Height != 320 {
		t.Fatalf("maximized it is %v, want its maximum of 480x320", got.Size())
	}
	d.Paint(core.NewPainter(px))
	fr, fg, fb := rgb()

	if fr == br && fg == bg && fb == bb {
		t.Fatalf("the desktop layer is unchanged at %d,%d,%d where the window left room", br, bg, bb)
	}

	// And it is the window's own title colour taken a quarter toward black,
	// which is what makes it read as room the window declined.
	wr, wg, wb := renderedBg(t, win.GetScheme().GetWindowTitle(true))
	for _, c := range []struct {
		name       string
		got, whole int
	}{{"red", fr, wr}, {"green", fg, wg}, {"blue", fb, wb}} {
		if want := c.whole * 3 / 4; c.got < want-2 || c.got > want+2 {
			t.Errorf("%s is %d in the shade against %d in the frame, want about %d",
				c.name, c.got, c.whole, want)
		}
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
