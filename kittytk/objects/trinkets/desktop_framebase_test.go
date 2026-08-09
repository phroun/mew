package trinkets

import (
	"image"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// paintProbe records whether a window's content painted.
type paintProbe struct {
	core.TrinketBase
	painted bool
}

func newPaintProbe() *paintProbe {
	p := &paintProbe{}
	p.TrinketBase = *core.NewTrinketBase()
	return p
}

func (p *paintProbe) Paint(*core.Painter) { p.painted = true }

// Frame paints the COMPLETE scene — child windows included — for every
// non-compositing present (software renderer, resize frames). FrameBase
// paints only desktop chrome: the GPU compositor renders windows on
// their own layers. Regression coverage for the desync where Frame
// painted chrome-only whenever windows existed, which blanked all open
// windows on the software path and during live resizes.
func TestDesktopFramePaintsWindowsFrameBaseDoesNot(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	surf := &msSurface{size: core.UnitSize{Width: 800, Height: 240}}
	d.surface = surf
	h := &desktopSurfaceHandler{d: d}

	probe := newPaintProbe()
	win := window.NewWindow("W")
	win.SetContent(probe)
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 800, Height: 240})
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
	win.Layout()

	h.Frame(core.NewPainter(px))
	if !probe.painted {
		t.Error("Frame must paint child windows (software/non-compositing path)")
	}

	probe.painted = false
	h.FrameBase(core.NewPainter(px))
	if probe.painted {
		t.Error("FrameBase must NOT paint child windows (the compositor layers them)")
	}

	// The desktop advertises its windows to the compositor.
	var provider platform.WindowProvider = h
	list := provider.GetChildWindows()
	if list == nil || len(list.Windows) != 1 {
		t.Fatalf("GetChildWindows = %+v, want exactly the one open window", list)
	}
}

// A compositor layer paints itself, so a minimized window still listed
// for the compositor keeps drawing — and casting a shadow — over the
// desktop after it was sent to the dock. The software paint loop has
// always applied this filter; the compositor list must apply it too.
func TestDesktopCompositorSkipsMinimizedWindows(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	d.surface = &msSurface{size: core.UnitSize{Width: 800, Height: 240}}
	h := &desktopSurfaceHandler{d: d}

	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 800, Height: 240})
	open := window.NewWindow("Open")
	hidden := window.NewWindow("Hidden")
	minimized := window.NewWindow("Minimized")
	for _, w := range []*window.Window{open, hidden, minimized} {
		d.WindowManager().AddWindow(w)
		w.SetBounds(core.UnitRect{Width: 400, Height: 200})
		w.Layout()
	}
	hidden.SetVisible(false)
	minimized.Minimize()

	var provider platform.WindowProvider = h
	list := provider.GetChildWindows()
	if list == nil {
		t.Fatal("GetChildWindows returned nil with a window manager present")
	}
	if len(list.Windows) != 1 {
		t.Fatalf("compositor got %d windows, want only the one that is open and not minimized",
			len(list.Windows))
	}
	if list.Windows[0] != open {
		t.Errorf("compositor got %v, want the open window", list.Windows[0])
	}
}

// The base layer's revision must hold still while only the windows above
// it change. Windows are the desktop's own trinket children, so a
// keystroke inside one bumps the desktop's subtree counter too — and a
// base-layer cache keyed on that raw counter would repaint the whole
// full-surface texture on every keystroke, which is exactly the cost it
// exists to avoid.
func TestDesktopBaseRevisionIgnoresWindowActivity(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	d.surface = &msSurface{size: core.UnitSize{Width: 800, Height: 240}}
	h := &desktopSurfaceHandler{d: d}
	var provider platform.WindowProvider = h

	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 800, Height: 240})
	win := window.NewWindow("W")
	content := core.NewTrinketBase()
	content.Init(content)
	win.SetContent(content)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: 400, Height: 200})
	win.Layout()

	baseRev := func() uint64 {
		list := provider.GetChildWindows()
		if list == nil || !list.HasBaseRevision {
			t.Fatal("desktop did not report a base revision")
		}
		return list.BaseRevision
	}

	before := baseRev()

	// Anything inside the window: its own layer, not the base's.
	content.Update()
	content.SetVisible(false)
	win.SetActive(true)
	if got := baseRev(); got != before {
		t.Errorf("base revision moved from %d to %d for window-only activity; "+
			"the base layer would repaint on every keystroke in any window", before, got)
	}

	// Chrome IS the base layer, so it must move the revision.
	d.SetWallpaperChunk(4)
	if got := baseRev(); got == before {
		t.Error("base revision did not move when the wallpaper changed")
	}
}

