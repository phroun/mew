package window

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

// SurfaceHost is G4's native mode: one Window as the entire content
// of one platform Surface. The OS (or host platform) provides the
// chrome - title bar, frame, move/resize - so the window suppresses
// its own and paints content edge to edge. In-surface mode (the
// WindowManager compositing many windows inside one surface) remains
// the other half of the dual mode; which one a window gets is host
// policy - see Window.NativeRequested.
type SurfaceHost struct {
	win     *Window
	surface platform.Surface
}

// NewSurfaceHost attaches the window to the surface and installs the
// handler. Call on the platform thread.
func NewSurfaceHost(win *Window, surface platform.Surface) *SurfaceHost {
	h := &SurfaceHost{win: win, surface: surface}

	// Native chrome lives outside: no KittyTK frame, no title bar,
	// geometry belongs to the platform.
	win.SetFlags(win.Flags() | WindowFlagFrameless | WindowFlagNoTitle | WindowFlagNoResize | WindowFlagNoMove)

	size := surface.Size()
	win.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	win.Layout()

	surface.SetHandler(h)
	surface.Invalidate(core.UnitRect{})
	return h
}

// Window returns the hosted window.
func (h *SurfaceHost) Window() *Window { return h.win }

// Invalidate requests a repaint of the hosted window.
func (h *SurfaceHost) Invalidate() {
	h.surface.Invalidate(core.UnitRect{})
}

// Frame implements platform.SurfaceHandler.
func (h *SurfaceHost) Frame(p *core.Painter) {
	h.win.Paint(p)
}

// Event implements platform.SurfaceHandler: surface coordinates ARE
// window coordinates (the window fills the surface at origin).
func (h *SurfaceHost) Event(ev core.Event) bool {
	var handled bool
	switch e := ev.(type) {
	case core.KeyPressEvent:
		handled = h.win.HandleKeyPress(e)
	case core.KeyReleaseEvent:
		handled = h.win.HandleKeyRelease(e)
	case core.MousePressEvent:
		handled = h.win.HandleMousePress(e)
	case core.MouseMoveEvent:
		handled = h.win.HandleMouseMove(e)
	case core.MouseReleaseEvent:
		handled = h.win.HandleMouseRelease(e)
	case core.MouseWheelEvent:
		handled = h.win.HandleMouseWheel(e)
	}
	// Parity contract: repaint after input until trinkets migrate to
	// precise invalidation.
	h.surface.Invalidate(core.UnitRect{})
	return handled
}

// Resized implements platform.SurfaceHandler: the window tracks the
// surface.
func (h *SurfaceHost) Resized(size core.UnitSize) {
	h.win.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
	h.win.Layout()
	h.surface.Invalidate(core.UnitRect{})
}
