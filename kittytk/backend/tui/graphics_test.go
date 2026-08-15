package tui

import (
	"image"

	"github.com/phroun/kittytk/core"
	"image/color"
	"strings"
	"testing"
)

// The kitty query answers about our own probe id. A reply for someone else's
// id, or a failure status, is not a yes.
func TestKittyProbeReplyIsAccepted(t *testing.T) {
	for _, c := range []struct {
		name, key string
		want      int
	}{
		{"our id, OK", "APC:Gi=19284;OK", GraphicsKitty},
		{"our id, failure", "APC:Gi=19284;ENOTSUPP", GraphicsNone},
		{"someone else's id", "APC:Gi=7;OK", GraphicsNone},
		{"not a graphics APC", "APC:Xhello", GraphicsNone},
	} {
		b := &TUIBackend{}
		b.handleAPC(c.key)
		if got := b.TerminalGraphics(); got != c.want {
			t.Errorf("%s: %q -> %d, want %d", c.name, c.key, got, c.want)
		}
	}
}

// Primary DA advertises sixel as attribute 4, among whatever else.
func TestDA1SixelDetection(t *testing.T) {
	for _, c := range []struct {
		key  string
		want int
	}{
		{"DA1:62;4;22", GraphicsSixel},
		{"DA1:4", GraphicsSixel},
		{"DA1:62;22", GraphicsNone}, // no sixel
		{"DA1:64;41", GraphicsNone}, // 41 is not 4
	} {
		b := &TUIBackend{}
		b.handleDA1(c.key)
		if got := b.TerminalGraphics(); got != c.want {
			t.Errorf("%q -> %d, want %d", c.key, got, c.want)
		}
	}
}

// Kitty is preferred over sixel whichever reply lands first: it can place and
// delete by id, where sixel only paints.
func TestKittyWinsOverSixelEitherOrder(t *testing.T) {
	b := &TUIBackend{}
	b.handleDA1("DA1:62;4;22")
	b.handleAPC("APC:Gi=19284;OK")
	if got := b.TerminalGraphics(); got != GraphicsKitty {
		t.Errorf("sixel then kitty -> %d, want kitty", got)
	}

	b = &TUIBackend{}
	b.handleAPC("APC:Gi=19284;OK")
	b.handleDA1("DA1:62;4;22")
	if got := b.TerminalGraphics(); got != GraphicsKitty {
		t.Errorf("kitty then sixel -> %d, want kitty", got)
	}
}

// A terminal that answered SOMETHING is not second-guessed from the
// environment; one that answered nothing at all is.
func TestEnvFallbackOnlyWhenNothingAnswered(t *testing.T) {
	b := &TUIBackend{}
	b.handleDA1("DA1:62;22") // answered: no sixel
	t.Setenv("KITTY_WINDOW_ID", "1")
	b.resolveGraphicsFallback()
	if got := b.TerminalGraphics(); got != GraphicsNone {
		t.Errorf("a terminal that answered was overridden by the environment: %d", got)
	}

	b = &TUIBackend{} // answered nothing
	b.resolveGraphicsFallback()
	if got := b.TerminalGraphics(); got != GraphicsKitty {
		t.Errorf("silent terminal -> %d, want the environment's kitty", got)
	}
}