// The wallpaper reaches the compositor as ONE tile plus a revision,
// whatever its size — repeating happens in the GPU's sampler, so the
// layer costs a quad instead of a fill over every pixel of the desktop.
func TestDesktopOffersWallpaperTileToCompositor(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	d.surface = &msSurface{size: core.UnitSize{Width: 800, Height: 240}}
	var provider platform.WindowProvider = &desktopSurfaceHandler{d: d}

	// The default 8x8 pattern is rendered into a tile, so it takes the
	// same path a custom image does.
	list := provider.GetChildWindows()
	if list == nil || list.Wallpaper == nil || list.Wallpaper.Tile == nil {
		t.Fatal("no wallpaper layer offered for the built-in pattern")
	}
	patternRev := list.Wallpaper.Revision
	if b := list.Wallpaper.Tile.Bounds(); b.Dx() != 8*2 || b.Dy() != 8*2 {
		t.Errorf("pattern tile is %dx%d, want 16x16 (8 bits at the default chunk of 2)",
			b.Dx(), b.Dy())
	}

	// An arbitrary-size image replaces it, at its own size.
	custom := image.NewRGBA(image.Rect(0, 0, 37, 91))
	d.SetWallpaperImage(custom)

	list = provider.GetChildWindows()
	if list.Wallpaper.Tile != custom {
		t.Error("the custom wallpaper image was not offered to the compositor")
	}
	if b := list.Wallpaper.Tile.Bounds(); b.Dx() != 37 || b.Dy() != 91 {
		t.Errorf("custom tile is %dx%d, want its own 37x91 — it must not be resized",
			b.Dx(), b.Dy())
	}
	if list.Wallpaper.Revision == patternRev {
		t.Error("the revision did not move for a new wallpaper; it would never be re-uploaded")
	}
}

// The tile's revision moves when the pattern's COLORS change, not just
// its bits — a theme switch repaints the wallpaper without touching the
// pattern, and a revision that missed it would leave the old tile
// uploaded forever.
func TestWallpaperRevisionTracksPatternInputs(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	d.surface = &msSurface{size: core.UnitSize{Width: 800, Height: 240}}

	_, before := d.WallpaperTile()

	d.SetWallpaperChunk(4)
	_, afterChunk := d.WallpaperTile()
	if afterChunk == before {
		t.Error("revision did not move when the chunk size changed")
	}

	d.SetWallpaperPattern([8]uint8{0xF0, 0x0F, 0xF0, 0x0F, 0xF0, 0x0F, 0xF0, 0x0F})
	_, afterPattern := d.WallpaperTile()
	if afterPattern == afterChunk {
		t.Error("revision did not move when the pattern changed")
	}

	// And it holds still when nothing changed, or the tile would upload
	// every frame.
	_, again := d.WallpaperTile()
	if again != afterPattern {
		t.Errorf("revision moved from %d to %d with nothing changed", afterPattern, again)
	}
}

