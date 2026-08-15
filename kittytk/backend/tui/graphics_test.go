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