// With no protocol, an image is dropped rather than emitting escape noise
// into a terminal that cannot read it.
func TestNoProtocolDrawsNothing(t *testing.T) {
	b := &TUIBackend{graphics: GraphicsNone}
	b.DrawImage(0, 0, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if len(b.pendingImages) != 0 {
		t.Errorf("image recorded with no protocol: %v", b.pendingImages)
	}
}

// solidImage is an opaque w x h block of one color.
func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// The kitty encoding transmits and displays in one command, sized in source
// pixels, leaving the cursor alone (C=1) so having drawn a picture does not
// move the text layout the caller computed.
func TestKittyEncodingShape(t *testing.T) {
	var sb strings.Builder
	writeKittyImage(&sb, solidImage(3, 2, color.RGBA{1, 2, 3, 255}))
	out := sb.String()
	// f=24 rather than f=32: a solid opaque picture carries no alpha worth
	// sending. TestKittyPayloadIsCompressed covers the choice between them.
	for _, want := range []string{"\033_G", "a=T", "f=24", "s=3", "v=2", "C=1", "m=0", "\033\\"} {
		if !strings.Contains(out, want) {
			t.Errorf("kitty payload missing %q: %q", want, out)
		}
	}
}

// A payload past the chunk limit is split, with m=1 on every piece but the
// last — the protocol requires it, and a terminal drops an overlong one.
func TestKittyChunksLargePayload(t *testing.T) {
	var sb strings.Builder
	// INCOMPRESSIBLE, so the payload is still large after o=z. A solid block
	// used to make this test's point and no longer does: it compresses to a
	// few hundred bytes and arrives in one chunk, which is the improvement
	// working rather than the chunking breaking.
	noise := image.NewRGBA(image.Rect(0, 0, 128, 128))
	seed := uint32(12345)
	for i := range noise.Pix {
		seed = seed*1664525 + 1013904223
		noise.Pix[i] = byte(seed >> 24)
	}
	writeKittyImage(&sb, noise)
	out := sb.String()
	if n := strings.Count(out, "\033_G"); n < 2 {
		t.Fatalf("large image was not chunked (%d commands)", n)
	}
	if strings.Count(out, "m=1") != strings.Count(out, "\033_G")-1 {
		t.Errorf("every chunk but the last must carry m=1: %d of %d",
			strings.Count(out, "m=1"), strings.Count(out, "\033_G"))
	}
	if !strings.Contains(out, "m=0") {
		t.Error("the final chunk must carry m=0")
	}
}

// Sixel opens with DCS q, declares the color registers it uses, and closes
// with ST. A fully transparent pixel is left unset rather than painted, so a
// cut-out composites over the text instead of blanking a rectangle.
func TestSixelEncodingShape(t *testing.T) {
	var sb strings.Builder
	writeSixelImage(&sb, solidImage(4, 6, color.RGBA{255, 0, 0, 255}))
	out := sb.String()
	if !strings.HasPrefix(out, "\033Pq") {
		t.Errorf("sixel must open with DCS q: %q", out[:min(8, len(out))])
	}
	if !strings.HasSuffix(out, "\033\\") {
		t.Error("sixel must close with ST")
	}
	if !strings.Contains(out, "#180;2;100;0;0") {
		t.Errorf("pure red should declare register 180 at 100%%,0,0: %q", out)
	}

	// Transparent image: registers get declared for nothing, and no sixel
	// data character above the empty 0x3F is emitted.
	sb.Reset()
	writeSixelImage(&sb, image.NewRGBA(image.Rect(0, 0, 4, 6)))
	body := strings.TrimSuffix(strings.TrimPrefix(sb.String(), "\033Pq"), "\033\\")
	if strings.ContainsAny(body, "@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmno~") {
		t.Errorf("transparent image painted pixels: %q", body)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// The outer terminal's cell size is not just this backend's business: it is
// the answer a program hosted in a pane needs for its OWN CSI 16 t.
//
// A child sizes, positions and scales every picture in pixels-per-cell, and
// there is no other channel for geometry in the terminal protocols. Told
// nothing, it draws nothing — which is precisely how chafa failed here for
// every format at once while working on the graphical host, where the cell
// comes from the font.
func TestCellPixelSizeIsReportedOnceTheTerminalAnswers(t *testing.T) {
	b := &TUIBackend{}
	// Before the reply: unknown, and it says so rather than guessing.
	if w, h := b.CellPixelSize(); w != 0 || h != 0 {
		t.Errorf("cell size = %dx%d before the query was answered, want 0x0", w, h)
	}

	b.mu.Lock()
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	b.mu.Unlock()
	if w, h := b.CellPixelSize(); w != 10 || h != 20 {
		t.Errorf("cell size = %dx%d, want the outer terminal's 10x20", w, h)
	}

	// A terminal that replied with nonsense is still unknown: half a cell
	// size is worse than none, because a child will believe it.
	b.mu.Lock()
	b.outerCellW, b.outerCellH = 0, 20
	b.mu.Unlock()
	if w, h := b.CellPixelSize(); w != 0 || h != 0 {
		t.Errorf("cell size = %dx%d from a zero width, want 0x0", w, h)
	}
}

// An image queued at a pixel anchor lands on the cell containing it, and only
// when the outer terminal can draw pictures at all.
func TestDrawImageQueuesByCellAndOnlyWhenSupported(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))

	// No graphics protocol: dropped, not queued for a terminal that would
	// print the escape as text.
	b := &TUIBackend{}
	b.mu.Lock()
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	b.mu.Unlock()
	b.DrawImagePx(35, 45, img)
	if n := len(b.pendingImages); n != 0 {
		t.Errorf("queued %d images with no graphics protocol, want 0", n)
	}

	b.mu.Lock()
	b.graphics = GraphicsKitty
	b.mu.Unlock()
	b.DrawImagePx(35, 45, img)
	if n := len(b.pendingImages); n != 1 {
		t.Fatalf("queued %d images, want 1", n)
	}
	if p := b.pendingImages[0]; p.col != 3 || p.row != 2 {
		t.Errorf("anchored at cell (%d,%d), want (3,2) for pixel (35,45) at a 10x20 cell",
			p.col, p.row)
	}
}

// When the escape query goes unanswered, the cell size comes from the kernel's
// winsize instead — and the reply still wins when it does arrive.
//
// The two channels fail differently, which is the whole reason both exist. A
// CSI 16 t reply has to be recognised by the terminal AND survive the trip back
// through whatever sits between; a multiplexer swallows it and a terminal that
// does not know the sequence says nothing. TIOCGWINSZ asks the kernel about our
// own tty and cannot be intercepted — though plenty of terminals leave the
// pixel fields at zero, so neither channel alone is enough.
func TestCellPixelSizeFallsBackToTheWinsize(t *testing.T) {
	restore := ttyPixelSizeFn
	t.Cleanup(func() { ttyPixelSizeFn = restore })
	ttyPixelSizeFn = func(int) (int, int) { return 800, 500 } // 80x25 at 10x20

	b := &TUIBackend{}
	b.cols, b.rows = 80, 25
	if w, h := b.CellPixelSize(); w != 10 || h != 20 {
		t.Errorf("cell size = %dx%d from an 800x500 window on an 80x25 grid, want 10x20", w, h)
	}

	// The terminal's own answer is authoritative: the winsize division is
	// whole-number and loses the remainder, where the reply is exact.
	b.mu.Lock()
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 11, 23, true
	b.mu.Unlock()
	if w, h := b.CellPixelSize(); w != 11 || h != 23 {
		t.Errorf("cell size = %dx%d, want the terminal's own 11x23 over the winsize", w, h)
	}

	// A terminal that fills in neither is still unknown, and says so.
	ttyPixelSizeFn = func(int) (int, int) { return 0, 0 }
	b2 := &TUIBackend{}
	b2.cols, b2.rows = 80, 25
	if w, h := b2.CellPixelSize(); w != 0 || h != 0 {
		t.Errorf("cell size = %dx%d with neither channel answering, want 0x0", w, h)
	}
}

// Placing a picture means moving the cursor, so the flush has to put it back.
//
// The text diff positions the caret just before this runs. Leaving it parked
// on the last image's anchor makes the terminal draw its own cursor block
// there — a solid cell of it, in the corner of the picture.
func TestImageFlushRestoresTheCursor(t *testing.T) {
	var out strings.Builder
	b := &TUIBackend{output: &out}
	b.mu.Lock()
	b.graphics = GraphicsSixel
	b.pendingImages = []placedImage{{col: 4, row: 2, img: image.NewRGBA(image.Rect(0, 0, 2, 2))}}
	b.mu.Unlock()

	b.flushImagesLocked()
	got := out.String()
	if !strings.HasPrefix(got, "\0337") {
		t.Errorf("the flush did not save the cursor first: %q", firstBytes(got))
	}
	if !strings.HasSuffix(got, "\0338") {
		t.Errorf("the flush did not restore the cursor: it ends %q", lastBytes(got))
	}
	// And it really did move it, which is why the restore is needed.
	if !strings.Contains(got, "\033[3;5H") {
		t.Error("the image was not addressed to its cell")
	}
}

func firstBytes(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func lastBytes(s string) string {
	if len(s) > 12 {
		return s[len(s)-12:]
	}
	return s
}

// An idle frame must not re-transmit a picture that is already on screen, and
// under kitty neither must a busy one.
//
// A frame is drawn on a heartbeat whether or not anything happened, and a
// picture is not a few bytes of text. Kitty placements persist until deleted —
// the same persistence that makes an image survive a `clear` — so text painted
// over those cells composites against them rather than destroying them, and no
// amount of text is a reason to send pixels again.
func TestKittyImagesSurviveTextWithoutBeingResent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var out strings.Builder
	b := &TUIBackend{output: &out, cols: 80, rows: 25}
	b.dmgMin = make([]int, b.rows)
	b.dmgMax = make([]int, b.rows)
	b.graphics = GraphicsKitty
	place := func() {
		b.pendingImages = []placedImage{{col: 4, row: 2, img: img}}
	}

	place()
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Fatal("the image was never sent")
	}

	// An idle frame.
	out.Reset()
	place()
	b.flushImagesLocked()
	if out.Len() != 0 {
		t.Errorf("an idle frame re-sent %d bytes for an unchanged picture", out.Len())
	}

	// A busy frame that repainted the very cells the image sits on. Kitty does
	// not care: the placement is still there, under or over the text per its z.
	out.Reset()
	for y := range b.dmgMin {
		b.dmgMin[y], b.dmgMax[y] = 0, b.cols-1
	}
	place()
	b.flushImagesLocked()
	if out.Len() != 0 {
		t.Errorf("a text repaint re-sent %d bytes under kitty, where placements persist", out.Len())
	}

	// Moving it is a real change.
	out.Reset()
	b.pendingImages = []placedImage{{col: 9, row: 2, img: img}}
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Error("a picture that moved was not re-sent")
	}
}

