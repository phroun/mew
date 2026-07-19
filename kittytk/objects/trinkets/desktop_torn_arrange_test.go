package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// fakeNativeSurface is a minimal platform.NativeSurface for exercising the
// torn-window Tile/Cascade arrangement.
type fakeNativeSurface struct {
	size      core.UnitSize
	pxW, pxH  int
	handler   platform.SurfaceHandler
	x, y      int
	minimized bool
}

func (s *fakeNativeSurface) Size() core.UnitSize                  { return s.size }
func (s *fakeNativeSurface) Metrics() core.CellMetrics            { return core.DefaultCellMetrics() }
func (s *fakeNativeSurface) SetHandler(h platform.SurfaceHandler) { s.handler = h }
func (s *fakeNativeSurface) Invalidate(core.UnitRect)             {}
func (s *fakeNativeSurface) SetCursorVisible(bool)                {}
func (s *fakeNativeSurface) SetCursorPosition(x, y core.Unit)     {}
func (s *fakeNativeSurface) ScreenPositionPx() (int, int)         { return s.x, s.y }
func (s *fakeNativeSurface) SetScreenPositionPx(x, y int)         { s.x, s.y = x, y }
func (s *fakeNativeSurface) ScreenSizePx() (int, int)             { return s.pxW, s.pxH }
func (s *fakeNativeSurface) SetScreenSizePx(w, h int)             { s.pxW, s.pxH = w, h }
func (s *fakeNativeSurface) WorkAreaPx() (int, int, int, int)     { return 0, 30, 1600, 970 }
func (s *fakeNativeSurface) Close()                               {}
func (s *fakeNativeSurface) SetOpacity(float64)                   {}
func (s *fakeNativeSurface) Raise()                               {}
func (s *fakeNativeSurface) Minimized() bool                      { return s.minimized }
func (s *fakeNativeSurface) Minimize()                            { s.minimized = true }
func (s *fakeNativeSurface) Restore()                             { s.minimized = false }

// Tiling torn app windows lays them out across the work area with the main
// window in the upper-left cell.
func TestArrangeTornAppWindowsTilePlacesMainUpperLeft(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	mk := func(title string) (*window.Window, *fakeNativeSurface) {
		w := window.NewWindow(title)
		s := &fakeNativeSurface{pxW: 400, pxH: 300}
		h := window.NewTearOffHost(w, s, 1, func() (int, int) { return 0, 0 }, nil)
		d.tornHosts = append(d.tornHosts, h)
		return w, s
	}
	main, ms := mk("Main")
	_, s2 := mk("Two")
	app := &mockApp{name: "Demo", main: main, windows: []*window.Window{main}}
	// The second window is another of the app's torn windows.
	app.windows = append(app.windows, d.tornHosts[1].Window())

	d.arrangeTornAppWindows(app, false)

	// Work area origin is (0,30); the main window fills the first cell there.
	if ms.x != 0 || ms.y != 30 {
		t.Errorf("main at (%d,%d), want upper-left (0,30)", ms.x, ms.y)
	}
	// Two windows -> 2 columns; the second sits in the next cell over.
	if s2.x == ms.x && s2.y == ms.y {
		t.Errorf("second window overlaps the main; want a distinct cell")
	}
	if ms.pxW <= 0 || ms.pxH <= 0 {
		t.Errorf("main not resized: %dx%d", ms.pxW, ms.pxH)
	}
}

// A NoResize torn window keeps its own size during tile/cascade; it is only
// repositioned.
func TestArrangeTornAppWindowsHonorsNoResize(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	fixed := window.NewWindow("Fixed")
	fixed.SetFlags(window.WindowFlagNoResize)
	fs := &fakeNativeSurface{pxW: 250, pxH: 180}
	fh := window.NewTearOffHost(fixed, fs, 1, func() (int, int) { return 0, 0 }, nil)
	d.tornHosts = append(d.tornHosts, fh)

	app := &mockApp{name: "Demo", main: fixed, windows: []*window.Window{fixed}}
	d.arrangeTornAppWindows(app, false)

	if fs.pxW != 250 || fs.pxH != 180 {
		t.Errorf("NoResize window was resized to %dx%d, want 250x180", fs.pxW, fs.pxH)
	}
	// Still repositioned to the work-area origin.
	if fs.x != 0 || fs.y != 30 {
		t.Errorf("NoResize window not repositioned: (%d,%d)", fs.x, fs.y)
	}
}

// Hide OS-minimizes a torn window; Show All un-minimizes it.
func TestHideShowTornWindows(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	w := window.NewWindow("Torn")
	s := &fakeNativeSurface{pxW: 400, pxH: 300}
	h := window.NewTearOffHost(w, s, 1, func() (int, int) { return 0, 0 }, nil)
	d.tornHosts = append(d.tornHosts, h)

	app := &mockApp{name: "Demo", main: w, windows: []*window.Window{w}}
	d.applications = append(d.applications, app)
	d.activeApp = app

	d.hideActiveApp()
	if !s.minimized {
		t.Error("Hide should OS-minimize the torn window")
	}
	d.showAllApps()
	if s.minimized {
		t.Error("Show All should un-minimize the torn window")
	}
}
