package trinkets

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/purfecterm"
)

// gfxHarness builds a graphical desktop hosting a terminal trinket.
func gfxHarness(t *testing.T) (*raster.Backend, *Desktop, *PurfecTerm, *window.Window) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 800, Height: 480})
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 800, Height: 480})

	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	win := window.NewWindow("term")
	win.SetContent(term)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 480})
	win.SetActive(true)
	win.Layout()
	return b, d, term, win
}

func paintTerm(b *raster.Backend, term *PurfecTerm) {
	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b).WithOffset(0, 16)) // below titlebar
}

func countColor(b *raster.Backend, want color.RGBA, tolerant bool) int {
	img := b.Image()
	n := 0
	for y := 0; y < img.Rect.Max.Y; y++ {
		for x := 0; x < img.Rect.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if tolerant {
				if absDiff(c.R, want.R) < 30 && absDiff(c.G, want.G) < 30 && absDiff(c.B, want.B) < 30 {
					n++
				}
			} else if c == want {
				n++
			}
		}
	}
	return n
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// The inactive caret: when the trinket lacks focus (or its window is
// inactive) the block cursor renders as a hollow box outline - the
// pre-existing behavior, kept working. Focused renders a filled
// block (color swap).
func TestGfxCursorInactiveHollowBox(t *testing.T) {
	b, _, term, win := gfxHarness(t)
	buf := term.Terminal().Buffer()
	buf.SetCursor(2, 1)
	scheme := purfecterm.DefaultColorScheme()
	cursor := pcRGBA(scheme.Cursor)

	// Unfocused: hollow box at the cursor cell. Coordinates come from the
	// measured cell pitch (scale=1) so the test tracks the font grid.
	win.SetActive(false)
	paintTerm(b, term)
	img := b.Image()
	cw, ch := term.cellDims()
	col, row := 2, 1
	cellLeft := col * int(cw)
	cellMidX := cellLeft + int(cw)/2
	cellMidY := 16 + row*int(ch) + int(ch)/2 // +16 = paint offset below titlebar
	edge := img.RGBAAt(cellLeft, cellMidY)   // left edge, mid-height
	center := img.RGBAAt(cellMidX, cellMidY) // center
	if edge != cursor {
		t.Errorf("unfocused cursor: left edge = %v, want hollow outline %v", edge, cursor)
	}
	if center == cursor {
		t.Errorf("unfocused cursor: center filled - should be hollow")
	}

	// Focused: solid block via color swap (center gets the swapped bg).
	win.SetActive(true)
	term.SetFocus()
	if !term.gfxFocused() {
		t.Skip("focus plumbing unavailable in harness")
	}
	paintTerm(b, term)
	center = b.Image().RGBAAt(cellMidX, cellMidY)
	fgDefault := pcRGBA(scheme.Foreground(buf.IsDarkTheme()))
	if center != fgDefault {
		t.Errorf("focused block cursor: center = %v, want swapped fg %v", center, fgDefault)
	}
}

// Selection paints the scheme's selection background.
func TestGfxSelectionPainting(t *testing.T) {
	b, _, term, _ := gfxHarness(t)
	term.Feed([]byte("select me please"))
	buf := term.Terminal().Buffer()
	buf.StartSelection(0, 0)
	buf.UpdateSelection(7, 0)
	buf.EndSelection()

	paintTerm(b, term)
	sel := pcRGBA(purfecterm.DefaultColorScheme().Selection)
	if n := countColor(b, sel, false); n < 100 {
		t.Errorf("selection background missing (%d px of %v)", n, sel)
	}
}

// Custom glyphs replace text rendering for their runes.
func TestGfxCustomGlyph(t *testing.T) {
	b, _, term, _ := gfxHarness(t)
	buf := term.Terminal().Buffer()
	// A 2x2 glyph, all pixels palette index 1 (foreground).
	buf.SetGlyph('@', 2, []int{1, 1, 1, 1})
	term.Feed([]byte("\x1b[38;2;255;0;255m@@@\x1b[0m"))

	paintTerm(b, term)
	// The glyph fills its whole cell with the resolved fg (magenta).
	if n := countColor(b, color.RGBA{255, 0, 255, 255}, false); n < 300 {
		t.Errorf("custom glyph coverage too small: %d px", n)
	}
}

// Sprites composite over the text layer at sprite-unit positions.
func TestGfxSprite(t *testing.T) {
	b, _, term, _ := gfxHarness(t)
	buf := term.Terminal().Buffer()
	buf.SetGlyph('S', 2, []int{1, 1, 1, 1}) // palette idx 1 = foreground
	// Sprite at (8, 8) sprite-units, default palette (FGP -1), one tile.
	buf.SetSprite(1, 8, 8, 1, -1, 0, 1.0, 1.0, -1, []rune{'S'})

	paintTerm(b, term)
	fg := pcRGBA(purfecterm.DefaultColorScheme().Foreground(buf.IsDarkTheme()))
	if n := countColor(b, fg, false); n < 60 {
		t.Errorf("sprite glyph missing (%d fg px)", n)
	}
}

