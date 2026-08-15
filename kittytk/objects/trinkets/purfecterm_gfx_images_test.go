package trinkets

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/purfecterm"
)

// sixelSolidBlock builds a DCS Sixel sequence painting a solid w x h block in
// one color register. h must be a multiple of 6: a sixel carries six vertical
// pixels, so each band is one row of "~" (all six bits set), run-length encoded
// across the width. r, g and b are DEC percentages (0-100), as Sixel defines
// its RGB registers.
func sixelSolidBlock(w, h, r, g, b int) []byte {
	var s strings.Builder
	s.WriteString("\x1bP0;1;0q")              // P2=1: unset pixels stay transparent
	fmt.Fprintf(&s, "#0;2;%d;%d;%d", r, g, b) // define + select register 0 as RGB
	for band := 0; band < h/6; band++ {
		if band > 0 {
			s.WriteByte('-') // next band
		}
		fmt.Fprintf(&s, "!%d~", w)
	}
	s.WriteString("\x1b\\")
	return []byte(s.String())
}

// colorExtent returns the bounding box (inclusive) and count of framebuffer
// pixels exactly matching want.
func colorExtent(b *raster.Backend, want color.RGBA) (x0, y0, x1, y1, n int) {
	return pixelExtent(b, func(c color.RGBA) bool { return c == want })
}

// pixelExtent is colorExtent over an arbitrary predicate, for imagery that has
// been resampled and so lands near a color rather than exactly on it.
func pixelExtent(b *raster.Backend, match func(color.RGBA) bool) (x0, y0, x1, y1, n int) {
	img := b.Image()
	x0, y0 = math.MaxInt32, math.MaxInt32
	x1, y1 = -1, -1
	for y := 0; y < img.Rect.Max.Y; y++ {
		for x := 0; x < img.Rect.Max.X; x++ {
			if !match(img.RGBAAt(x, y)) {
				continue
			}
			n++
			if x < x0 {
				x0 = x
			}
			if y < y0 {
				y0 = y
			}
			if x > x1 {
				x1 = x
			}
			if y > y1 {
				y1 = y
			}
		}
	}
	return
}

// A Sixel image placed by the emulator blits onto the graphical terminal at its
// anchor cell, one image pixel per device pixel. The decoder hands back a plain
// RGBA byte slice; the renderer wraps it and hands it to the painter, so any
// scaling, stride or premultiply mistake shows up as the wrong coverage or the
// wrong extent - not merely the wrong shade.
func TestGfxSixelImageBlit(t *testing.T) {
	term, b := gfxImageTerm(t)

	const imgW, imgH = 24, 24
	const anchorRow, anchorCol = 2, 3
	term.Feed([]byte(fmt.Sprintf("\x1b[%d;%dH", anchorRow+1, anchorCol+1)))
	term.Feed(sixelSolidBlock(imgW, imgH, 100, 0, 100))

	if got := len(term.Terminal().Buffer().GetImages()); got != 1 {
		t.Fatalf("emulator placed %d images, want 1", got)
	}

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	magenta := color.RGBA{255, 0, 255, 255}
	x0, y0, x1, y1, n := colorExtent(b, magenta)
	if n == 0 {
		t.Fatal("sixel image did not render: no image pixels in the framebuffer")
	}
	if w, h := x1-x0+1, y1-y0+1; w != imgW || h != imgH {
		t.Errorf("image extent = %dx%d, want %dx%d (the blit is being scaled)", w, h, imgW, imgH)
	}
	if n != imgW*imgH {
		t.Errorf("image coverage = %d px, want %d (solid block, one image pixel per device pixel)", n, imgW*imgH)
	}

	// The anchor is the cell grid's, at the same scaled cell pitch the text
	// layer paints with - not the unit origin, and not the raw row/col.
	cwU, chU := term.cellDims()
	ppu := term.gfx.ppu
	wantX := int(math.Floor(float64(anchorCol*cwU) * ppu))
	wantY := int(math.Floor(float64(anchorRow*chU) * ppu))
	if x0 != wantX || y0 != wantY {
		t.Errorf("image anchored at (%d,%d) px, want (%d,%d) for cell (%d,%d) at %.3f px/unit",
			x0, y0, wantX, wantY, anchorCol, anchorRow, ppu)
	}
}

