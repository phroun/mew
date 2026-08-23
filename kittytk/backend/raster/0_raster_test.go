package raster_test

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	_ "github.com/phroun/kittytk/objects/trinkets" // wire vocabulary registrations
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// The D23 proof: a protocol-built window rendered through the PIXEL
// implementation of the same primitives the TUI uses - real glyphs,
// real lines - with zero trinket changes. Set RASTER_PNG=/path to
// keep the image.
func TestRenderProtocolWindowToPNG(t *testing.T) {
	b, err := raster.New(640, 384)
	if err != nil {
		t.Fatal(err)
	}
	b.Clear(style.DefaultStyle())

	conn := client.NewInProcess(nil)
	ui, err := conn.Build(`
w=new window title="Graphical KittyTK" width=608 height=352 children={
	p=new panel layout=vbox spacing=0 children={
		new label caption="This window is rendered as PIXELS through the same"
		new label caption="primitives the terminal uses - no trinket changed."
		new separator caption="Trinkets"
		new checkbox caption="A real checkbox (checked)" checked
		new checkbox caption="Tri-state, indeterminate" tristate
		new radiobutton caption="Radio option one" group=g checked
		new radiobutton caption="Radio option two" group=g
		new textinput text="Text input content"
		new combobox children={new item caption="First item"; new item caption="Second"} selected=0
		new progress value=65
		new panel border border_style=double layout=vbox children={
			new label caption="A double border drawn with real lines"
		}
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	w := ui.Object("w").Target().(*window.Window)
	w.SetBounds(core.UnitRect{X: 16, Y: 16, Width: 608, Height: 352})
	w.Layout()
	w.Paint(core.NewPainter(b).WithOffset(16, 16))

	// The frame must contain non-background pixels (something drew).
	img := b.Image()
	bgAt := img.RGBAAt(2, 2)
	drawn := 0
	for y := 0; y < 384; y += 3 {
		for x := 0; x < 640; x += 3 {
			if img.RGBAAt(x, y) != bgAt {
				drawn++
			}
		}
	}
	if drawn < 500 {
		t.Fatalf("suspiciously empty frame: %d non-bg samples", drawn)
	}

	// Border lines are PIXELS: the double-border panel must produce
	// runs of identical-colored horizontal pixels longer than any
	// glyph (a line, not box-drawing characters).
	longestRun := 0
	for y := 0; y < 384; y++ {
		run := 1
		prev := img.RGBAAt(0, y)
		for x := 1; x < 640; x++ {
			c := img.RGBAAt(x, y)
			if c == prev && c != bgAt && (c != color.RGBA{16, 16, 24, 255}) {
				run++
				if run > longestRun {
					longestRun = run
				}
			} else {
				run = 1
				prev = c
			}
		}
	}
	if longestRun < 200 {
		t.Errorf("no long solid line found (longest run %d px) - borders may still be runes", longestRun)
	}

	out := os.Getenv("RASTER_PNG")
	if out == "" {
		out = filepath.Join(t.TempDir(), "window.png")
	}
	if err := b.WritePNG(out); err != nil {
		t.Fatal(err)
	}
	t.Logf("rendered to %s", out)
}
