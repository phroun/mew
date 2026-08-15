//go:build sdl

package sdl

import (
	"fmt"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/platform"
)

// SoftwareRenderer implements CPU-based rasterization with SDL's
// renderer and texture presentation. This is the traditional path:
// trinkets paint to a shared raster.Backend, then SDL blits it to screen.
type SoftwareRenderer struct {
	vsync bool
}

// NewSoftwareRenderer creates a CPU-based renderer
func NewSoftwareRenderer(vsync bool) *SoftwareRenderer {
	return &SoftwareRenderer{vsync: vsync}
}

// Initialize sets up the software renderer (no-op, SDL handles initialization)
func (r *SoftwareRenderer) Initialize() error {
	return nil
}

// Shutdown cleans up software renderer resources (no-op)
func (r *SoftwareRenderer) Shutdown() {
}

// CreateWindowRenderer creates SDL renderer and texture for a window
func (r *SoftwareRenderer) CreateWindowRenderer(w *nativeWin, pxW, pxH int) error {
	// Empty driver name lets SDL pick the best available; falling back
	// to the software driver covers video drivers with no acceleration
	// (headless dummy, bare VMs).
	renderer, err := sdl3.CreateRenderer(w.window, "", r.vsync)
	if err != nil {
		renderer, err = sdl3.CreateRenderer(w.window, "software", false)
		if err != nil {
			return err
		}
	}
	w.renderer = renderer

	// Create streaming texture matching the backend's pixel layout:
	// Go's image.RGBA stores bytes R,G,B,A, which on little-endian is
	// SDL's packed ABGR8888. ARGB8888 here reads as B,G,R,A and swaps
	// red and blue across the whole UI.
	texture, err := renderer.CreateTexture(
		sdl3.PIXELFORMAT_ABGR8888,
		sdl3.TEXTUREACCESS_STREAMING,
		int32(pxW),
		int32(pxH),
	)
	if err != nil {
		renderer.Destroy()
		w.renderer = nil
		return err
	}
	w.texture = texture

	return nil
}

// DestroyWindowRenderer cleans up SDL renderer and texture
func (r *SoftwareRenderer) DestroyWindowRenderer(w *nativeWin) {
	if w.texture != nil {
		w.texture.Destroy()
		w.texture = nil
	}
	if w.renderer != nil {
		w.renderer.Destroy()
		w.renderer = nil
	}
}

// ResizeWindowRenderer recreates texture at new size
func (r *SoftwareRenderer) ResizeWindowRenderer(w *nativeWin, pxW, pxH int) error {
	if w.renderer == nil {
		// Window creation sizes the framebuffer before CreateWindowRenderer
		// has built the SDL renderer; that first texture is created at the
		// right size already.
		return nil
	}
	if w.texture != nil {
		w.texture.Destroy()
	}

	// Same byte-order contract as CreateWindowRenderer: image.RGBA is
	// SDL ABGR8888 on little-endian.
	texture, err := w.renderer.CreateTexture(
		sdl3.PIXELFORMAT_ABGR8888,
		sdl3.TEXTUREACCESS_STREAMING,
		int32(pxW),
		int32(pxH),
	)
	if err != nil {
		w.texture = nil
		return err
	}
	w.texture = texture
	return nil
}

// Present copies backend pixels to SDL texture and displays it
func (r *SoftwareRenderer) Present(w *nativeWin, backend *raster.Backend) error {
	if w.texture == nil || w.renderer == nil {
		return nil // Window not ready
	}

	// Re-upload only what was repainted. The streaming texture keeps its
	// last contents otherwise, so a frame that changed nothing — every
	// frame of a window drag, where the OS moves the window and the
	// picture is identical — costs a copy to screen and nothing more.
	if w.pixelsDirty {
		img := backend.Image()
		_ = w.texture.Update(nil, img.Pix, img.Stride)
		w.pixelsDirty = false
	}

	// Render to screen
	if w.transparent {
		// Alpha-0 clear so the window's cleared corners composite as
		// nothing rather than as black.
		_ = w.renderer.SetDrawColor(0, 0, 0, 0)
	}
	w.renderer.Clear()
	w.renderer.Copy(w.texture, nil, nil)
	w.renderer.Present()

	return nil
}

// RenderFrame is not used by software renderer (uses Present directly)
func (r *SoftwareRenderer) RenderFrame(w *nativeWin, windows []*nativeWin, renderWindow func(*nativeWin)) error {
	// Software renderer doesn't do compositing - just calls Present per window
	return nil
}

// ApplyWindowShape is a no-op: rounded corners come from the
// framebuffer's own cleared pixels (see punchRoundedCorners), not from
// a window-system shape.
func (r *SoftwareRenderer) ApplyWindowShape(w *nativeWin, radiusPx int, transparent bool) error {
	return nil
}

// SetRotationEnabled is a no-op for software renderer
func (r *SoftwareRenderer) SetRotationEnabled(enabled bool) {
	// Software renderer doesn't support rotation effects
}

// SetWindowTransform is a no-op for software renderer
func (r *SoftwareRenderer) SetWindowTransform(windowID uint32, translateX, translateY, rotation, scaleX, scaleY, opacity float32) {
	// Software renderer doesn't support per-window transforms
}

// SupportsFeature checks software renderer capabilities
func (r *SoftwareRenderer) SupportsFeature(feature RendererFeature) bool {
	// Software renderer doesn't support GPU features
	return false
}

// RenderFrameWithChildWindows is not implemented for software renderer.
// Software renderer uses the old direct rendering path.
func (r *SoftwareRenderer) RenderFrameWithChildWindows(w *nativeWin, childWindows *platform.ChildWindowList, scale int, renderWindow func(*nativeWin)) error {
	return fmt.Errorf("child window compositing not supported by software renderer")
}
