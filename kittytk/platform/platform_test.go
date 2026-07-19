package platform

import (
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// fakeBackend is a minimal RenderBackend with an injectable event
// queue and frame counting.
type fakeBackend struct {
	mu     sync.Mutex
	events []core.Event
	frames int
}

func (f *fakeBackend) push(ev core.Event) {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
}

func (f *fakeBackend) Init() error { return nil }
func (f *fakeBackend) Shutdown()   {}
func (f *fakeBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (f *fakeBackend) Size() core.UnitSize { return core.UnitSize{Width: 640, Height: 320} }
func (f *fakeBackend) BeginFrame() {
	f.mu.Lock()
	f.frames++
	f.mu.Unlock()
}
func (f *fakeBackend) frameCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.frames
}
func (f *fakeBackend) EndFrame()                                            {}
func (f *fakeBackend) Clear(style.CellStyle)                                {}
func (f *fakeBackend) SetClip(core.UnitRect)                                {}
func (f *fakeBackend) DrawCell(core.Unit, core.Unit, rune, style.CellStyle) {}
func (f *fakeBackend) DrawText(x, y core.Unit, text string, s style.CellStyle, ft *core.Font) core.Unit {
	return 0
}
func (f *fakeBackend) DrawTextAligned(core.UnitRect, string, core.Alignment, core.Alignment, style.CellStyle, *core.Font) {
}
func (f *fakeBackend) FillRect(core.UnitRect, rune, style.CellStyle)                     {}
func (f *fakeBackend) DrawRect(core.UnitRect, style.BorderStyle, style.CellStyle)        {}
func (f *fakeBackend) DrawHLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (f *fakeBackend) DrawVLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (f *fakeBackend) DrawBox(core.UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (f *fakeBackend) PollEvent() core.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		return nil
	}
	ev := f.events[0]
	f.events = f.events[1:]
	return ev
}
func (f *fakeBackend) WaitEvent() core.Event                  { return nil }
func (f *fakeBackend) SetCursorVisible(bool)                  {}
func (f *fakeBackend) SetCursorPosition(core.Unit, core.Unit) {}
func (f *fakeBackend) SupportsColor() bool                    { return true }
func (f *fakeBackend) SupportsMouse() bool                    { return true }
func (f *fakeBackend) SupportsUnicode() bool                  { return true }
func (f *fakeBackend) ColorDepth() int                        { return 256 }
func (f *fakeBackend) GetClipboard() string                   { return "clip" }
func (f *fakeBackend) SetClipboard(string)                    {}
func (f *fakeBackend) Beep()                                  {}

// recordingHandler collects callbacks.
type recordingHandler struct {
	mu      sync.Mutex
	events  []core.Event
	frames  int
	resizes []core.UnitSize
}

func (h *recordingHandler) Frame(*core.Painter) {
	h.mu.Lock()
	h.frames++
	h.mu.Unlock()
}
func (h *recordingHandler) Event(ev core.Event) bool {
	h.mu.Lock()
	h.events = append(h.events, ev)
	h.mu.Unlock()
	return true
}
func (h *recordingHandler) Resized(s core.UnitSize) {
	h.mu.Lock()
	h.resizes = append(h.resizes, s)
	h.mu.Unlock()
}

func TestInvertedLoopContract(t *testing.T) {
	backend := &fakeBackend{}
	p := NewPolling(backend)
	handler := &recordingHandler{}

	var platformThreadPosts int
	done := make(chan int, 1)

	go func() {
		done <- p.Run(func(pf Platform) {
			s, err := pf.CreateSurface(SurfaceOptions{})
			if err != nil {
				t.Errorf("CreateSurface: %v", err)
				pf.Quit(1)
				return
			}
			s.SetHandler(handler)

			// A second surface is refused (one terminal).
			if _, err := pf.CreateSurface(SurfaceOptions{}); err == nil {
				t.Error("second surface should error")
			}

			// Damage-driven: invalidation triggers a frame.
			s.Invalidate(core.UnitRect{})

			// Input: events reach the handler; resize routes to
			// Resized, not Event.
			backend.push(core.KeyPressEvent{Key: "a"})
			backend.push(core.ResizeEvent{Width: 800, Height: 400})

			// Cross-thread door: Post from another goroutine lands
			// in the loop.
			go pf.Post(func() { platformThreadPosts++ })

			// Timers via PostAfter; the last one ends the run.
			pf.PostAfter(30*time.Millisecond, func() { platformThreadPosts++ })
			pf.PostAfter(80*time.Millisecond, func() { pf.Quit(42) })
		})
	}()

	select {
	case code := <-done:
		if code != 42 {
			t.Errorf("exit code = %d, want 42", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("inverted loop did not exit")
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.frames < 1 {
		t.Error("invalidation produced no frame")
	}
	if len(handler.events) != 1 {
		t.Fatalf("events = %v, want the one key press", handler.events)
	}
	if k, ok := handler.events[0].(core.KeyPressEvent); !ok || k.Key != "a" {
		t.Errorf("event = %+v", handler.events[0])
	}
	if len(handler.resizes) != 1 || handler.resizes[0].Width != 800 {
		t.Errorf("resizes = %v", handler.resizes)
	}
	if platformThreadPosts != 2 {
		t.Errorf("posts executed = %d, want 2", platformThreadPosts)
	}
	if backend.frameCount() < 1 {
		t.Error("backend saw no BeginFrame")
	}
}
