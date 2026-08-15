package trinkets

import (
	"image"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// cellSurface is a minimal CELL backend: no pixels of its own, but it knows
// what a cell measures on the terminal it draws into and it can pass an image
// onward — which is exactly the shape of the TUI host.
//
// GraphicalMode is deliberately absent, so Painter.Graphical() is false and
// PurfecTerm takes the text path. That is the whole point: this is the surface
// where the pictures used to disappear.
type cellSurface struct {
	cellW, cellH int
	images       []cellSurfaceImage
	// seq counts draw calls, so a test can check what was painted AFTER what.
	// The real backend resolves "on top" exactly this way.
	seq        int
	lastCell   int
	firstImage int
}

type cellSurfaceImage struct {
	x, y core.Unit
	img  image.Image
	seq  int
}

// CellPixelSize implements core.CellPixelSizer.
func (c *cellSurface) CellPixelSize() (int, int) { return c.cellW, c.cellH }

// DrawImage / DrawImagePx implement core.ImageDrawer.
func (c *cellSurface) DrawImage(x, y core.Unit, img image.Image) {
	c.seq++
	if c.firstImage == 0 {
		c.firstImage = c.seq
	}
	c.images = append(c.images, cellSurfaceImage{x: x, y: y, img: img, seq: c.seq})
}
func (c *cellSurface) DrawImagePx(xPx, yPx int, img image.Image) {
	c.images = append(c.images, cellSurfaceImage{x: core.Unit(xPx), y: core.Unit(yPx), img: img})
}

func (c *cellSurface) Init() error         { return nil }
func (c *cellSurface) Shutdown()           {}
func (c *cellSurface) Size() core.UnitSize { return core.UnitSize{Width: 640, Height: 400} }
func (c *cellSurface) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (c *cellSurface) BeginFrame()           {}
func (c *cellSurface) EndFrame()             {}
func (c *cellSurface) Clear(style.CellStyle) {}
func (c *cellSurface) SetClip(core.UnitRect) {}
func (c *cellSurface) DrawCell(core.Unit, core.Unit, rune, style.CellStyle) {
	c.seq++
	c.lastCell = c.seq
}
func (c *cellSurface) DrawText(core.Unit, core.Unit, string, style.CellStyle, *core.Font) core.Unit {
	return 0
}
func (c *cellSurface) DrawTextAligned(core.UnitRect, string, core.Alignment, core.Alignment, style.CellStyle, *core.Font) {
}
func (c *cellSurface) FillRect(core.UnitRect, rune, style.CellStyle)                     {}
func (c *cellSurface) DrawRect(core.UnitRect, style.BorderStyle, style.CellStyle)        {}
func (c *cellSurface) DrawHLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (c *cellSurface) DrawVLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (c *cellSurface) DrawBox(core.UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (c *cellSurface) PollEvent() core.Event                                             { return nil }
func (c *cellSurface) WaitEvent() core.Event                                             { return nil }
func (c *cellSurface) SetCursorVisible(bool)                                             {}
func (c *cellSurface) SetCursorPosition(core.Unit, core.Unit)                            {}
func (c *cellSurface) SetCursorStyle(int)                                                {}
func (c *cellSurface) SupportsColor() bool                                               { return true }
func (c *cellSurface) SupportsMouse() bool                                               { return true }
func (c *cellSurface) SupportsUnicode() bool                                             { return true }
func (c *cellSurface) ColorDepth() int                                                   { return 24 }
func (c *cellSurface) GetClipboard() string                                              { return "" }
func (c *cellSurface) SetClipboard(string)                                               {}
func (c *cellSurface) Beep()                                                             {}

// tuiTerm is a painted terminal on a cell surface, the terminal-host shape.
func tuiTerm(t *testing.T, cellW, cellH int) (*PurfecTerm, *cellSurface) {
	t.Helper()
	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})
	c := &cellSurface{cellW: cellW, cellH: cellH}
	p := core.NewPainter(c)
	if p.Graphical() {
		t.Fatal("the stub reported a graphical surface; this must exercise the text path")
	}
	term.Paint(p)
	return term, c
}

