package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// reentrantTearPlatform reproduces the SDL re-entrancy behind the solo-mode
// double/ghost dialog: creating the torn OS surface fires an event-watch that
// drains the post queue synchronously, re-running a deferred tear of the SAME
// window while it is still mid-createTornHost (before that call removes it from
// the manager / marks it detached). Its CreateSurface calls back into
// createTornHost for that window exactly once, on the first torn surface.
type reentrantTearPlatform struct {
	*msPlatform
	d         *Desktop
	win       *window.Window
	reentered bool
}

// Run passes the WRAPPER (not the embedded msPlatform) to init, so d.surface —
// and every later plat.CreateSurface via d.platform — routes through the
// re-entrant override below.
func (p *reentrantTearPlatform) Run(init func(platform.Platform)) int {
	init(p)
	if p.msPlatform.script != nil {
		p.msPlatform.script()
	}
	return 0
}

func (p *reentrantTearPlatform) CreateSurface(o platform.SurfaceOptions) (platform.Surface, error) {
	s, err := p.msPlatform.CreateSurface(o)
	// The first TORN surface (surfaces == primary + this one) is where SDL
	// would re-enter: simulate the deferred tear of the same window firing now,
	// before the outer createTornHost has latched its claimed state.
	if err == nil && !p.reentered && len(p.msPlatform.surfaces) == 2 {
		p.reentered = true
		p.d.createTornHost(p.win, 10, 10)
	}
	return s, err
}

// TestTearOffReentrantDoesNotDoubleHost locks the fix for the solo-mode
// double/ghost dialog. A window must never be hosted on two surfaces just
// because a re-entrant tear fired before createTornHost latched its claimed
// state — the tearing guard makes the re-entrant call a no-op.
func TestTearOffReentrantDoesNotDoubleHost(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("dlg")
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})

	plat := &reentrantTearPlatform{msPlatform: &msPlatform{}, d: d, win: win}
	plat.msPlatform.script = func() {
		d.WindowManager().AddWindow(win)
		win.Layout()

		// Outer tear. Its CreateSurface re-enters createTornHost(win); without
		// the guard that would build a second host on a third surface.
		host := d.createTornHost(win, 0, 0)
		if host == nil {
			t.Fatal("outer tear-off returned nil host")
		}
		if !plat.reentered {
			t.Fatal("re-entrancy was not exercised (test would not catch the bug)")
		}
		// Exactly the primary desktop surface + one torn surface, and the
		// window hosted exactly once.
		if n := len(plat.msPlatform.surfaces); n != 2 {
			t.Fatalf("re-entrant tear created an extra surface: %d total (want 2)", n)
		}
		if n := len(d.tornHosts); n != 1 {
			t.Fatalf("window hosted %d times (want 1)", n)
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}