// A transparent Sixel pixel leaves the terminal background alone: the decoder
// emits alpha 0 for unset pixels and the blit must composite, not overwrite.
func TestGfxSixelImageTransparency(t *testing.T) {
	term, b := gfxImageTerm(t)

	// Two bands, 12 px wide: the first painted, the second left unset.
	// "#0;2;0;100;0" defines register 0 as pure green.
	term.Feed([]byte("\x1b[1;1H"))
	term.Feed([]byte("\x1bP0;1;0q#0;2;0;100;0!12~-\x1b\\"))

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	green := color.RGBA{0, 255, 0, 255}
	_, _, _, y1, n := colorExtent(b, green)
	if n != 12*6 {
		t.Errorf("painted band = %d px, want %d", n, 12*6)
	}
	if y1 != 5 {
		t.Errorf("painted band reaches y=%d, want 5: the unset band must stay transparent", y1)
	}
}

// gfxImageTerm builds a painted terminal on a raster backend, ready for image
// tests: the grid (and the cell pixel size the emulator anchors images against)
// has settled by the time it returns.
func gfxImageTerm(t *testing.T) (*PurfecTerm, *raster.Backend) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})
	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))
	return term, b
}

// cellPx is the terminal's cell size in device pixels, computed the way the
// paint path computes it.
func cellPx(term *PurfecTerm) (w, h float64) {
	buf := term.Terminal().Buffer()
	cwU, chU := term.cellDims()
	return float64(cwU) * buf.GetHorizontalScale() * term.gfx.ppu,
		float64(chU) * term.gfx.ppu * buf.GetVerticalScale()
}

// kittyRGBA builds a kitty graphics transmit-and-display command carrying raw
// RGBA (f=32) pixels, with any extra keys the caller needs.
func kittyRGBA(w, h int, pix []byte, extra string) []byte {
	return []byte(fmt.Sprintf("\x1b_Ga=T,f=32,s=%d,v=%d%s;%s\x1b\\",
		w, h, extra, base64.StdEncoding.EncodeToString(pix)))
}

// The cell size the terminal reports (CSI 16 t) must be its REAL device cell,
// because a program sizes images against it and the emulator divides by it to
// work out how many rows a placement reserves. The synthetic sub-unit grid used
// for ?1016 pointer coordinates is a separate axis and must not leak into it.
func TestGfxCellPixelSizeIsRealDeviceSize(t *testing.T) {
	term, _ := gfxImageTerm(t)
	buf := term.Terminal().Buffer()

	wantW, wantH := cellPx(term)
	gotW, gotH := buf.GetCellPixelSize()
	if gotW != int(math.Round(wantW)) || gotH != int(math.Round(wantH)) {
		t.Errorf("reported cell size = %dx%d px, want %dx%d",
			gotW, gotH, int(math.Round(wantW)), int(math.Round(wantH)))
	}
	if gotW == 1000 || gotH == 1000 {
		t.Errorf("cell size %dx%d is a synthetic grid, not device pixels", gotW, gotH)
	}
	// The pointer unit is the SAME as the reported cell size, not a synthetic
	// grid of its own. This assertion used to demand 1000 square, which is the
	// half of the split that broke the mouse: the app has one number to divide
	// a ?1016 coordinate by - the cell size CSI 16 t gave it - so a report
	// encoded in any other unit lands the click off by the ratio between them.
	// See TestPixelReportDividesByTheAdvertisedCellSize.
	if pw, ph := buf.GetPointerPixelUnit(); pw != gotW || ph != gotH {
		t.Errorf("pointer unit = %dx%d, want the advertised cell %dx%d", pw, ph, gotW, gotH)
	}

	// The size is reported so image geometry works: a bitmap taller than one
	// cell must reserve the rows it actually covers.
	const imgW, imgH = 24, 24
	term.Feed([]byte("\x1b[3;1H"))
	term.Feed(sixelSolidBlock(imgW, imgH, 100, 0, 100))
	images := buf.GetImages()
	if len(images) != 1 {
		t.Fatalf("emulator placed %d images, want 1", len(images))
	}
	wantRows := (imgH + gotH - 1) / gotH
	if images[0].CellsHigh != wantRows {
		t.Errorf("a %d px tall image reserved %d rows, want %d at a %d px cell",
			imgH, images[0].CellsHigh, wantRows, gotH)
	}
}

