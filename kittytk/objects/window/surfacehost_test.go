package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/platform"
	"github.com/phroun/kittytk/style"
)

// fakeSurface records handler installation and invalidations.
type fakeSurface struct {
	size        core.UnitSize
	handler     platform.SurfaceHandler
	invalidated int
}

func (s *fakeSurface) Size() core.UnitSize { return s.size }
func (s *fakeSurface) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (s *fakeSurface) SetHandler(h platform.SurfaceHandler)   { s.handler = h }
func (s *fakeSurface) Invalidate(core.UnitRect)               { s.invalidated++ }
func (s *fakeSurface) SetCursorVisible(bool)                  {}
func (s *fakeSurface) SetCursorPosition(core.Unit, core.Unit) {}

// clickTrinket counts presses and records its laid-out bounds.
type clickTrinket struct {
	core.TrinketBase
	clicks int
	paints int
}

func newClickTrinket() *clickTrinket {
	w := &clickTrinket{}
	w.TrinketBase = *core.NewTrinketBase()
	w.Init(w)
	w.SetFocusPolicy(core.StrongFocus)
	return w
}

func (w *clickTrinket) Paint(*core.Painter) { w.paints++ }
func (w *clickTrinket) HandleMousePress(core.MousePressEvent) bool {
	w.clicks++
	return true
}
func (w *clickTrinket) SizeHint() core.UnitSize {
	return core.UnitSize{Width: 80, Height: 32}
}

// nullPaintBackend: just enough backend for a Painter.
type nullPaintBackend struct{}

func (nullPaintBackend) Init() error { return nil }
func (nullPaintBackend) Shutdown()   {}
func (nullPaintBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (nullPaintBackend) Size() core.UnitSize                                  { return core.UnitSize{Width: 640, Height: 320} }
func (nullPaintBackend) BeginFrame()                                          {}
func (nullPaintBackend) EndFrame()                                            {}
func (nullPaintBackend) Clear(style.CellStyle)                                {}
func (nullPaintBackend) SetClip(core.UnitRect)                                {}
func (nullPaintBackend) DrawCell(core.Unit, core.Unit, rune, style.CellStyle) {}
func (nullPaintBackend) DrawText(x, y core.Unit, t string, s style.CellStyle, f *core.Font) core.Unit {
	return 0
}
func (nullPaintBackend) DrawTextAligned(core.UnitRect, string, core.Alignment, core.Alignment, style.CellStyle, *core.Font) {
}
func (nullPaintBackend) FillRect(core.UnitRect, rune, style.CellStyle)                     {}
func (nullPaintBackend) DrawRect(core.UnitRect, style.BorderStyle, style.CellStyle)        {}
func (nullPaintBackend) DrawHLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (nullPaintBackend) DrawVLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (nullPaintBackend) DrawBox(core.UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (nullPaintBackend) PollEvent() core.Event                                             { return nil }
func (nullPaintBackend) WaitEvent() core.Event                                             { return nil }
func (nullPaintBackend) SetCursorVisible(bool)                                             {}
func (nullPaintBackend) SetCursorPosition(core.Unit, core.Unit)                            {}
func (nullPaintBackend) SupportsColor() bool                                               { return true }
func (nullPaintBackend) SupportsMouse() bool                                               { return true }
func (nullPaintBackend) SupportsUnicode() bool                                             { return true }
func (nullPaintBackend) ColorDepth() int                                                   { return 256 }
func (nullPaintBackend) GetClipboard() string                                              { return "" }
func (nullPaintBackend) SetClipboard(string)                                               {}
func (nullPaintBackend) Beep()                                                             {}

func TestSurfaceHostRunsWindowAsNativeSurface(t *testing.T) {
	win := NewWindow("Hosted")
	content := newClickTrinket()
	win.SetContent(content)

	surface := &fakeSurface{size: core.UnitSize{Width: 8 * 50, Height: 16 * 12}}
	host := NewSurfaceHost(win, surface)

	// The window fills the surface, chrome suppressed (OS provides it).
	if b := win.Bounds(); b.Width != 400 || b.Height != 192 || b.X != 0 || b.Y != 0 {
		t.Errorf("window bounds = %+v", b)
	}
	for _, f := range []WindowFlags{WindowFlagFrameless, WindowFlagNoTitle, WindowFlagNoResize, WindowFlagNoMove} {
		if win.Flags()&f == 0 {
			t.Errorf("missing flag %v", f)
		}
	}
	if surface.handler == nil {
		t.Fatal("handler not installed")
	}

	// Content received layout (frameless: full surface interior).
	if content.Bounds().Width <= 0 || content.Bounds().Height <= 0 {
		t.Errorf("content not laid out: %+v", content.Bounds())
	}

	// Frame paints the window (and therefore the content).
	surface.handler.Frame(core.NewPainter(nullPaintBackend{}))
	if content.paints != 1 {
		t.Errorf("content paints = %d", content.paints)
	}

	// Mouse input lands in the content (frameless window: no title
	// offset; surface coords are window coords).
	cb := content.Bounds()
	surface.handler.Event(core.MousePressEvent{X: cb.X + 8, Y: cb.Y + 8, Button: core.LeftButton})
	if content.clicks != 1 {
		t.Errorf("content clicks = %d", content.clicks)
	}

	// Resize: window and content track the surface.
	surface.size = core.UnitSize{Width: 8 * 60, Height: 16 * 20}
	surface.handler.Resized(surface.size)
	if b := win.Bounds(); b.Width != 480 || b.Height != 320 {
		t.Errorf("bounds after resize = %+v", b)
	}

	// Damage requests flow to the surface.
	before := surface.invalidated
	host.Invalidate()
	if surface.invalidated != before+1 {
		t.Errorf("invalidate did not reach surface")
	}
}

func TestNativeRequestedFlag(t *testing.T) {
	win := NewWindow("n")
	if win.NativeRequested() {
		t.Error("default should be in-surface")
	}
	win.SetNativeRequested(true)
	if !win.NativeRequested() {
		t.Error("request not recorded")
	}
}
