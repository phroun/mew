package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// buildDemoWindow parses the shared main-window script onto a fresh
// desktop whose backend renders at the given UI point size, the same
// wiring kittytk-sdl performs for cfg.FontSize: font_size scales
// pixels-per-unit (the cell's pixel size), the denomination stays 8x16,
// and the UI font stays one cell tall in units.
func buildDemoWindow(t *testing.T, fontSize int) (*raster.Backend, *window.Window) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	b, err := raster.New(1024, 768)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(fontSize)

	d := trinkets.NewDesktop()
	d.SetBackend(b)
	d.SetFont(&core.Font{Name: "ui-text", Size: 12})
	d.SetBounds(core.UnitRect{Width: 1024, Height: 768})
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 1024, Height: 768})

	ctx := &protocol.BindContext{Dispatch: func(string) {}}
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(ctx),
		byID:  make(map[uint64]any),
	}
	script, err := protocol.Parse(mainWindowScript())
	if err != nil {
		t.Fatal(err)
	}
	reply, err := protocol.NewSession().Execute(script, factory)
	if err != nil {
		t.Fatal(err)
	}
	win := factory.byID[reply.IDs["w"]].(*window.Window)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 1024, Height: 768})
	win.SetActive(true)
	win.Layout()
	return b, win
}

// FontSize scales the whole desktop's type in PIXELS while the unit
// layout is invariant: the demo window paints its chrome intact, the
// root denomination stays 8x16 at every size, the title measures the
// same number of UNITS, and pixels-per-unit grows so the same title
// occupies more device pixels. This is the kittytk-sdl font_size knob
// end to end; at 12pt it is the historical default. PNGs are written for
// eyeballing.
func TestFontSizeScalesDesktop(t *testing.T) {
	dir := t.TempDir()
	if env := os.Getenv("KITTYTK_PROOF_DIR"); env != "" {
		dir = env
	}

	var unitWidths [2]core.Unit
	var ppu [2]float64
	for i, size := range []int{12, 18} {
		b, win := buildDemoWindow(t, size)

		// font_size does NOT change the denomination.
		if m := b.Metrics(); m.CellWidth != 8 || m.CellHeight != 16 {
			t.Fatalf("%dpt root denomination = %+v, want 8x16", size, m)
		}

		// The title measures the same UNITS at every font_size (layout is
		// font_size-invariant); only pixels-per-unit grows.
		unitWidths[i] = (&core.Font{Name: "ui-text", Size: 12}).MeasureText("KittyTK Demo")
		ppu[i] = b.PxPerUnit()

		b.Clear(style.DefaultStyle())
		win.Paint(core.NewPainter(b))
		out := filepath.Join(dir, "fontsize_"+itoa(size)+".png")
		if err := b.WritePNG(out); err != nil {
			t.Fatalf("WritePNG: %v", err)
		}
		t.Logf("font_size=%d -> denomination %+v, title %d units, pxPerUnit %.3f, png %s",
			size, b.Metrics(), unitWidths[i], ppu[i], out)
	}

	if unitWidths[0] != unitWidths[1] {
		t.Errorf("title unit width changed with font_size (should be invariant): 12pt=%d 18pt=%d",
			unitWidths[0], unitWidths[1])
	}
	if ppu[1] <= ppu[0] {
		t.Errorf("pixels-per-unit did not grow with font_size: 12pt=%.3f 18pt=%.3f", ppu[0], ppu[1])
	}
	// 18pt is exactly 1.5x the 12pt base pixels-per-unit.
	if ppu[0] != 1.0 || ppu[1] != 1.5 {
		t.Errorf("pxPerUnit = {%.3f, %.3f}, want {1.0, 1.5}", ppu[0], ppu[1])
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