// Sixel has no placement to persist: the pixels ARE screen content, so text
// painted over them erases that part. It goes again — but only when the diff
// actually touched the cells it covers. A change on an unrelated row is not a
// reason to re-transmit a picture.
func TestSixelImageResentOnlyWhenItsOwnCellsAreRepainted(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 40)) // 2x2 cells at 10x20
	var out strings.Builder
	b := &TUIBackend{output: &out, cols: 80, rows: 25}
	b.dmgMin = make([]int, b.rows)
	b.dmgMax = make([]int, b.rows)
	b.graphics = GraphicsSixel
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	clearDamage := func() {
		for y := range b.dmgMin {
			b.dmgMin[y], b.dmgMax[y] = -1, -1
		}
	}
	place := func() { b.pendingImages = []placedImage{{col: 4, row: 2, img: img}} }

	clearDamage()
	place()
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Fatal("the image was never sent")
	}

	// Text on a row the picture does not reach.
	out.Reset()
	clearDamage()
	b.markDamage(20, 0, 79)
	place()
	b.flushImagesLocked()
	if out.Len() != 0 {
		t.Errorf("text on an unrelated row re-sent %d bytes", out.Len())
	}

	// Text on the picture's rows but well to the right of it.
	out.Reset()
	clearDamage()
	b.markDamage(2, 40, 79)
	place()
	b.flushImagesLocked()
	if out.Len() != 0 {
		t.Errorf("text on the same row but a distant column re-sent %d bytes", out.Len())
	}

	// Text ON it: the sixel pixels there were overwritten, so it goes again.
	out.Reset()
	clearDamage()
	b.markDamage(3, 5, 5)
	place()
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Error("text painted over the picture did not cause it to be redrawn")
	}
}

