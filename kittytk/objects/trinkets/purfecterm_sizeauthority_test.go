//go:build sdl

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// In graphical mode the native-pixel paint path is the single authority on
// the terminal's column/row count. updateTerminalSize (invoked by SetBounds
// on every relayout) divides in units and, at a fractional pixels-per-unit,
// undercounts by a row/column. If it were allowed to resize the emulator it
// would fight the paint path: relayout shrinks the grid, the next paint grows
// it back, and each shrink scrolls the bottom line into scrollback while the
// word-wrapper wraps at the wrong column. This asserts that once paint has
// sized the grid, a relayout (SetBounds) leaves the emulator's size - and
// therefore the wrapper's column width - untouched.
func TestGfxPaintOwnsTerminalSize(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, _ := raster.NewScaled(1200, 640, 2)
	b.SetFontSize(10) // fractional ppu: 2*10/12 = 1.667 px/unit
	core.SetTextMeasurer(b)
	d := NewDesktop()
	d.SetBackend(b)
	sz := b.Size()
	d.SetBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	d.SetFont(&core.Font{Name: "ui-text", Size: 10})
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("no term")
	}
	term.SetFont(&core.Font{Name: "ui-text", Size: 10})
	win := window.NewWindow("term")
	win.SetContent(term)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: sz.Width, Height: sz.Height})
	win.SetActive(true)
	win.Layout()

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b)) // paint sizes cols/rows from the pixel viewport
	wantCols, wantRows := term.Terminal().GetSize()
	if wantCols <= 0 || wantRows <= 0 {
		t.Fatalf("paint produced a degenerate grid %dx%d", wantCols, wantRows)
	}

	// A relayout at the same bounds must not disturb the grid the paint path
	// established - otherwise the wrapper's column count drifts and the bottom
	// row scrolls away under the cursor.
	term.SetBounds(term.Bounds())
	gotCols, gotRows := term.Terminal().GetSize()
	if gotCols != wantCols || gotRows != wantRows {
		t.Errorf("SetBounds resized the grid: paint set %dx%d, relayout changed it to %dx%d",
			wantCols, wantRows, gotCols, gotRows)
	}
}
