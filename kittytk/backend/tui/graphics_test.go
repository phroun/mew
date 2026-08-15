package tui

import (
	"image"
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
	for _, want := range []string{"\033_G", "a=T", "f=32", "s=3", "v=2", "C=1", "m=0", "\033\\"} {
		if !strings.Contains(out, want) {
			t.Errorf("kitty payload missing %q: %q", want, out)
		}
	}
}

// A payload past the chunk limit is split, with m=1 on every piece but the
// last — the protocol requires it, and a terminal drops an overlong one.
func TestKittyChunksLargePayload(t *testing.T) {
	var sb strings.Builder
	// 64x64 RGBA is 16384 bytes -> ~21848 base64 chars -> 6 chunks.
	writeKittyImage(&sb, solidImage(64, 64, color.RGBA{9, 9, 9, 255}))
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

	b.flushImagesLocked(true)
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

// An idle frame must not re-transmit a picture that is already on screen.
//
// A frame is drawn on a heartbeat whether or not anything happened, and a
// picture is not a few bytes of text — re-sending one every frame pushes its
// whole base64 payload down the pty forever for no change at all. Kitty
// placements persist until deleted and sixel pixels are screen content, so
// when neither the images nor the text moved, the screen is already correct.
func TestUnchangedImagesAreNotResentEveryFrame(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	place := func(b *TUIBackend) {
		b.mu.Lock()
		b.pendingImages = []placedImage{{col: 4, row: 2, img: img}}
		b.mu.Unlock()
	}

	var out strings.Builder
	b := &TUIBackend{output: &out}
	b.mu.Lock()
	b.graphics = GraphicsKitty
	b.mu.Unlock()

	place(b)
	b.flushImagesLocked(true) // the frame that first drew it
	first := out.Len()
	if first == 0 {
		t.Fatal("the image was never sent")
	}

	// An idle frame: same picture, no text written.
	out.Reset()
	place(b)
	b.flushImagesLocked(false)
	if out.Len() != 0 {
		t.Errorf("an idle frame re-sent %d bytes for an unchanged picture", out.Len())
	}

	// Text moved: the diff may have painted over it, so it goes again.
	out.Reset()
	place(b)
	b.flushImagesLocked(true)
	if out.Len() == 0 {
		t.Error("the picture was not redrawn after the text diff wrote over the screen")
	}

	// The picture itself moved: always goes again, idle or not.
	out.Reset()
	b.mu.Lock()
	b.pendingImages = []placedImage{{col: 9, row: 2, img: img}}
	b.mu.Unlock()
	b.flushImagesLocked(false)
	if out.Len() == 0 {
		t.Error("a picture that moved was not re-sent")
	}

	// And it going away is a change too — the stale placement must be deleted.
	out.Reset()
	b.flushImagesLocked(false)
	if !strings.Contains(out.String(), "\033_Ga=d,d=A\033\\") {
		t.Errorf("the placement was not deleted when the picture went away: %q", out.String())
	}
}
