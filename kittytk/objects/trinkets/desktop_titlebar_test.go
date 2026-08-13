package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// titlebarTestDesktop is the msPlatform harness the frame-mode tests share:
// a graphical desktop run on the fake native platform, with the frame mode
// set before RunOn (the way a host sets it from [window] desktop_frame).
func titlebarTestDesktop(t *testing.T, mode string, script func(d *Desktop, plat *msPlatform)) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)
	if mode != "" {
		d.SetDesktopFrame(mode)
	}
	d.SetTitle("Frame Test")

	plat := &msPlatform{}
	plat.script = func() {
		script(d, plat)
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// The themed frame (the default): the OS chrome is stripped at startup and
// the desktop carries its own title bar row — one cell, above the menu bar
// — so the client area starts two rows down and the menu bar's Bounds sit
// on the second row.
func TestThemedFrameStripsChromeAndCarriesATitleBar(t *testing.T) {
	titlebarTestDesktop(t, "", func(d *Desktop, plat *msPlatform) {
		surf := plat.surfaces[0]
		if surf.bordered {
			t.Error("themed frame left the OS chrome on")
		}
		cell := d.EffectiveCellMetrics().CellHeight
		if got := d.TitleBarHeight(); got != cell {
			t.Errorf("TitleBarHeight = %v, want one cell (%v)", got, cell)
		}
		if got := d.ClientArea().Y; got != 2*cell {
			t.Errorf("ClientArea.Y = %v, want title bar + menu bar (%v)", got, 2*cell)
		}
		if got := d.menuBar.Bounds().Y; got != cell {
			t.Errorf("menu bar Bounds.Y = %v, want below the title bar (%v)", got, cell)
		}
		// The window verbs the OS bar carried are on the Ψ menu now.
		var found []string
		for _, it := range d.systemMenu.Items() {
			found = append(found, it.Text)
		}
		joined := strings.Join(found, "|")
		if !strings.Contains(joined, "Minimize") || !strings.Contains(joined, "Zoom") {
			t.Errorf("system menu lacks Minimize/Zoom: %q", joined)
		}
	})
}

// Dragging the themed title bar moves the OS window the way a torn
// window's title bar does: global pointer deltas onto its pixel origin.
// The press does NOT reach the menu bar (its row is one further down).
func TestThemedTitleBarDragMovesTheHostWindow(t *testing.T) {
	titlebarTestDesktop(t, "", func(d *Desktop, plat *msPlatform) {
		surf := plat.surfaces[0]
		h := surf.handler

		// Press mid-bar (below the 3-unit resize sliver, above the menu row).
		plat.gx, plat.gy = 450, 68
		h.Event(core.MousePressEvent{X: 400, Y: 8, Button: core.LeftButton})
		if d.menuBar.ActiveMenu() != nil {
			t.Fatal("title-bar press opened a menu")
		}
		plat.gx, plat.gy = 480, 88 // +30, +20
		h.Event(core.MouseMoveEvent{X: 400, Y: 8, Buttons: core.LeftButton})
		if surf.x != 80 || surf.y != 80 {
			t.Errorf("drag moved the window to (%d,%d), want (80,80)", surf.x, surf.y)
		}
		h.Event(core.MouseReleaseEvent{X: 400, Y: 8, Button: core.LeftButton})

		// Disarmed: further movement moves nothing.
		plat.gx = 600
		h.Event(core.MouseMoveEvent{X: 400, Y: 8})
		if surf.x != 80 {
			t.Errorf("released drag kept moving the window: x=%d, want 80", surf.x)
		}
	})
}

// The menu bar still works one row down: a press on its row (translated at
// the desktop boundary — the bar itself is origin-local) opens a menu, and
// the reported dropdown bounds are in desktop coordinates, below the bar.
func TestThemedFrameTranslatesTheMenuBar(t *testing.T) {
	titlebarTestDesktop(t, "", func(d *Desktop, plat *msPlatform) {
		surf := plat.surfaces[0]
		h := surf.handler
		cell := d.EffectiveCellMetrics().CellHeight

		// Press on the Ψ title, second row (Y just past the title bar).
		h.Event(core.MousePressEvent{X: 4, Y: cell + 4, Button: core.LeftButton})
		if d.menuBar.ActiveMenu() == nil {
			t.Fatal("menu-bar press one row down did not open a menu")
		}
		if got := d.ActiveMenuBounds().Y; got != 2*cell {
			t.Errorf("dropdown top = %v, want below title bar + menu bar (%v)", got, 2*cell)
		}
		h.Event(core.MouseReleaseEvent{X: 4, Y: cell + 4, Button: core.LeftButton})
	})
}

// native_titlebar keeps the OS chrome and the in-client resize zones (the
// pre-knob behavior): no themed title row, but the desktop's own edges
// still answer.
func TestNativeTitlebarKeepsChromeAndZones(t *testing.T) {
	titlebarTestDesktop(t, DesktopFrameNativeTitlebar, func(d *Desktop, plat *msPlatform) {
		surf := plat.surfaces[0]
		if !surf.bordered {
			t.Error("native_titlebar stripped the OS chrome")
		}
		if got := d.TitleBarHeight(); got != 0 {
			t.Errorf("TitleBarHeight = %v, want 0", got)
		}
		surf.handler.Event(core.MouseMoveEvent{X: 798, Y: 240})
		if edges := d.hostHoverEdges(); edges != window.ResizeEdgeRight {
			t.Errorf("right-edge hover = %d, want right", edges)
		}
	})
}

// native stands the desktop's own zones down entirely: the OS chrome is
// the whole story.
func TestNativeFrameStandsDownEntirely(t *testing.T) {
	titlebarTestDesktop(t, DesktopFrameNative, func(d *Desktop, plat *msPlatform) {
		surf := plat.surfaces[0]
		if !surf.bordered {
			t.Error("native mode stripped the OS chrome")
		}
		surf.handler.Event(core.MouseMoveEvent{X: 798, Y: 240})
		if edges := d.hostHoverEdges(); edges != 0 {
			t.Errorf("native mode answered %d at the edge, want 0", edges)
		}
		if edges := d.hostEdgeAt(798, 240); edges != 0 {
			t.Errorf("hostEdgeAt answered %d in native mode, want 0", edges)
		}
	})
}

// The Ψ menu's Zoom fills the work area and zooming again restores the
// exact prior rectangle; it is a plain move+resize, so the desktop's own
// edge zones stay live while zoomed.
func TestZoomToggleFillsTheWorkAreaAndRestores(t *testing.T) {
	titlebarTestDesktop(t, "", func(d *Desktop, plat *msPlatform) {
		surf := plat.surfaces[0]

		d.hostZoomToggle()
		if surf.x != 0 || surf.y != 0 {
			t.Errorf("zoomed origin (%d,%d), want the work area's (0,0)", surf.x, surf.y)
		}
		if w, h := surf.ScreenSizePx(); w != 1600 || h != 1000 {
			t.Errorf("zoomed size %dx%d, want the work area's 1600x1000", w, h)
		}
		surf.handler.Event(core.MouseMoveEvent{X: 1598, Y: 500})
		if edges := d.hostHoverEdges(); edges != window.ResizeEdgeRight {
			t.Errorf("edge zones stood down while zoomed: %d, want right", edges)
		}

		d.hostZoomToggle()
		if surf.x != 50 || surf.y != 60 {
			t.Errorf("restored origin (%d,%d), want (50,60)", surf.x, surf.y)
		}
		if w, h := surf.ScreenSizePx(); w != 800 || h != 480 {
			t.Errorf("restored size %dx%d, want 800x480", w, h)
		}
	})
}

// Under the themed default the OS chrome strip is permanent: solo mode
// strips nothing new, and exiting solo must NOT restore an OS border the
// desktop never wants. The title bar row stands down while the app owns
// the surface and returns with the desktop.
func TestThemedFrameStaysBorderlessAcrossSolo(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)
	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		primary := plat.surfaces[0]
		if primary.bordered {
			t.Fatal("themed frame did not strip the chrome at startup")
		}
		d.EnterSoloMode(main)
		if primary.bordered {
			t.Error("solo mode re-bordered a themed surface")
		}
		if d.TitleBarHeight() != 0 {
			t.Error("themed title bar row still reserved in solo mode")
		}
		d.ExitSoloMode()
		if primary.bordered {
			t.Error("solo exit restored the OS chrome on a themed desktop")
		}
		if d.TitleBarHeight() == 0 {
			t.Error("themed title bar did not return with the desktop")
		}
		d.QuitWithCode(0)
	}
	d.RunOn(plat)
}

// Minimize on the Ψ menu miniaturizes the OS window.
func TestHostMinimizeMiniaturizes(t *testing.T) {
	titlebarTestDesktop(t, "", func(d *Desktop, plat *msPlatform) {
		surf := plat.surfaces[0]
		d.hostMinimize()
		if !surf.minimized {
			t.Error("hostMinimize did not minimize the OS window")
		}
	})
}