// Partial alpha must be premultiplied before the blit. A Bitmap carries STRAIGHT
// alpha; handing those bytes to the painter as if they were Go's premultiplied
// image.RGBA makes a half-transparent pixel composite at full source intensity,
// which washes out every soft edge a PNG or a browser frame brings in.
func TestGfxImagePartialAlphaBlend(t *testing.T) {
	term, b := gfxImageTerm(t)

	const anchorRow, anchorCol = 3, 5
	cwPx, chPx := cellPx(term)
	px := int(math.Floor(float64(anchorCol) * cwPx))
	py := int(math.Floor(float64(anchorRow) * chPx))

	// What the image will land on, sampled before it is placed, so the
	// expectation holds whatever the color scheme paints.
	dst := b.Image().RGBAAt(px, py)

	const alpha = 128
	pix := make([]byte, 0, 2*2*4)
	for i := 0; i < 4; i++ {
		pix = append(pix, 255, 255, 255, alpha) // white at half alpha
	}
	term.Feed([]byte(fmt.Sprintf("\x1b[%d;%dH", anchorRow+1, anchorCol+1)))
	term.Feed(kittyRGBA(2, 2, pix, ""))
	if n := len(term.Terminal().Buffer().GetImages()); n != 1 {
		t.Fatalf("emulator placed %d images, want 1", n)
	}

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))
	got := b.Image().RGBAAt(px, py)

	// Source-over with STRAIGHT source alpha: src*a + dst*(1-a).
	blend := func(d uint8) uint8 {
		return uint8(255*alpha/255 + uint32(d)*(255-alpha)/255)
	}
	want := color.RGBA{blend(dst.R), blend(dst.G), blend(dst.B), 255}
	// Treating the straight bytes as premultiplied instead: src + dst*(1-a),
	// which saturates. Only meaningful where the two actually differ.
	bug := func(d uint8) uint8 {
		v := 255 + uint32(d)*(255-alpha)/255
		if v > 255 {
			v = 255
		}
		return uint8(v)
	}
	near := func(a, b uint8) bool { return int(a)-int(b) <= 2 && int(b)-int(a) <= 2 }
	if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) {
		t.Errorf("half-alpha pixel = %v over %v, want ~%v", got, dst, want)
	}
	if near(want.R, bug(dst.R)) && near(want.G, bug(dst.G)) && near(want.B, bug(dst.B)) {
		t.Skip("background too bright to tell a premultiply mistake from a correct blend")
	}
	if near(got.R, bug(dst.R)) && near(got.G, bug(dst.G)) && near(got.B, bug(dst.B)) {
		t.Errorf("half-alpha pixel = %v, the unpremultiplied result: straight alpha was blitted as premultiplied", got)
	}
}

// A placement can ask to be drawn at a size that is not the size it was decoded
// at - the kitty protocol's c= and r= size an image in CELLS. The renderer must
// scale to PlacedImage.DestSize, in the same device pixels it draws in.
func TestGfxImageScaledToCells(t *testing.T) {
	term, b := gfxImageTerm(t)

	const cols, rows = 4, 3
	pix := make([]byte, 0, 2*2*4)
	for i := 0; i < 4; i++ {
		pix = append(pix, 255, 0, 0, 255) // opaque red
	}
	term.Feed([]byte("\x1b[3;1H"))
	term.Feed(kittyRGBA(2, 2, pix, fmt.Sprintf(",c=%d,r=%d", cols, rows)))

	images := term.Terminal().Buffer().GetImages()
	if len(images) != 1 {
		t.Fatalf("emulator placed %d images, want 1", len(images))
	}
	cellW, cellH := term.Terminal().Buffer().GetCellPixelSize()
	wantW, wantH := cols*cellW, rows*cellH
	if dw, dh := images[0].DestSize(); dw != wantW || dh != wantH {
		t.Fatalf("emulator sized the placement %dx%d px, want %dx%d", dw, dh, wantW, wantH)
	}

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	// Resampled, so match on hue rather than an exact value.
	red := func(c color.RGBA) bool { return c.R > 200 && c.G < 60 && c.B < 60 }
	x0, y0, x1, y1, n := pixelExtent(b, red)
	if n == 0 {
		t.Fatal("scaled image did not render")
	}
	if w, h := x1-x0+1, y1-y0+1; w != wantW || h != wantH {
		t.Errorf("scaled extent = %dx%d px, want %dx%d (the 2x2 source was not scaled to its dest size)",
			w, h, wantW, wantH)
	}
	if n != wantW*wantH {
		t.Errorf("scaled coverage = %d px, want %d (solid source, solid result)", n, wantW*wantH)
	}
}

