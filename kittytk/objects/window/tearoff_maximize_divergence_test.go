package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The solo primary window is an OS-RESIZABLE window, so the window manager
// serves its edges itself and a drag on one never reaches edgeAt (the check
// that keeps a zoomed torn window from being resized at all). A window shrunk
// that way must give up its maximized state and adopt the size it actually
// has: left maximized it paints the maximized frame — title bar only, no
// border stroke, a restore button that would teleport it — around an
// arbitrary rect, with the surface past the content never repainted.
func TestTearOffHostOSResizeClearsMaximized(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 400, Height: 300}, x: 100, y: 100}
	win := NewWindow("solo")
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	h.ToggleZoom()
	if !win.IsMaximized() {
		t.Fatal("ToggleZoom did not maximize the window")
	}
	if surf.size.Width != 1600 || surf.size.Height != 970 {
		t.Fatalf("zoom filled %v, want the 1600x970 work area", surf.size)
	}

	// The window manager resizes the OS window out from under us.
	surf.SetScreenSizePx(900, 600)

	if win.IsMaximized() {
		t.Error("window still maximized after the OS resized it below the work area")
	}
	if b := win.Bounds(); b.Width != 900 || b.Height != 600 {
		t.Errorf("bounds %dx%d, want the size it actually has (900x600)", b.Width, b.Height)
	}

	// The host's zoom state cleared with it, so the maximize button zooms
	// again rather than "restoring" a window that is already restored.
	h.ToggleZoom()
	if !win.IsMaximized() || surf.size.Width != 1600 {
		t.Errorf("re-zoom after the heal left %v maximized=%v, want a filled work area",
			surf.size, win.IsMaximized())
	}
}

// A resize that still fills the work area is the zoom itself landing, not a
// user shrink, so the maximized state must survive it.
func TestTearOffHostZoomKeepsMaximized(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 400, Height: 300}}
	win := NewWindow("solo")
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	h.ToggleZoom()
	if !win.IsMaximized() {
		t.Fatal("zoom-to-fill did not leave the window maximized")
	}

	// A repeat of the same fill (a stray WINDOW_RESIZED, say) changes nothing.
	surf.SetScreenSizePx(1600, 970)
	if !win.IsMaximized() {
		t.Error("a resize that still fills the work area cleared the maximized state")
	}
}

// RestoreInPlace clears the maximized state without moving the window: the
// rect it has becomes the rect it keeps (Restore would snap back to the saved
// pre-maximize bounds, undoing a resize the user just performed).
func TestRestoreInPlaceKeepsBounds(t *testing.T) {
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 10, Y: 10, Width: 200, Height: 100})
	win.Maximize()
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 1600, Height: 970})

	win.RestoreInPlace()

	if win.IsMaximized() {
		t.Error("RestoreInPlace left the window maximized")
	}
	if b := win.Bounds(); b.Width != 1600 || b.Height != 970 {
		t.Errorf("bounds %dx%d, want the 1600x970 it already had", b.Width, b.Height)
	}
	// Restore() is now a no-op (already normal) rather than a teleport back to
	// the pre-maximize 200x100.
	win.Restore()
	if b := win.Bounds(); b.Width != 1600 {
		t.Errorf("a later Restore teleported to %dx%d", b.Width, b.Height)
	}
}