// A sixel is cell content — an expensive glyph — so the repair for text
// painted over it is the CELLS that were rewritten, not the whole picture.
//
// The emitter honours Bounds(), so a crop costs nothing extra to send; what it
// saves is everything else. A one-cell overwrite in the corner of a big
// picture used to re-transmit the entire thing.
func TestSixelRepaintsOnlyTheDamagedCells(t *testing.T) {
	// 8x4 cells at 10x20 px.
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	var out strings.Builder
	b := &TUIBackend{output: &out, cols: 80, rows: 25}
	b.dmgMin, b.dmgMax = make([]int, b.rows), make([]int, b.rows)
	b.graphics = GraphicsSixel
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	clear := func() {
		for y := range b.dmgMin {
			b.dmgMin[y], b.dmgMax[y] = -1, -1
		}
	}
	place := func() { b.pendingImages = []placedImage{{col: 4, row: 2, img: img}} }

	clear()
	place()
	b.flushImagesLocked()
	whole := out.Len()
	if whole == 0 {
		t.Fatal("the image was never sent")
	}

	// One cell overwritten, two rows down and one column in.
	out.Reset()
	clear()
	b.markDamage(3, 5, 5)
	place()
	b.flushImagesLocked()
	patched := out.Len()
	if patched == 0 {
		t.Fatal("the damaged cell was not repaired")
	}
	if patched >= whole {
		t.Errorf("a one-cell repair sent %d bytes against %d for the whole picture: "+
			"it is re-transmitting everything", patched, whole)
	}
	// Addressed at the damaged cell, not the picture's anchor.
	if !strings.Contains(out.String(), "\033[4;6H") {
		t.Errorf("the patch was not addressed to the damaged cell (4;6): %q", out.String()[:24])
	}
}