// A source crop (the kitty protocol's x/y/w/h) shows only part of the stored
// image, and the placement is the size of the CROP, not of the whole bitmap.
func TestGfxImageSourceCrop(t *testing.T) {
	term, b := gfxImageTerm(t)

	// 4x4: the left half red, the right half blue. Cropping to the right half
	// must leave no red on screen at all.
	pix := make([]byte, 0, 4*4*4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if x < 2 {
				pix = append(pix, 255, 0, 0, 255)
			} else {
				pix = append(pix, 0, 0, 255, 255)
			}
		}
	}
	term.Feed([]byte("\x1b[3;1H"))
	term.Feed(kittyRGBA(4, 4, pix, ",x=2,y=0,w=2,h=4"))

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	blue := color.RGBA{0, 0, 255, 255}
	x0, y0, x1, y1, n := colorExtent(b, blue)
	if n != 2*4 {
		t.Errorf("cropped image covered %d px, want %d", n, 2*4)
	}
	if w, h := x1-x0+1, y1-y0+1; w != 2 || h != 4 {
		t.Errorf("cropped extent = %dx%d, want 2x4", w, h)
	}
	if _, _, _, _, red := colorExtent(b, color.RGBA{255, 0, 0, 255}); red != 0 {
		t.Errorf("%d px of the cropped-away half rendered: the source rect is being ignored", red)
	}
}

// An image at a negative z-index draws UNDER the glyphs, which is what makes
// text-over-image work; a virtual placement is positioned by placeholder cells
// and must not be drawn at its anchor at all.
func TestGfxImageZOrderAndVirtual(t *testing.T) {
	term, b := gfxImageTerm(t)

	buf := term.Terminal().Buffer()
	below, above := buf.GetImagesByZ()
	if len(below)+len(above) != 0 {
		t.Fatalf("terminal started with %d placements", len(below)+len(above))
	}

	// A virtual placement (U=1) is held for its placeholder cells, not drawn.
	pix := make([]byte, 0, 2*2*4)
	for i := 0; i < 4; i++ {
		pix = append(pix, 0, 255, 0, 255)
	}
	term.Feed([]byte("\x1b[3;1H"))
	term.Feed(kittyRGBA(2, 2, pix, ",i=7,U=1"))

	if n := len(buf.GetImages()); n != 1 {
		t.Fatalf("emulator placed %d images, want 1", n)
	}
	if below, above := buf.GetImagesByZ(); len(below)+len(above) != 0 {
		t.Fatalf("a virtual placement reached the draw bands (%d below, %d above)",
			len(below), len(above))
	}

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))
	if _, _, _, _, n := colorExtent(b, color.RGBA{0, 255, 0, 255}); n != 0 {
		t.Errorf("%d px of a virtual placement rendered at its anchor", n)
	}
}

