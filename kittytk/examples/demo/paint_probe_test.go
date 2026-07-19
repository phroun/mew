package main

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// gridBackend is a throwaway RenderBackend that records runes so we
// can see exactly what gets painted where.
type gridBackend struct {
	w, h  int
	cells [][]rune
	clip  core.UnitRect
}

func newGridBackend(w, h int) *gridBackend {
	g := &gridBackend{w: w, h: h, cells: make([][]rune, h)}
	for i := range g.cells {
		g.cells[i] = make([]rune, w)
		for j := range g.cells[i] {
			g.cells[i][j] = ' '
		}
	}
	return g
}

func (g *gridBackend) Init() error { return nil }
func (g *gridBackend) Shutdown()   {}
func (g *gridBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (g *gridBackend) Size() core.UnitSize {
	return core.UnitSize{Width: core.Unit(g.w * 8), Height: core.Unit(g.h * 16)}
}
func (g *gridBackend) BeginFrame()             {}
func (g *gridBackend) EndFrame()               {}
func (g *gridBackend) Clear(style.CellStyle)   {}
func (g *gridBackend) SetClip(c core.UnitRect) { g.clip = c }
func (g *gridBackend) put(x, y core.Unit, ch rune) {
	cx, cy := int(x/8), int(y/16)
	if cx < 0 || cy < 0 || cx >= g.w || cy >= g.h {
		return
	}
	if !g.clip.IsEmpty() {
		if x < g.clip.X || y < g.clip.Y || x >= g.clip.X+g.clip.Width || y >= g.clip.Y+g.clip.Height {
			return
		}
	}
	g.cells[cy][cx] = ch
}
func (g *gridBackend) DrawCell(x, y core.Unit, ch rune, _ style.CellStyle) { g.put(x, y, ch) }
func (g *gridBackend) DrawText(x, y core.Unit, text string, _ style.CellStyle, _ *core.Font) core.Unit {
	for i, ch := range []rune(text) {
		g.put(x+core.Unit(i*8), y, ch)
	}
	return core.Unit(len([]rune(text)) * 8)
}
func (g *gridBackend) DrawTextAligned(b core.UnitRect, text string, _, _ core.Alignment, s style.CellStyle, f *core.Font) {
	g.DrawText(b.X, b.Y, text, s, f)
}
func (g *gridBackend) FillRect(r core.UnitRect, ch rune, _ style.CellStyle) {
	for y := r.Y; y < r.Y+r.Height; y += 16 {
		for x := r.X; x < r.X+r.Width; x += 8 {
			g.put(x, y, ch)
		}
	}
}
func (g *gridBackend) DrawRect(r core.UnitRect, b style.BorderStyle, _ style.CellStyle) {
	x2, y2 := r.X+r.Width-8, r.Y+r.Height-16
	for x := r.X + 8; x < x2; x += 8 {
		g.put(x, r.Y, b.Horizontal)
		g.put(x, y2, b.Horizontal)
	}
	for y := r.Y + 16; y < y2; y += 16 {
		g.put(r.X, y, b.Vertical)
		g.put(x2, y, b.Vertical)
	}
	g.put(r.X, r.Y, b.TopLeft)
	g.put(x2, r.Y, b.TopRight)
	g.put(r.X, y2, b.BottomLeft)
	g.put(x2, y2, b.BottomRight)
}
func (g *gridBackend) DrawHLine(x, y, w core.Unit, ch rune, _ style.CellStyle) {
	for i := core.Unit(0); i < w; i += 8 {
		g.put(x+i, y, ch)
	}
}
func (g *gridBackend) DrawVLine(x, y, h core.Unit, ch rune, _ style.CellStyle) {
	for i := core.Unit(0); i < h; i += 16 {
		g.put(x, y+i, ch)
	}
}
func (g *gridBackend) DrawBox(r core.UnitRect, b style.BorderStyle, _ string, s style.CellStyle) {
	g.DrawRect(r, b, s)
}
func (g *gridBackend) PollEvent() core.Event                  { return nil }
func (g *gridBackend) WaitEvent() core.Event                  { return nil }
func (g *gridBackend) SetCursorVisible(bool)                  {}
func (g *gridBackend) SetCursorPosition(core.Unit, core.Unit) {}
func (g *gridBackend) SupportsColor() bool                    { return true }
func (g *gridBackend) SupportsMouse() bool                    { return false }
func (g *gridBackend) SupportsUnicode() bool                  { return true }
func (g *gridBackend) ColorDepth() int                        { return 256 }
func (g *gridBackend) GetClipboard() string                   { return "" }
func (g *gridBackend) SetClipboard(string)                    {}
func (g *gridBackend) Beep()                                  {}

func (g *gridBackend) dump() string {
	var sb strings.Builder
	for _, row := range g.cells {
		sb.WriteString(strings.TrimRight(string(row), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func paintScript(t *testing.T, script string) string {
	t.Helper()
	ctx := &protocol.BindContext{Dispatch: func(string) {}}
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(ctx),
		byID:  make(map[uint64]any),
	}
	parsed, err := protocol.Parse(script)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reply, err := protocol.NewSession().Execute(parsed, factory)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	rootTrinket, _ := factory.byID[reply.IDs["root"]].(core.Trinket)
	if rootTrinket == nil {
		t.Fatal("no root trinket")
	}

	g := newGridBackend(50, 14)
	w := window.NewWindow("T")
	w.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 8 * 50, Height: 16 * 14})
	w.SetContent(rootTrinket)
	w.Layout()
	w.Paint(core.NewPainter(g))
	return g.dump()
}

func TestPanelBorderPaintProbe(t *testing.T) {
	base := `
root=new panel layout=vbox %s children={
	new label caption="hello"
	new checkbox caption="check"
}
`
	with := paintScript(t, strings.Replace(base, "%s", "border", 1))
	without := paintScript(t, strings.Replace(base, "%s", "!border", 1))
	t.Logf("WITH border:\n%s", with)
	t.Logf("WITHOUT border (!border):\n%s", without)
	if with == without {
		t.Error("border flag made no visual difference")
	}
}

func TestProtocolDemoWindowPaint(t *testing.T) {
	out := paintScript(t, protocolWindowScript)
	t.Logf("Protocol Demo window:\n%s", out)
	if strings.Contains(out, "·") {
		t.Error("separator must not show grab-handle dots (splitter-only decoration)")
	}
}

func TestSeparatorPaint(t *testing.T) {
	out := paintScript(t, `
root=new panel layout=vbox children={
	new separator
	new separator caption="Section"
}
`)
	t.Logf("separators:\n%s", out)
	if strings.Contains(out, "·") {
		t.Error("separator must not show grab-handle dots")
	}
	if !strings.Contains(out, "── Section ──") {
		t.Error("titled separator should read \"── Section ──\" (single spaces, no dots)")
	}
	// The plain rule spans the window's full content width (48 cells).
	if !strings.Contains(out, strings.Repeat("─", 48)) {
		t.Error("plain separator should span the full content width")
	}
}