// iTerm2 renders kitty graphics from 3.5 on but does not answer the query we
// probe with, so the environment has to speak for it — and only for versions
// that can, since a wrong guess prints an escape sequence as text.
func TestITermVersionGatesKittyFallback(t *testing.T) {
	for _, c := range []struct {
		version string
		want    int
	}{
		{"3.5.0", GraphicsKitty},
		{"3.6.9", GraphicsKitty},
		{"4.0", GraphicsKitty},
		{"3.4.23", GraphicsNone}, // too old: DA1 offers sixel instead
		{"3.4", GraphicsNone},
		{"2.9.9", GraphicsNone},
		{"", GraphicsNone},
		{"nonsense", GraphicsNone},
	} {
		t.Setenv("TERM_PROGRAM", "iTerm.app")
		t.Setenv("TERM_PROGRAM_VERSION", c.version)
		t.Setenv("KITTY_WINDOW_ID", "")
		t.Setenv("TERM", "xterm-256color")
		if got := graphicsFromEnv(); got != c.want {
			t.Errorf("iTerm2 %q -> %d, want %d", c.version, got, c.want)
		}
	}
}

// A picture is confined to the clip in force when it is drawn, the same clip
// text obeys — otherwise it runs past the edge of the window that drew it and
// keeps going, because nothing downstream knows where the window ended.
func TestImageIsCroppedToTheClip(t *testing.T) {
	b := &TUIBackend{cols: 80, rows: 25}
	b.metrics = core.CellMetrics{CellWidth: 8, CellHeight: 16}
	b.graphics = GraphicsKitty
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	// A window occupying cells (2,1)..(11,5): 10 cells wide, 5 tall.
	b.clipRect = core.UnitRect{X: 16, Y: 16, Width: 80, Height: 80}

	// An 8x8-cell picture anchored at (4,2) — it runs off the clip's bottom
	// and right by 1 and 5 cells.
	img := image.NewRGBA(image.Rect(0, 0, 80, 160))
	b.queueImageLocked(4, 2, img)
	if len(b.pendingImages) != 1 {
		t.Fatalf("queued %d images, want 1", len(b.pendingImages))
	}
	got := b.pendingImages[0]
	if got.col != 4 || got.row != 2 {
		t.Errorf("anchor moved to (%d,%d), want (4,2): only the far edges are out", got.col, got.row)
	}
	if w := got.img.Bounds().Dx(); w != 80 {
		t.Errorf("width %d px, want 80: cols 4..11 all fit the clip", w)
	}
	if h := got.img.Bounds().Dy(); h != 80 {
		t.Errorf("height %d px, want 80: rows 2..5 fit, rows 6..9 are past the clip", h)
	}

	// Entirely outside: nothing queued at all.
	b.pendingImages = nil
	b.queueImageLocked(40, 20, image.NewRGBA(image.Rect(0, 0, 20, 20)))
	if len(b.pendingImages) != 0 {
		t.Errorf("queued %d images wholly outside the clip, want 0", len(b.pendingImages))
	}
}