// kittyPlaceholders renders n placeholder cells for image id: the first cell
// carries the image row and column as combining diacritics and the rest inherit
// from it, which is how a client emits one row of a virtual placement.
func kittyPlaceholders(id uint32, imgRow, imgCol, n int) string {
	rowMark, _ := purfecterm.KittyDiacriticFor(imgRow)
	colMark, _ := purfecterm.KittyDiacriticFor(imgCol)
	var s strings.Builder
	fmt.Fprintf(&s, "\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	s.WriteRune(purfecterm.KittyPlaceholderRune)
	s.WriteRune(rowMark)
	s.WriteRune(colMark)
	for i := 1; i < n; i++ {
		s.WriteRune(purfecterm.KittyPlaceholderRune)
	}
	s.WriteString("\x1b[0m")
	return s.String()
}

// A virtual placement is drawn where its Unicode placeholder cells are printed,
// filling exactly those cells - not at the anchor it was created with, and not
// at the size it was decoded at.
func TestGfxKittyPlaceholderRendering(t *testing.T) {
	term, b := gfxImageTerm(t)
	buf := term.Terminal().Buffer()
	cellW, cellH := buf.GetCellPixelSize()

	pix := make([]byte, 0, 2*2*4)
	for i := 0; i < 4; i++ {
		pix = append(pix, 255, 0, 0, 255) // opaque red
	}
	term.Feed([]byte("\x1b[1;1H"))
	term.Feed(kittyRGBA(2, 2, pix, ",i=42,c=2,r=1,U=1"))

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))
	red := func(c color.RGBA) bool { return c.R > 200 && c.G < 60 && c.B < 60 }
	if _, _, _, _, n := pixelExtent(b, red); n != 0 {
		t.Fatalf("%d px drawn for a virtual placement with no placeholder cells", n)
	}

	// Two cells at screen row 3, column 1.
	const anchorRow, anchorCol = 3, 1
	term.Feed([]byte(fmt.Sprintf("\x1b[%d;%dH", anchorRow+1, anchorCol+1)))
	term.Feed([]byte(kittyPlaceholders(42, 0, 0, 2)))

	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	x0, y0, x1, y1, n := pixelExtent(b, red)
	if n == 0 {
		t.Fatal("the placeholder cells rendered no image")
	}
	cwPx, chPx := cellPx(term)
	wantX := int(math.Floor(float64(anchorCol) * cwPx))
	wantY := int(math.Floor(float64(anchorRow) * chPx))
	if x0 != wantX || y0 != wantY {
		t.Errorf("image at (%d,%d) px, want (%d,%d): it must land on the placeholder cells",
			x0, y0, wantX, wantY)
	}
	if w, h := x1-x0+1, y1-y0+1; w != 2*cellW || h != cellH {
		t.Errorf("image covers %dx%d px, want %dx%d (two cells wide, one tall)",
			w, h, 2*cellW, cellH)
	}

	// The placeholder cells themselves must not paint a character over it.
	if n != 2*cellW*cellH {
		t.Errorf("image coverage = %d px, want %d: something is drawn on top of it",
			n, 2*cellW*cellH)
	}
}

// gfxImageTermScaled is gfxImageTerm on a surface magnified by scale, standing
// on a screen whose content scale is density. The two are given separately
// BECAUSE they are separate: the magnification is what this application chose,
// the density is what panel it is on, and every interesting case is one where
// they differ.
func gfxImageTermScaled(t *testing.T, scale int, density float64) (*PurfecTerm, *raster.Backend) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.NewScaled(640, 400, scale)
	if err != nil {
		t.Fatal(err)
	}
	b.SetDisplayDensity(density)
	core.SetTextMeasurer(b)

	term := NewPurfecTerm()
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})
	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))
	return term, b
}

// On a HiDPI SCREEN the cell we ADVERTISE is deliberately larger than the one
// we paint, by the screen's density.
//
// The kitty protocol gives a terminal no way to state a display scale - a
// client works density out purely from the pixels-per-cell it is told - so
// claiming a bigger cell is the only way to ask for a denser rendering. A
// browser told a cell is twice its real size lays out half as many css pixels
// in it, which is precisely a device-pixel ratio of 2, and the image it sends
// back is twice the size we draw it at.
func TestGfxOversampledCellIsAdvertisedLarger(t *testing.T) {
	const scale, density = 2, 2.0
	term, _ := gfxImageTermScaled(t, scale, density)
	buf := term.Terminal().Buffer()

	realW, realH := cellPx(term)
	gotW, gotH := buf.GetCellPixelSize()
	wantW := int(math.Round(math.Round(realW) * density))
	wantH := int(math.Round(math.Round(realH) * density))
	if gotW != wantW || gotH != wantH {
		t.Errorf("advertised cell = %dx%d, want the real %vx%v times the density %v = %dx%d",
			gotW, gotH, math.Round(realW), math.Round(realH), density, wantW, wantH)
	}
	// The pointer unit follows the advertised cell, so the mouse stays
	// self-consistent through the oversample (it encodes in these units).
	if pw, ph := buf.GetPointerPixelUnit(); pw != gotW || ph != gotH {
		t.Errorf("pointer unit %dx%d drifted from the advertised cell %dx%d", pw, ph, gotW, gotH)
	}
}