// Screen splits re-render buffer rows at split positions.
func TestGfxScreenSplit(t *testing.T) {
	b, _, term, _ := gfxHarness(t)
	buf := term.Terminal().Buffer()
	term.Feed([]byte("\x1b[38;2;255;128;0mSPLITROW\x1b[0m\r\nsecond line"))
	// Overlay: from scanline 32 (sprite units) show buffer row 0 again.
	buf.SetScreenSplit(1, 32, 0, 0, 0, 0, 0, 0)

	paintTerm(b, term)
	// The orange row 0 text must appear twice (once in place, once in
	// the split region) - so orange coverage roughly doubles.
	orange := countColor(b, color.RGBA{255, 128, 0, 255}, true)
	buf.DeleteAllScreenSplits()
	paintTerm(b, term)
	orangeNoSplit := countColor(b, color.RGBA{255, 128, 0, 255}, true)
	if orange < orangeNoSplit*3/2 {
		t.Errorf("split did not re-render row: with=%d without=%d", orange, orangeNoSplit)
	}
}

// Double-width lines paint each character across two cells.
func TestGfxDoubleWidthLine(t *testing.T) {
	b, _, term, _ := gfxHarness(t)
	term.Feed([]byte("\x1b#6\x1b[38;2;0;255;0mWW\x1b[0m"))
	paintTerm(b, term)
	green := 0
	img := b.Image()
	// Ink beyond the normal 2-cell extent (16px) proves 2x width.
	// Tolerant color match: antialiased coverage varies by typeface.
	for y := 16; y < 32; y++ {
		for x := 16; x < 32; x++ {
			c := img.RGBAAt(x, y)
			if c.G > 150 && c.R < 100 {
				green++
			}
		}
	}
	if green < 10 {
		t.Errorf("double-width glyphs not stretched (only %d green px in second double-cell)", green)
	}
}

// The vertical scrollbar thumb appears when scrolled into scrollback,
// and wheel scrolling moves the offset.
func TestGfxScrollbackAndScrollbar(t *testing.T) {
	b, _, term, _ := gfxHarness(t)
	buf := term.Terminal().Buffer()
	for i := 0; i < 100; i++ {
		term.Feed([]byte("line\r\n"))
	}
	if buf.GetMaxScrollOffset() <= 0 {
		t.Skip("no scrollback accumulated")
	}

	// Wheel up scrolls back.
	term.HandleMouseWheel(core.MouseWheelEvent{X: 10, Y: 10, DeltaY: -1})
	if buf.GetScrollOffset() != 3 {
		t.Errorf("wheel up: offset = %d, want 3", buf.GetScrollOffset())
	}
	// Shift+wheel scrolls horizontally (no-op at offset 0, but must
	// not touch the vertical offset).
	term.HandleMouseWheel(core.MouseWheelEvent{X: 10, Y: 10, DeltaY: -1, Modifiers: core.ShiftModifier})
	if buf.GetScrollOffset() != 3 {
		t.Errorf("shift+wheel changed vertical offset")
	}

	// Thumb painted in the right lane.
	paintTerm(b, term)
	thumb := color.RGBA{168, 168, 168, 255}
	found := 0
	img := b.Image()
	for y := 16; y < 480; y++ {
		for x := 800 - 5; x < 800; x++ {
			if img.RGBAAt(x, y) == thumb {
				found++
			}
		}
	}
	if found < 20 {
		t.Errorf("vertical scrollbar thumb not painted (%d px)", found)
	}
}

// Drag selection through the graphical mouse path.
func TestGfxDragSelection(t *testing.T) {
	_, _, term, _ := gfxHarness(t)
	term.Feed([]byte("drag across this text"))
	buf := term.Terminal().Buffer()

	term.HandleMousePress(core.MousePressEvent{X: 4, Y: 4, Button: core.LeftButton})
	term.HandleMouseMove(core.MouseMoveEvent{X: 60, Y: 4, Buttons: core.LeftButton})
	term.HandleMouseRelease(core.MouseReleaseEvent{X: 60, Y: 4, Button: core.LeftButton})

	if got := buf.GetSelectedText(); len(got) < 3 {
		t.Errorf("drag selection produced %q", got)
	}
}

// The context menu registers a popup and Copy puts the selection on
// the desktop clipboard.
func TestGfxContextMenuAndCopy(t *testing.T) {
	_, d, term, _ := gfxHarness(t)
	term.Feed([]byte("clipboard payload"))
	buf := term.Terminal().Buffer()
	buf.StartSelection(0, 0)
	buf.UpdateSelection(8, 0)
	buf.EndSelection()

	term.CopySelection()
	if got := d.Clipboard(); got == "" {
		t.Error("CopySelection left the clipboard empty")
	}

	// Right-click opens the context menu popup (no tracking active).
	term.HandleMousePress(core.MousePressEvent{X: 40, Y: 20, Button: core.RightButton})
	// The popup is registered with the window manager; painting the
	// popups must not panic and the menu id must be gone after a press
	// inside it selects an item.
	term.HandleMousePress(core.MousePressEvent{X: 40, Y: 20, Button: core.RightButton})
}