// A window painted over a picture takes those cells away from it.
//
// Trinkets paint back to front into one plane, so "on top" is "written later".
// An image is queued rather than composited, so it has to ask after the fact —
// and without asking, it is emitted over the very window that covers it.
func TestImageIsOccludedByWhatIsPaintedOverIt(t *testing.T) {
	b := &TUIBackend{cols: 80, rows: 25}
	b.metrics = core.CellMetrics{CellWidth: 8, CellHeight: 16}
	b.graphics = GraphicsKitty
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	b.dmgMin, b.dmgMax = make([]int, b.rows), make([]int, b.rows)
	b.cellSeq = make([][]uint32, b.rows)
	for y := range b.cellSeq {
		b.cellSeq[y] = make([]uint32, b.cols)
	}

	// A 4x4-cell picture at (2,2), queued at paint order 100.
	img := image.NewRGBA(image.Rect(0, 0, 40, 80))
	p := placedImage{col: 2, row: 2, img: img, seq: 100}

	// Nothing over it: one block, unsplit.
	if got := b.visibleBlocksLocked(p); len(got) != 1 || got[0].img != img {
		t.Fatalf("an unobstructed picture split into %d blocks, want 1 whole one", len(got))
	}

	// A window painted LATER across its bottom-right quarter.
	for y := 4; y <= 5; y++ {
		for x := 4; x <= 5; x++ {
			b.cellSeq[y][x] = 200
		}
	}
	blocks := b.visibleBlocksLocked(p)
	if len(blocks) != 2 {
		t.Fatalf("split into %d blocks, want 2 (the full rows above, the left half below)", len(blocks))
	}
	// Rows 2-3 survive whole; rows 4-5 keep only columns 2-3.
	if blocks[0].row != 2 || blocks[0].col != 2 || blocks[0].img.Bounds().Dy() != 40 {
		t.Errorf("first block at (%d,%d) %v, want rows 2-3 across the full width",
			blocks[0].col, blocks[0].row, blocks[0].img.Bounds())
	}
	if blocks[1].row != 4 || blocks[1].col != 2 || blocks[1].img.Bounds().Dx() != 20 {
		t.Errorf("second block at (%d,%d) %v, want rows 4-5 columns 2-3 only",
			blocks[1].col, blocks[1].row, blocks[1].img.Bounds())
	}

	// Buried completely: nothing to draw.
	for y := 2; y <= 5; y++ {
		for x := 2; x <= 5; x++ {
			b.cellSeq[y][x] = 200
		}
	}
	if got := b.visibleBlocksLocked(p); len(got) != 0 {
		t.Errorf("a fully covered picture produced %d blocks, want none", len(got))
	}
}

// Content painted BEFORE the image does not occlude it — that is the desktop
// the window sits on, not something covering it.
func TestEarlierPaintDoesNotOccludeAnImage(t *testing.T) {
	b := &TUIBackend{cols: 80, rows: 25}
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	b.cellSeq = make([][]uint32, b.rows)
	for y := range b.cellSeq {
		b.cellSeq[y] = make([]uint32, b.cols)
		for x := range b.cellSeq[y] {
			b.cellSeq[y][x] = 50 // the desktop, painted first
		}
	}
	p := placedImage{col: 2, row: 2, img: image.NewRGBA(image.Rect(0, 0, 40, 80)), seq: 100}
	if got := b.visibleBlocksLocked(p); len(got) != 1 {
		t.Errorf("the desktop underneath split the picture into %d blocks, want 1", len(got))
	}
}

