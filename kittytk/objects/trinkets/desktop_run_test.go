package trinkets

import (
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// nullBackend: headless RenderBackend for exercising the inverted
// loop end to end.
type nullBackend struct {
	mu     sync.Mutex
	events []core.Event
	frames int
}

func (n *nullBackend) push(ev core.Event) {
	n.mu.Lock()
	n.events = append(n.events, ev)
	n.mu.Unlock()
}
func (n *nullBackend) frameCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.frames
}

func (n *nullBackend) Init() error { return nil }
func (n *nullBackend) Shutdown()   {}
func (n *nullBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (n *nullBackend) Size() core.UnitSize { return core.UnitSize{Width: 8 * 80, Height: 16 * 24} }
func (n *nullBackend) BeginFrame() {
	n.mu.Lock()
	n.frames++
	n.mu.Unlock()
}
func (n *nullBackend) EndFrame()                                            {}
func (n *nullBackend) Clear(style.CellStyle)                                {}
func (n *nullBackend) SetClip(core.UnitRect)                                {}
func (n *nullBackend) DrawCell(core.Unit, core.Unit, rune, style.CellStyle) {}
func (n *nullBackend) DrawText(x, y core.Unit, text string, s style.CellStyle, f *core.Font) core.Unit {
	return 0
}
func (n *nullBackend) DrawTextAligned(core.UnitRect, string, core.Alignment, core.Alignment, style.CellStyle, *core.Font) {
}
func (n *nullBackend) FillRect(core.UnitRect, rune, style.CellStyle)                     {}
func (n *nullBackend) DrawRect(core.UnitRect, style.BorderStyle, style.CellStyle)        {}
func (n *nullBackend) DrawHLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (n *nullBackend) DrawVLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (n *nullBackend) DrawBox(core.UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (n *nullBackend) PollEvent() core.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.events) == 0 {
		return nil
	}
	ev := n.events[0]
	n.events = n.events[1:]
	return ev
}
func (n *nullBackend) WaitEvent() core.Event                  { return nil }
func (n *nullBackend) SetCursorVisible(bool)                  {}
func (n *nullBackend) SetCursorPosition(core.Unit, core.Unit) {}
func (n *nullBackend) SupportsColor() bool                    { return true }
func (n *nullBackend) SupportsMouse() bool                    { return true }
func (n *nullBackend) SupportsUnicode() bool                  { return true }
func (n *nullBackend) ColorDepth() int                        { return 256 }
func (n *nullBackend) GetClipboard() string                   { return "" }
func (n *nullBackend) SetClipboard(string)                    {}
func (n *nullBackend) Beep()                                  {}

// The desktop runs end to end under the inverted loop: startup fires,
// frames paint, events dispatch, a desktop timer fires through
// PostAfter self-ticking, and QuitEvent exits cleanly.
func TestDesktopRunsOnInvertedLoop(t *testing.T) {
	backend := &nullBackend{}
	d := NewDesktop()
	d.SetBackend(backend)

	timerFired := make(chan struct{}, 1)
	started := false
	d.SetOnStartup(func() {
		started = true
		d.StartTimer(20*time.Millisecond, func() {
			select {
			case timerFired <- struct{}{}:
			default:
			}
			// Exercise event dispatch, then quit through the event
			// path (QuitEvent -> dispatchEvent -> QuitWithCode).
			backend.push(core.KeyPressEvent{Key: "a"})
			backend.push(core.QuitEvent{})
		})
	})

	shutdown := false
	d.SetOnShutdown(func() { shutdown = true })

	done := make(chan int, 1)
	go func() { done <- d.Run() }()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("desktop did not exit")
	}

	if !started {
		t.Error("onStartup did not run")
	}
	if !shutdown {
		t.Error("onShutdown did not run")
	}
	select {
	case <-timerFired:
	default:
		t.Error("desktop timer did not fire under the inverted loop")
	}
	if backend.frameCount() < 1 {
		t.Error("no frames painted")
	}
	if d.IsRunning() {
		t.Error("desktop still marked running")
	}
}
