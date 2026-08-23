//go:build sdl

package sdl

import (
	"os"
	"testing"
	"time"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
	"github.com/phroun/kittytk/style"
)

// fillHandler paints its whole surface a solid color every frame, so a
// present that skips any region leaves zero-alpha pixels behind.
type fillHandler struct {
	size    core.UnitSize
	frames  int
	resizes int
}

func (h *fillHandler) Frame(p *core.Painter) {
	h.frames++
	p.Clear(core.UnitRect{Width: h.size.Width, Height: h.size.Height},
		style.CellStyle{Fg: style.ColorDefault, Bg: style.RGB(200, 40, 160)})
}
func (h *fillHandler) Event(core.Event) bool { return true }
func (h *fillHandler) Resized(sz core.UnitSize) {
	h.resizes++
	h.size = sz
}

// The software renderer's streaming texture must use SDL's ABGR8888:
// that is the packed format whose little-endian memory layout matches
// image.RGBA's R,G,B,A bytes, which Present uploads verbatim. Any other
// format swaps channels across the whole UI (ARGB8888 turned every blue
// into orange).
func TestSoftwareTextureMatchesBackendByteOrder(t *testing.T) {
	requireSDL(t)
	os.Setenv("SDL_VIDEODRIVER", "dummy")

	p := newTestPlatform(t)
	handler := &fillHandler{}
	var format uint32

	done := make(chan int, 1)
	go func() {
		done <- p.Run(func(pf platform.Platform) {
			s, err := pf.CreateSurface(platform.SurfaceOptions{})
			if err != nil {
				t.Errorf("CreateSurface: %v", err)
				pf.Quit(1)
				return
			}
			handler.size = s.Size()
			s.SetHandler(handler)
			s.Invalidate(core.UnitRect{})

			pf.PostAfter(50*time.Millisecond, func() {
				if p.main != nil && p.main.texture != nil {
					format = p.main.texture.Format()
				}
				pf.Quit(7)
			})
		})
	}()

	select {
	case code := <-done:
		if code != 7 {
			t.Fatalf("exit code = %d, want 7", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SDL loop did not exit")
	}

	if format == 0 {
		t.Fatal("no software texture was created")
	}
	if format != uint32(sdl3.PIXELFORMAT_ABGR8888) {
		t.Errorf("texture format = %#x, want ABGR8888 (%#x) to match image.RGBA byte order",
			format, uint32(sdl3.PIXELFORMAT_ABGR8888))
	}
}

// A live resize must leave the window FULLY painted: the framebuffer is
// recreated from zeroed memory, so unless the resize path invalidates the
// surface and repaints everything, part of the window presents as black.
// This is the headless regression for the "black triangle after resize"
// bug (and for resize losing content in general).
func TestSDLLiveResizeRepaintsFully(t *testing.T) {
	requireSDL(t)
	os.Setenv("SDL_VIDEODRIVER", "dummy")

	p := newTestPlatform(t)
	handler := &fillHandler{}

	done := make(chan int, 1)
	go func() {
		done <- p.Run(func(pf platform.Platform) {
			s, err := pf.CreateSurface(platform.SurfaceOptions{})
			if err != nil {
				t.Errorf("CreateSurface: %v", err)
				pf.Quit(1)
				return
			}
			handler.size = s.Size()
			s.SetHandler(handler)
			s.Invalidate(core.UnitRect{})

			// Let a first frame land, then grow the window; the SIZE_CHANGED
			// event drives liveResize exactly like a user drag.
			pf.PostAfter(50*time.Millisecond, func() {
				if p.main != nil && p.main.window != nil {
					p.main.window.SetSize(240, 160)
				}
			})
			pf.PostAfter(300*time.Millisecond, func() { pf.Quit(7) })
		})
	}()

	select {
	case code := <-done:
		if code != 7 {
			t.Fatalf("exit code = %d, want 7", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SDL loop did not exit")
	}

	if handler.resizes < 1 {
		t.Error("handler.Resized never called for the window resize")
	}

	img := p.Backend().Image()
	if img == nil {
		t.Fatal("no framebuffer after resize")
	}
	b := img.Bounds()
	if b.Dx() != 240 || b.Dy() != 160 {
		t.Fatalf("framebuffer = %dx%d, want 240x160", b.Dx(), b.Dy())
	}

	// Every pixel must have been painted: the fresh backend starts
	// zero-filled, and the fill paints alpha 255 everywhere. The
	// bottom-right corner is precisely where the black-triangle bug left
	// unpainted memory.
	for _, pt := range [][2]int{
		{0, 0}, {b.Dx() - 1, 0}, {0, b.Dy() - 1},
		{b.Dx() - 1, b.Dy() - 1}, {b.Dx() / 2, b.Dy() / 2},
		{b.Dx() * 3 / 4, b.Dy() * 3 / 4},
	} {
		i := img.PixOffset(pt[0], pt[1])
		if img.Pix[i+3] == 0 {
			t.Errorf("pixel (%d,%d) never painted after resize (alpha 0)", pt[0], pt[1])
		}
	}
}