// And the image that comes back is drawn at the size we PAINT, not the size it
// was sent at: an image covering N advertised cells covers N real cells. That
// halving is what turns the child's oversampled render into a crisp one rather
// than a picture spilling past its pane.
func TestGfxOversampledImageDrawsAtRealCellSize(t *testing.T) {
	const scale, density = 2, 2.0
	term, b := gfxImageTermScaled(t, scale, density)

	realW, realH := cellPx(term)
	rcw, rch := int(math.Round(realW)), int(math.Round(realH))
	if rcw <= 0 || rch <= 0 {
		t.Fatalf("no real cell (%vx%v)", realW, realH)
	}
	// Two advertised cells wide and tall - so four REAL cells of source
	// pixels, which must land on two real cells of screen.
	acw, ach := rcw*int(density), rch*int(density)
	imgW, imgH := acw*2, ach*2
	pix := make([]byte, imgW*imgH*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = 0, 255, 0, 255
	}

	term.Feed([]byte("\x1b[1;1H"))
	// c/r place it across exactly two advertised cells, which is what a
	// client sizing against the cell we reported would ask for.
	term.Feed(kittyRGBA(imgW, imgH, pix, ",c=2,r=2"))
	b.Clear(style.DefaultStyle())
	term.Paint(core.NewPainter(b))

	img := b.Image()
	green := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return c.G > 200 && c.R < 80 && c.B < 80
	}
	// Inside two REAL cells: painted.
	if !green(rcw/2, rch/2) {
		t.Error("the image did not paint at its anchor")
	}
	if !green(2*rcw-2, 2*rch-2) {
		t.Error("the image did not cover the two real cells it claims")
	}
	// Beyond them: NOT painted. Undivided, it would have spilled to four.
	if green(2*rcw+rcw/2, rch/2) {
		t.Errorf("the image spilled past two real cells - it was drawn at the "+
			"advertised size (%dx%d) instead of the painted one", imgW, imgH)
	}
	if green(rcw/2, 2*rch+rch/2) {
		t.Error("the image spilled below two real cells")
	}
}

// The oversample path halves an image on its way to the screen, and what that
// halving keeps is the whole of the source, not a sample of it.
//
// A 1px checkerboard is the honest test: averaged, every destination pixel is
// the same mid-tone, because each covers one white pixel and one black. Sampled
// - two of the four read, two dropped - the destination keeps the checker, at
// half the frequency and full contrast. That second picture is the aliasing
// this is here to prevent; it is also indistinguishable from a correct render
// until the source has fine detail, which is exactly when it matters.
func TestGfxDownscaleAveragesRatherThanSamples(t *testing.T) {
	checker := func(w, h int) *image.NRGBA {
		im := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := byte(0)
				if (x+y)%2 == 0 {
					v = 255
				}
				o := im.PixOffset(x, y)
				im.Pix[o], im.Pix[o+1], im.Pix[o+2], im.Pix[o+3] = v, v, v, 255
			}
		}
		return im
	}

	// Exactly halved, and one pixel off exactly halved: a window an odd number
	// of pixels wide produces the second, and ApproxBiLinear's fixed 2x2 tap
	// aliases badly there.
	for _, c := range []struct{ srcW, srcH, dstW, dstH int }{
		{64, 32, 32, 16},
		{65, 33, 33, 17},
	} {
		src := checker(c.srcW, c.srcH)
		dst := image.NewRGBA(image.Rect(0, 0, c.dstW, c.dstH))
		if !boxDownscaleGfx(dst, src, src.Rect) {
			t.Errorf("%dx%d -> %dx%d: declined to average", c.srcW, c.srcH, c.dstW, c.dstH)
			continue
		}
		// The last row/column of an odd source is a half block, so it keeps
		// the checker legitimately; the interior must be flat.
		lo, hi := 255, 0
		for y := 0; y < c.dstH-1; y++ {
			for x := 0; x < c.dstW-1; x++ {
				v := int(dst.RGBAAt(x, y).R)
				if v < lo {
					lo = v
				}
				if v > hi {
					hi = v
				}
			}
		}
		if hi-lo > 2 {
			t.Errorf("%dx%d -> %dx%d: interior ranges %d..%d, want one flat mid-tone: "+
				"the checker survived, so pixels were dropped rather than averaged",
				c.srcW, c.srcH, c.dstW, c.dstH, lo, hi)
		}
		if lo < 120 || hi > 136 {
			t.Errorf("%dx%d -> %dx%d: mid-tone %d..%d, want ~128", c.srcW, c.srcH, c.dstW, c.dstH, lo, hi)
		}
	}
}

