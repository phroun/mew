//go:build sdl

package sdl

import (
	"os"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
)

type recordingHandler struct {
	frames  int
	resizes int
}

func (h *recordingHandler) Frame(*core.Painter)   { h.frames++ }
func (h *recordingHandler) Event(core.Event) bool { return true }
func (h *recordingHandler) Resized(core.UnitSize) { h.resizes++ }

// The SDL loop runs headlessly under the dummy video driver: window,
// renderer, texture, frame paint into the raster framebuffer, posts,
// timers, clean exit.
func TestSDLPlatformHeadless(t *testing.T) {
	os.Setenv("SDL_VIDEODRIVER", "dummy")

	p := New("test", 320, 200)
	handler := &recordingHandler{}
	posts := 0

	done := make(chan int, 1)
	go func() {
		done <- p.Run(func(pf platform.Platform) {
			s, err := pf.CreateSurface(platform.SurfaceOptions{})
			if err != nil {
				t.Errorf("CreateSurface: %v", err)
				pf.Quit(1)
				return
			}
			s.SetHandler(handler)
			s.Invalidate(core.UnitRect{})

			go pf.Post(func() { posts++ })
			pf.PostAfter(150*time.Millisecond, func() { pf.Quit(7) })
		})
	}()

	select {
	case code := <-done:
		if code != 7 {
			t.Errorf("exit code = %d, want 7", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SDL loop did not exit")
	}
	if handler.frames < 1 {
		t.Error("no frames painted")
	}
	if posts != 1 {
		t.Errorf("posts = %d", posts)
	}
	// The framebuffer really exists and was painted through.
	if p.Backend() == nil || p.Backend().Image() == nil {
		t.Error("no framebuffer")
	}
}