// A menu opening over an unchanged picture must redraw it, cropped.
//
// The placement list is identical in that frame — same picture, same anchor —
// so comparing placements calls it a repeat and skips the flush, leaving the
// PREVIOUS full-size placement on screen and on top of the menu it should be
// under. What goes on screen is the visible blocks, so those are what a repeat
// has to be judged on.
func TestOcclusionChangeCountsAsAChange(t *testing.T) {
	var out strings.Builder
	b := &TUIBackend{output: &out, cols: 40, rows: 20}
	b.metrics = core.CellMetrics{CellWidth: 8, CellHeight: 16}
	b.graphics = GraphicsKitty
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	b.allocateBuffers()

	img := image.NewRGBA(image.Rect(0, 0, 40, 80)) // 4x4 cells
	queue := func() {
		b.pendingImages = []placedImage{{col: 2, row: 2, img: img, seq: 100}}
	}

	queue()
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Fatal("the picture was never sent")
	}

	// The identical frame again: nothing to do.
	out.Reset()
	queue()
	b.flushImagesLocked()
	if out.Len() != 0 {
		t.Errorf("an unchanged frame re-sent %d bytes", out.Len())
	}

	// A menu opens over its bottom-right. The placement list has not moved.
	out.Reset()
	for y := 4; y <= 5; y++ {
		for x := 4; x <= 5; x++ {
			b.cellSeq[y][x] = 200
		}
	}
	queue()
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Fatal("a menu opened over the picture and nothing was redrawn: the stale " +
			"placement stays on screen, on top of the menu")
	}
	// It went again as two blocks, not one whole picture.
	if n := strings.Count(out.String(), "\033_G"); n < 3 { // 1 delete + 2 placements
		t.Errorf("emitted %d kitty commands, want a delete plus two blocks", n)
	}

	// And once the menu closes, the whole picture returns.
	out.Reset()
	for y := 4; y <= 5; y++ {
		for x := 4; x <= 5; x++ {
			b.cellSeq[y][x] = 0
		}
	}
	queue()
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Error("the picture was not restored when the menu closed")
	}
}

// A CLIPPED picture that has not changed must not be re-transmitted.
//
// Cropping makes a new SubImage wrapper every time it runs, and a clipped
// picture is re-cropped on every frame — so comparing wrappers answers
// "different" forever and the whole payload goes again for as long as it is on
// screen. Which is every picture in a window, since a window clips.
func TestClippedImageIsNotResentEveryFrame(t *testing.T) {
	var out strings.Builder
	b := &TUIBackend{output: &out, cols: 40, rows: 20}
	b.metrics = core.CellMetrics{CellWidth: 8, CellHeight: 16}
	b.graphics = GraphicsKitty
	b.outerCellW, b.outerCellH, b.outerCellSizeOK = 10, 20, true
	b.allocateBuffers()
	b.clipRect = core.UnitRect{X: 0, Y: 0, Width: 320, Height: 80} // 40x5 cells

	img := image.NewRGBA(image.Rect(0, 0, 40, 160)) // 4x8 cells: 3 rows clipped off
	frame := func() int {
		out.Reset()
		b.mu.Lock()
		b.queueImageLocked(2, 2, img)
		b.mu.Unlock()
		b.flushImagesLocked()
		return out.Len()
	}

	if first := frame(); first == 0 {
		t.Fatal("the clipped picture was never sent")
	}
	if n := frame(); n != 0 {
		t.Errorf("an unchanged clipped picture re-sent %d bytes", n)
	}
	if n := frame(); n != 0 {
		t.Errorf("still re-sending on the third frame: %d bytes", n)
	}

	// Moving it is still a change.
	out.Reset()
	b.mu.Lock()
	b.queueImageLocked(6, 2, img)
	b.mu.Unlock()
	b.flushImagesLocked()
	if out.Len() == 0 {
		t.Error("a clipped picture that moved was not re-sent")
	}
}

// Identity is by pixels, not by wrapper: two crops of the same parent over the
// same rectangle are the same picture, and over different rectangles are not.
func TestSameImageComparesPixelsNotWrappers(t *testing.T) {
	parent := image.NewRGBA(image.Rect(0, 0, 40, 40))
	r := image.Rect(10, 10, 30, 30)
	a := parent.SubImage(r)
	c := parent.SubImage(r) // a distinct wrapper over the same pixels
	if a == c {
		t.Skip("SubImage returned the same wrapper; the test cannot show anything")
	}
	if !sameImage(a, c) {
		t.Error("two crops of the same parent over the same rectangle read as different")
	}
	if sameImage(a, parent.SubImage(image.Rect(0, 0, 20, 20))) {
		t.Error("crops over different rectangles read as the same")
	}
	if sameImage(a, image.NewRGBA(r)) {
		t.Error("a different buffer with the same bounds read as the same")
	}
}