// Blocks only apply where they land exactly and actually reduce; everything
// else stays with the resampler, which handles arbitrary ratios.
func TestGfxDownscaleDeclinesWhereBlocksDoNotFit(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for _, c := range []struct {
		dstW, dstH int
		why        string
	}{
		{128, 64, "an upscale"},
		{50, 30, "a ratio under 2 on both axes"},
		{20, 16, "blocks that miss dst's width"},
	} {
		dst := image.NewRGBA(image.Rect(0, 0, c.dstW, c.dstH))
		if boxDownscaleGfx(dst, src, src.Rect) {
			t.Errorf("64x32 -> %dx%d: averaged %s", c.dstW, c.dstH, c.why)
		}
	}
}

// A crop is downscaled from where it sits, not from the image's origin.
func TestGfxDownscaleHonoursTheSourceRect(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			o := src.PixOffset(x, y)
			v := byte(0)
			if x >= 4 && y >= 4 {
				v = 200 // only the bottom-right quadrant is lit
			}
			src.Pix[o], src.Pix[o+1], src.Pix[o+2], src.Pix[o+3] = v, v, v, 255
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if !boxDownscaleGfx(dst, src, image.Rect(4, 4, 8, 8)) {
		t.Fatal("declined the cropped 4x4 -> 2x2")
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if got := dst.RGBAAt(x, y).R; got != 200 {
				t.Errorf("crop pixel (%d,%d) = %d, want 200: averaged from the wrong origin", x, y, got)
			}
		}
	}
}

// The oversample follows the SCREEN, not this application's magnification.
//
// These two numbers were one number for a while, and it read correctly the
// whole time a user's magnification happened to match their panel. It does not
// have to. Someone on a HiDPI screen who asks for a magnification of 1 — "use
// my real pixels, I will pick bigger fonts" — still has a child process that
// asks the window system for the density, gets 2, and renders to it. Deriving
// the correction from the magnification switches it off in exactly that case
// and leaves every picture twice the size it should be, which is the same
// symptom as having no correction at all.
//
// So: vary them independently, and check the advertised cell follows the panel.
func TestGfxOversampleFollowsTheScreenNotTheMagnification(t *testing.T) {
	for _, c := range []struct {
		scale   int
		density float64
		want    float64
	}{
		{1, 2, 2}, // real pixels on a HiDPI panel: the case that was broken
		{2, 2, 2}, // magnification matching the panel: the case that hid it
		{2, 1, 1}, // a magnified view of an ordinary screen: no correction owed
		{1, 1, 1},
	} {
		term, _ := gfxImageTermScaled(t, c.scale, c.density)
		if got := term.gfx.oversample; got != c.want {
			t.Errorf("scale=%d density=%v: oversample %v, want %v",
				c.scale, c.density, got, c.want)
		}
		realW, _ := cellPx(term)
		gotW, _ := term.Terminal().Buffer().GetCellPixelSize()
		if want := int(math.Round(math.Round(realW) * c.want)); gotW != want {
			t.Errorf("scale=%d density=%v: advertised cell %d px, want %d",
				c.scale, c.density, gotW, want)
		}
	}
}