// A child hosted on a CELL surface must still be told what a cell measures in
// pixels, or it cannot draw a picture at all.
//
// Everything about an image is expressed in that one number — its size, its
// placement, how many rows it reserves — and CSI 16 t is the only way to ask.
// The graphical host answers from its font. The terminal host has no font of
// its own; it has to pass on what the terminal IT draws into said, which the
// backend already asked for its own reasons. While nothing forwarded that, a
// child was told a cell is 0x0 and every graphics format failed at once —
// which reads like broken protocol support and is really an unanswered
// question.
func TestTUIChildIsToldTheOuterCellSize(t *testing.T) {
	term, _ := tuiTerm(t, 10, 20)
	cw, ch := term.Terminal().Buffer().GetCellPixelSize()
	if cw != 10 || ch != 20 {
		t.Errorf("child was told a cell is %dx%d px, want the outer terminal's 10x20", cw, ch)
	}
	// The pointer unit follows it, as on every other surface: an app has one
	// number to divide a ?1016 coordinate by.
	if pw, ph := term.Terminal().Buffer().GetPointerPixelUnit(); pw != cw || ph != ch {
		t.Errorf("pointer unit %dx%d drifted from the advertised cell %dx%d", pw, ph, cw, ch)
	}

	// And the pane's pixel window follows, so the PTY winsize carries pixels.
	// A program that sizes its viewport from TIOCGWINSZ rather than asking
	// gets nothing from a zero.
	cols, rows := term.Terminal().GetSize()
	wpx, hpx := term.ChildWindowPixels()
	if wpx != cols*10 || hpx != rows*20 {
		t.Errorf("child window = %dx%d px, want %dx%d for a %dx%d grid",
			wpx, hpx, cols*10, rows*20, cols, rows)
	}
}

// Until the outer terminal answers, nothing is advertised. A guess here is
// worse than silence: the child would size a picture to it and be wrong.
func TestTUIAdvertisesNothingBeforeTheTerminalAnswers(t *testing.T) {
	term, _ := tuiTerm(t, 0, 0)
	if cw, ch := term.Terminal().Buffer().GetCellPixelSize(); cw != 0 || ch != 0 {
		t.Errorf("child was told a cell is %dx%d px with no answer from the terminal, want 0x0", cw, ch)
	}
	if wpx, hpx := term.ChildWindowPixels(); wpx != 0 || hpx != 0 {
		t.Errorf("child window = %dx%d px with no cell size, want 0x0", wpx, hpx)
	}
}

// An image the child placed reaches the surface, which on a terminal host is
// what hands it to the outer terminal. The text path used to walk cells only
// and never ask the buffer for images at all, so a picture was decoded,
// placed, and silently dropped every frame.
func TestTUIPlacedImageReachesTheSurface(t *testing.T) {
	term, c := tuiTerm(t, 10, 20)

	const anchorRow, anchorCol = 2, 3
	term.Feed([]byte("\x1b[3;4H"))
	term.Feed(sixelSolidBlock(24, 24, 100, 0, 100))
	if n := len(term.Terminal().Buffer().GetImages()); n != 1 {
		t.Fatalf("the emulator placed %d images, want 1", n)
	}

	c.images = nil
	term.Paint(core.NewPainter(c))
	if len(c.images) != 1 {
		t.Fatalf("the surface received %d images, want 1: the text path dropped it", len(c.images))
	}

	// Anchored at the cell the child asked for, in units.
	m := c.Metrics()
	wantX, wantY := m.CellToUnitsX(anchorCol), m.CellToUnitsY(anchorRow)
	if got := c.images[0]; got.x != wantX || got.y != wantY {
		t.Errorf("image anchored at (%v,%v) units, want (%v,%v) for cell (%d,%d)",
			got.x, got.y, wantX, wantY, anchorCol, anchorRow)
	}
	if b := c.images[0].img.Bounds(); b.Dx() != 24 || b.Dy() != 24 {
		t.Errorf("image is %dx%d px, want the 24x24 the child sent: a cell surface "+
			"passes pixels through untouched, having none of its own to scale to",
			b.Dx(), b.Dy())
	}
}

// Two pictures in one frame must be two pictures.
//
// imageForBlitGfx hands back a shared scratch buffer, valid only until the next
// call. The graphical path composites immediately and is fine with that; this
// one hands the image to a surface that emits it at the END of the frame, so
// without a copy the second placement overwrites the first and both come out
// showing the last one.
func TestTUITwoImagesInAFrameAreNotTheSameBuffer(t *testing.T) {
	term, c := tuiTerm(t, 10, 20)

	// PARTIAL alpha, so each goes through the real conversion into the shared
	// scratch. A whole, unscaled, binary-alpha bitmap is wrapped where it lies
	// with no copy - which Sixel always is, so a sixel pair would pass this
	// test without ever touching the buffer it is about to prove is unsafe.
	rgba := func(r, g, b byte) []byte {
		pix := make([]byte, 12*12*4)
		for i := 0; i < len(pix); i += 4 {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = r, g, b, 128
		}
		return pix
	}
	term.Feed([]byte("\x1b[2;1H"))
	term.Feed(kittyRGBA(12, 12, rgba(255, 0, 0), ""))
	term.Feed([]byte("\x1b[8;1H"))
	term.Feed(kittyRGBA(12, 12, rgba(0, 0, 255), ""))
	if n := len(term.Terminal().Buffer().GetImages()); n != 2 {
		t.Skipf("the emulator placed %d images, want 2", n)
	}

	c.images = nil
	term.Paint(core.NewPainter(c))
	if len(c.images) != 2 {
		t.Fatalf("the surface received %d images, want 2", len(c.images))
	}
	if c.images[0].img == c.images[1].img {
		t.Fatal("both placements are the same buffer: the scratch was handed out twice, " +
			"so both pictures show whichever was built last")
	}
	if got := c.images[0].img.At(2, 2); got == c.images[1].img.At(2, 2) {
		t.Errorf("both pictures are the same colour (%v): the second overwrote the first", got)
	}
}