// FrameBase leaves the background to the compositor's wallpaper layer:
// it clears to transparent and Paint skips the fill, so the tiled quad
// underneath shows through. On a surface with no alpha it falls back to
// the opaque clear and the CPU-tiled wallpaper.
func TestFrameBaseClearsTransparentForWallpaper(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(200, 120)
	d := NewDesktop()
	d.SetBackend(px)
	d.surface = &msSurface{size: core.UnitSize{Width: 200, Height: 120}}
	h := &desktopSurfaceHandler{d: d}

	h.FrameBase(core.NewPainter(px))

	// Somewhere in the middle of the desktop, clear of every bar: the
	// base layer must have left it transparent for the wallpaper.
	img := px.Image()
	o := img.PixOffset(100, 60)
	if a := img.Pix[o+3]; a != 0 {
		t.Errorf("desktop background alpha = %d after FrameBase, want 0 — "+
			"the wallpaper layer underneath would be hidden", a)
	}

	// Frame (the non-compositing path) still paints an opaque background.
	h.Frame(core.NewPainter(px))
	if a := img.Pix[img.PixOffset(100, 60)+3]; a == 0 {
		t.Error("Frame left the desktop background transparent; " +
			"the non-compositing present would show nothing")
	}
}

// Repaint requests that never reach a trinket's Update() must still move
// the base layer's revision, or a compositor caching its texture would
// hold a stale one. Both of these bypass the trinket path entirely.
func TestBaseRevisionMovesForNonTrinketRepaintRequests(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 240)
	d := NewDesktop()
	d.SetBackend(px)
	d.surface = &msSurface{size: core.UnitSize{Width: 800, Height: 240}}

	for _, tc := range []struct {
		name    string
		request func()
	}{
		{"RequestUpdate (wallpaper and theme setters call it)", d.RequestUpdate},
		{"InvalidateRect (a ticking clock, a blinking caret)", func() {
			d.InvalidateRect(core.UnitRect{X: 10, Y: 10, Width: 40, Height: 8})
		}},
		{"InvalidateRect with a degenerate rect", func() {
			d.InvalidateRect(core.UnitRect{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := d.SubtreeRepaintRevision()
			tc.request()
			if d.SubtreeRepaintRevision() == before {
				t.Error("revision did not move; the base layer would keep a stale texture " +
					"until the compositor's heartbeat")
			}
		})
	}
}

// caretProbe asks for the platform caret at a fixed local position while
// it paints, the way a focused text field does.
type caretProbe struct {
	core.TrinketBase
	x, y core.Unit
}

func newCaretProbe(x, y core.Unit) *caretProbe {
	p := &caretProbe{x: x, y: y}
	p.TrinketBase = *core.NewTrinketBase()
	p.Init(p)
	return p
}

func (c *caretProbe) Paint(p *core.Painter) { p.RequestTextCaret(c.x, c.y, 2) }

// FrameBase must NOT apply the caret itself. Child windows, menus and
// popups paint on compositor layers of their own and any of them may
// claim the caret; the host gathers every layer's request and applies
// the single winner. A chrome-only request applied here would hide a
// focused window's caret for the rest of the frame — which is why a
// docked window's caret never reached the OS at all.
func TestFrameBaseLeavesTheCaretToTheHost(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(400, 200)
	d := NewDesktop()
	d.SetBackend(px)
	surf := &msSurface{size: core.UnitSize{Width: 400, Height: 200}}
	d.surface = surf
	h := &desktopSurfaceHandler{d: d}

	// A focused trinket in the desktop's own content asks for the caret.
	d.SetContent(newCaretProbe(10, 20))

	surf.caretCalls = 0
	h.FrameBase(core.NewPainter(px))
	if surf.caretCalls != 0 {
		t.Errorf("FrameBase touched the surface caret %d times, want 0 — "+
			"the host applies the winner across all layers", surf.caretCalls)
	}

	// Frame, the non-compositing path, still owns it.
	surf.caretCalls = 0
	h.Frame(core.NewPainter(px))
	if surf.caretCalls == 0 {
		t.Error("Frame did not apply the caret; the single-surface present has no one else to do it")
	}
}