// Kitty payloads go compressed, and drop the alpha channel when there is
// nothing in it.
//
// Raw pixels base64'd is the most expensive thing this file does: a
// full-window frame measures 4.83 MB on the wire that way, and every byte
// crosses a pty and is decoded by the terminal before anything appears. A
// picture that changes often — a browser in a pane — lives or dies on it.
func TestKittyPayloadIsCompressed(t *testing.T) {
	// Web-page-ish: flat background with text-like runs. Compresses well, as
	// real screen content does; random noise would prove nothing.
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			c := color.RGBA{13, 17, 23, 255}
			if y%22 < 10 && x%9 < 5 {
				c = color.RGBA{230, 237, 243, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}

	var sb strings.Builder
	writeKittyImage(&sb, img)
	got := sb.String()
	if !strings.Contains(got, ",o=z") {
		t.Error("the payload was not compressed (no o=z)")
	}
	if !strings.Contains(got, "f=24") {
		t.Error("an opaque picture was sent as RGBA; the alpha channel is a quarter of it")
	}
	// The uncompressed form is 4 bytes a pixel, base64'd.
	rawB64 := (400 * 300 * 4) * 4 / 3
	if len(got) > rawB64/4 {
		t.Errorf("payload is %d bytes against %d uncompressed: less than a 4x saving on "+
			"content that compresses 30x", len(got), rawB64)
	}

	// Partial alpha still goes as RGBA — dropping it would lose the blend.
	img.SetRGBA(10, 10, color.RGBA{255, 0, 0, 128})
	sb.Reset()
	writeKittyImage(&sb, img)
	if strings.Contains(sb.String(), "f=24") {
		t.Error("a picture with partial alpha was sent as RGB, losing the alpha")
	}
	if !strings.Contains(sb.String(), "f=32") {
		t.Error("expected RGBA for a picture with partial alpha")
	}
}

// Motion tracking is asked for per frame and only turned on while wanted.
//
// The outer terminal reports motion with no button held only under ?1003, so
// without asking, those events do not exist and cannot be forwarded to anyone
// — which is why a hosted browser saw no hover at all. It is off by default
// because it is not free: every pixel the pointer crosses becomes a report.
func TestMotionTrackingFollowsDemand(t *testing.T) {
	var tty strings.Builder
	b := &TUIBackend{ttyOut: &tty, cols: 10, rows: 5}
	b.allocateBuffers()

	// Nobody asked: nothing said.
	b.BeginFrame()
	b.mu.Lock()
	b.applyMotionTrackingLocked()
	b.mu.Unlock()
	if tty.Len() != 0 {
		t.Errorf("enabled motion with nothing asking: %q", tty.String())
	}

	// Someone asks: enabled once.
	tty.Reset()
	b.BeginFrame()
	b.RequestMotionTracking()
	b.mu.Lock()
	b.applyMotionTrackingLocked()
	b.mu.Unlock()
	if got := tty.String(); got != "\033[?1003h" {
		t.Errorf("wrote %q, want the enable", got)
	}

	// Still asking: nothing more on the wire.
	tty.Reset()
	b.BeginFrame()
	b.RequestMotionTracking()
	b.mu.Lock()
	b.applyMotionTrackingLocked()
	b.mu.Unlock()
	if tty.Len() != 0 {
		t.Errorf("re-enabled an already-enabled mode: %q", tty.String())
	}

	// Stops asking: turned back off, so the wire goes quiet again.
	tty.Reset()
	b.BeginFrame()
	b.mu.Lock()
	b.applyMotionTrackingLocked()
	b.mu.Unlock()
	if got := tty.String(); got != "\033[?1003l" {
		t.Errorf("wrote %q, want the disable once nothing wanted motion", got)
	}
}