// An unchanged picture keeps the SAME buffer from frame to frame.
//
// A frame is drawn on a heartbeat whether anything happened or not. The surface
// decides what to re-transmit by comparing what it was handed against what it
// last drew, so a fresh copy every frame is a pointer it has never seen and
// every heartbeat re-sends the whole payload down the pty.
func TestTUIUnchangedImageKeepsItsBufferAcrossFrames(t *testing.T) {
	term, c := tuiTerm(t, 10, 20)
	term.Feed([]byte("\x1b[2;1H"))
	term.Feed(sixelSolidBlock(12, 12, 100, 0, 100))

	c.images = nil
	term.Paint(core.NewPainter(c))
	if len(c.images) != 1 {
		t.Fatalf("first frame: %d images, want 1", len(c.images))
	}
	first := c.images[0].img

	c.images = nil
	term.Paint(core.NewPainter(c)) // an idle repaint: nothing changed
	if len(c.images) != 1 {
		t.Fatalf("second frame: %d images, want 1", len(c.images))
	}
	if c.images[0].img != first {
		t.Error("the picture was rebuilt on an idle frame, so the surface sees a " +
			"buffer it has never been handed and re-sends the whole payload")
	}
}

// Pictures must be handed over AFTER this trinket's own text.
//
// A cell surface decides what is on top by paint order, and a picture is queued
// rather than composited — so it is taken to be covered by whatever is written
// to its cells afterwards. Queued first, a terminal's own blank cells count as
// painted over the picture and every cell of it is suppressed: the image
// vanishes entirely rather than merely spilling.
//
// That is not a rule this file can enforce on its own, so it is pinned here.
func TestTUIImagesAreHandedOverAfterTheText(t *testing.T) {
	term, c := tuiTerm(t, 10, 20)
	term.Feed([]byte("\x1b[2;1H"))
	term.Feed(sixelSolidBlock(24, 24, 100, 0, 100))
	term.Feed([]byte("\x1b[10;1Hsome text below it"))

	c.images, c.seq, c.lastCell, c.firstImage = nil, 0, 0, 0
	term.Paint(core.NewPainter(c))
	if len(c.images) == 0 {
		t.Fatal("no image reached the surface")
	}
	if c.lastCell == 0 {
		t.Fatal("no text reached the surface; the ordering cannot be judged")
	}
	if c.firstImage < c.lastCell {
		t.Errorf("the first image was handed over at step %d, before the last text cell at %d: "+
			"a surface that resolves depth by paint order will treat the terminal's own "+
			"cells as covering the picture and drop it entirely",
			c.firstImage, c.lastCell)
	}
}

// A picture scrolled partly off the top or left keeps the part still on screen.
//
// The outer terminal places a picture at a cell and draws all of it from there;
// it cannot be told to start part way in. So the trimming has to happen here,
// on the pixels, and dropping the whole thing instead — which this did — makes
// a picture disappear outright the moment one corner leaves the view.
func TestTUICropLeadingCellsTrimsRatherThanDrops(t *testing.T) {
	term, c := tuiTerm(t, 10, 20)
	p := core.NewPainter(c)
	// 4 cells wide, 3 tall, at 10x20.
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))

	for _, tc := range []struct {
		name        string
		col, row    int
		wantW       int
		wantH       int
		wantDropped bool
	}{
		{name: "two cells off the left", col: -2, row: 3, wantW: 20, wantH: 60},
		{name: "one row off the top", col: 5, row: -1, wantW: 40, wantH: 40},
		{name: "both corners out", col: -1, row: -2, wantW: 30, wantH: 20},
		{name: "entirely off the left", col: -4, row: 0, wantDropped: true},
		{name: "entirely off the top", col: 0, row: -3, wantDropped: true},
	} {
		got := term.cropLeadingCells(p, img, tc.col, tc.row)
		if tc.wantDropped {
			if got != nil {
				t.Errorf("%s: kept %v, want nothing left", tc.name, got.Bounds())
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: dropped the whole picture, want the visible part", tc.name)
			continue
		}
		if w, h := got.Bounds().Dx(), got.Bounds().Dy(); w != tc.wantW || h != tc.wantH {
			t.Errorf("%s: kept %dx%d px, want %dx%d", tc.name, w, h, tc.wantW, tc.wantH)
		}
	}

	// With no cell size there is no way to measure the cut, and guessing would
	// misplace the picture — so it is left to the caller to drop.
	_, plain := tuiTerm(t, 0, 0)
	if got := term.cropLeadingCells(core.NewPainter(plain), img, -1, 0); got != nil {
		t.Error("cropped without knowing the cell size")
	}
}
