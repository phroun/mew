package tui

// Passing pictures through to the OUTER terminal.
//
// The graphical hosts composite an image into their own framebuffer. A
// terminal host has no framebuffer: its surface is somebody else's terminal,
// reached only through a byte stream. But that terminal can very likely draw
// a picture itself, so the honest way to implement core.ImageDrawer here is
// to hand the image on in a protocol the outer terminal understands.
//
// Two are supported, in preference order:
//
//   - the KITTY graphics protocol (APC _G), which places an image by id at
//     the cursor and can delete it again, so a placement has a lifetime we
//     can manage;
//   - SIXEL (DCS q), which has no placement model at all - the pixels become
//     screen content at the cursor, like very wide text.
//
// Which one is available is asked of the terminal at startup, alongside the
// pixel-mouse probe next door, and falls back to what the environment says
// when nothing answers.

import (
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"

	"github.com/phroun/kittytk/core"
)

// The graphics protocol this backend will speak to the outer terminal.
const (
	GraphicsNone  = iota // no pictures; DrawImage is a no-op
	GraphicsKitty        // kitty graphics protocol (APC _G)
	GraphicsSixel        // sixel (DCS q)
)

// graphicsProbeID is the image id the startup query uses. Odd and distinctive
// so a reply is recognisably ours; a terminal answers about this id without
// anything being displayed.
const graphicsProbeID = 0x4B54 // "KT"

// maxKittyChunk is the payload size per APC chunk. The protocol requires
// chunking above 4096 base64 bytes.
const maxKittyChunk = 4096

// placedImage is one image the paint pass asked for, waiting to be emitted
// after the text diff (see flushImages).
type placedImage struct {
	col, row int // 0-based cell anchor
	img      image.Image
}

// ttyPixelSizeFn is the winsize probe, indirected so a test can supply an
// answer without a terminal attached.
var ttyPixelSizeFn = ttyPixelSize

// TerminalGraphics reports the protocol this backend will use to show an
// image on the outer terminal, one of the Graphics* constants.
func (t *TUIBackend) TerminalGraphics() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.graphics
}

// probeGraphics asks the outer terminal what it can draw. Both queries are
// answered asynchronously - they arrive as DA1:/APC: keys once the keyboard
// reader is running - so this must be called after Start(), exactly like the
// pixel-mouse probe it sits beside.
//
// The kitty query transmits a 1x1 RGB image by direct payload and asks for
// the result; a terminal that implements the protocol answers OK, and one
// that does not answers nothing at all. DA1 comes second because its reply
// is the fallback signal: attribute 4 means sixel.
func (t *TUIBackend) probeGraphics() {
	t.writeTTY("\033_Gi=" + strconv.Itoa(graphicsProbeID) +
		",s=1,v=1,a=q,t=d,f=24;" + base64.StdEncoding.EncodeToString([]byte{0, 0, 0}) + "\033\\")
	t.writeTTY("\033[c")
}

// handleAPC consumes an "APC:<body>" reply. The only APC this backend asks
// for is the kitty graphics query, whose success reply is "Gi=<id>;OK"; any
// other status for our id (ENOTSUPP, EBADF, ...) is a terminal saying it
// cannot, which is as useful an answer and equally final.
func (t *TUIBackend) handleAPC(key string) {
	body := strings.TrimPrefix(key, "APC:")
	if !strings.HasPrefix(body, "G") {
		return
	}
	id, status, ok := strings.Cut(body[1:], ";")
	if !ok || !strings.Contains(id, "i="+strconv.Itoa(graphicsProbeID)) {
		return
	}
	if status != "OK" {
		return
	}
	t.mu.Lock()
	// Kitty wins over a sixel answer that may already have arrived: it can
	// place and delete by id, where sixel only paints.
	t.graphics = GraphicsKitty
	t.graphicsAnswered = true
	t.mu.Unlock()
}

// handleDA1 consumes a "DA1:<attrs>" primary Device Attributes reply.
// Attribute 4 is sixel. This never downgrades a kitty answer.
func (t *TUIBackend) handleDA1(key string) {
	attrs := strings.Split(strings.TrimPrefix(key, "DA1:"), ";")
	sixel := false
	for _, a := range attrs {
		if a == "4" {
			sixel = true
			break
		}
	}
	t.mu.Lock()
	if sixel && t.graphics == GraphicsNone {
		t.graphics = GraphicsSixel
	}
	t.graphicsAnswered = true
	t.mu.Unlock()
}

// graphicsFromEnv is the fallback for a terminal that answered neither query
// - a multiplexer swallowing them, or one that simply ignores what it does
// not know. It is deliberately conservative: a wrong guess here draws garbage
// into somebody's terminal, so only names that are unambiguous count.
func graphicsFromEnv() int {
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return GraphicsKitty
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "ghostty", "wezterm":
		return GraphicsKitty
	}
	term := strings.ToLower(os.Getenv("TERM"))
	switch {
	case strings.Contains(term, "kitty"), strings.Contains(term, "ghostty"):
		return GraphicsKitty
	case strings.Contains(term, "sixel"), strings.Contains(term, "foot"),
		strings.Contains(term, "mlterm"), strings.Contains(term, "yaft"):
		return GraphicsSixel
	}
	return GraphicsNone
}

// resolveGraphicsFallback settles on the environment's answer if the probes
// never came back. Called once the reply window has passed.
func (t *TUIBackend) resolveGraphicsFallback() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.graphicsAnswered && t.graphics == GraphicsNone {
		t.graphics = graphicsFromEnv()
	}
}

// DrawImage implements core.ImageDrawer: it records the image at a unit
// position for emission after this frame's text.
//
// It cannot draw immediately. The screen is painted as a diff of the cell
// buffer and flushed in one write at the end, so an image emitted mid-paint
// would be overwritten by text addressed after it. Recording and emitting
// last also matches how the picture should sit: over the row the text layer
// has already made room for.
func (t *TUIBackend) DrawImage(x, y core.Unit, img image.Image) {
	if img == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.graphics == GraphicsNone {
		return
	}
	t.pendingImages = append(t.pendingImages, placedImage{
		col: t.metrics.UnitsToCellX(x),
		row: t.metrics.UnitsToCellY(y),
		img: img,
	})
}

// DrawImagePx implements core.ImageDrawer's device-pixel anchor. The outer
// terminal places images by CELL, so a sub-cell offset cannot be honoured;
// the anchor lands on the cell containing it. Falls back to the unit path
// when the outer cell size is unknown.
func (t *TUIBackend) DrawImagePx(xPx, yPx int, img image.Image) {
	if img == nil {
		return
	}
	t.mu.Lock()
	cw, ch := t.outerCellW, t.outerCellH
	known := t.outerCellSizeOK
	none := t.graphics == GraphicsNone
	t.mu.Unlock()
	if none {
		return
	}
	if !known || cw <= 0 || ch <= 0 {
		t.DrawImage(core.Unit(xPx), core.Unit(yPx), img)
		return
	}
	t.mu.Lock()
	t.pendingImages = append(t.pendingImages, placedImage{col: xPx / cw, row: yPx / ch, img: img})
	t.mu.Unlock()
}

// flushImagesLocked emits this frame's images, after the text diff.
//
// Kitty placements from the previous frame are deleted first: the protocol
// keeps a placement alive until told otherwise, so without this an image that
// moved or went away would stay on screen under the new one. Sixel has no
// such notion - its pixels are already screen content, and the text diff that
// repainted those cells is what erased them.
// The caller already holds t.mu (EndFrame does, and writes the text diff
// under it), so this must NOT take it again - sync.Mutex is not reentrant.
func (t *TUIBackend) flushImagesLocked(textChanged bool) {
	proto := t.graphics
	imgs := t.pendingImages
	t.pendingImages = nil
	had := t.hadImages
	t.hadImages = len(imgs) > 0

	if proto == GraphicsNone || (len(imgs) == 0 && !had) {
		return
	}

	// Nothing to do when the same pictures are already on screen and this
	// frame's text diff wrote nothing that could have disturbed them.
	//
	// A frame is drawn on a heartbeat whether or not anything happened, and a
	// picture is not a few bytes of text: re-transmitting one every frame
	// pushes its whole payload down the pty forever, base64'd, for no change
	// at all. Kitty placements persist until deleted, and sixel pixels are
	// screen content that only a repaint of those cells can erase — so when
	// neither the set nor the text moved, the screen is already correct.
	same := !textChanged && len(imgs) == len(t.shownImages)
	if same {
		for i := range imgs {
			if imgs[i] != t.shownImages[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.shownImages = imgs
		return
	}
	t.shownImages = imgs

	var sb strings.Builder
	// Save the cursor, because placing a picture means moving it. The caret
	// was already positioned by the text diff just above, and leaving it
	// parked wherever the last image was anchored puts the terminal's own
	// cursor block on top of that image — a solid cell of it, in the corner.
	// DECSC/DECRC restores the position (and the pen) once the pictures are
	// out, so the caret ends the frame where the text layer put it.
	sb.WriteString("\0337")
	if proto == GraphicsKitty && had {
		sb.WriteString("\033_Ga=d,d=A\033\\") // delete every placement
	}
	for _, p := range imgs {
		if p.col < 0 || p.row < 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\033[%d;%dH", p.row+1, p.col+1))
		switch proto {
		case GraphicsKitty:
			writeKittyImage(&sb, p.img)
		case GraphicsSixel:
			writeSixelImage(&sb, p.img)
		}
	}
	sb.WriteString("\0338")
	t.write(sb.String())
}

// writeKittyImage transmits and displays img at the cursor: RGBA direct
// payload, base64, chunked as the protocol requires (m=1 on every piece but
// the last). C=1 keeps the cursor where it was, so the text layout the caller
// computed is not disturbed by having drawn a picture.
func writeKittyImage(sb *strings.Builder, img image.Image) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	raw := make([]byte, 0, w*h*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			raw = append(raw, byte(r>>8), byte(g>>8), byte(bl>>8), byte(a>>8))
		}
	}
	payload := base64.StdEncoding.EncodeToString(raw)

	first := true
	for len(payload) > 0 {
		n := len(payload)
		if n > maxKittyChunk {
			n = maxKittyChunk
		}
		chunk := payload[:n]
		payload = payload[n:]
		more := 0
		if len(payload) > 0 {
			more = 1
		}
		sb.WriteString("\033_G")
		if first {
			fmt.Fprintf(sb, "a=T,f=32,s=%d,v=%d,C=1,m=%d", w, h, more)
			first = false
		} else {
			fmt.Fprintf(sb, "m=%d", more)
		}
		sb.WriteString(";")
		sb.WriteString(chunk)
		sb.WriteString("\033\\")
	}
}

// writeSixelImage encodes img as sixel at the cursor.
//
// Colours are quantised to the 6x6x6 cube (216 entries), which sixel's own
// percentage-based colour registers express exactly and every sixel terminal
// has room for. Fully transparent pixels are left unset rather than painted,
// so an image with a cut-out composites over the text under it instead of
// blanking a rectangle.
func writeSixelImage(sb *strings.Builder, img image.Image) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return
	}

	// Quantise once: index per pixel, -1 for transparent.
	idx := make([]int, w*h)
	used := make(map[int]bool)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a>>8 < 128 {
				idx[y*w+x] = -1
				continue
			}
			q := int(r>>8)*5/255*36 + int(g>>8)*5/255*6 + int(bl>>8)*5/255
			idx[y*w+x] = q
			used[q] = true
		}
	}

	sb.WriteString("\033Pq") // DCS q: sixel data follows
	// Declare only the registers this image uses, as percentages.
	for c := range used {
		r, g, bl := c/36, (c/6)%6, c%6
		fmt.Fprintf(sb, "#%d;2;%d;%d;%d", c, r*100/5, g*100/5, bl*100/5)
	}

	// Sixel paints six rows at a time, one colour pass per band.
	for top := 0; top < h; top += 6 {
		bandColors := map[int]bool{}
		for y := top; y < top+6 && y < h; y++ {
			for x := 0; x < w; x++ {
				if c := idx[y*w+x]; c >= 0 {
					bandColors[c] = true
				}
			}
		}
		first := true
		for c := range bandColors {
			if !first {
				sb.WriteString("$") // carriage return within the band
			}
			first = false
			fmt.Fprintf(sb, "#%d", c)
			runChar, runLen := -1, 0
			emit := func() {
				if runLen <= 0 {
					return
				}
				if runLen > 3 {
					fmt.Fprintf(sb, "!%d%c", runLen, rune(runChar))
				} else {
					sb.WriteString(strings.Repeat(string(rune(runChar)), runLen))
				}
			}
			for x := 0; x < w; x++ {
				bits := 0
				for k := 0; k < 6; k++ {
					y := top + k
					if y < h && idx[y*w+x] == c {
						bits |= 1 << k
					}
				}
				ch := 0x3F + bits
				if ch == runChar {
					runLen++
					continue
				}
				emit()
				runChar, runLen = ch, 1
			}
			emit()
		}
		sb.WriteString("-") // next band
	}
	sb.WriteString("\033\\") // ST
}

// CellPixelSize implements core.CellPixelSizer: the outer terminal's cell in
// device pixels, as it answered CSI 16 t at startup. 0,0 until it does, and on
// a terminal that never answers.
//
// The value is queried for this backend's own use (a ?1016 pixel mouse
// coordinate is divided by it), but it is just as much the answer a program
// hosted inside a pane needs for its OWN CSI 16 t: a picture is sized,
// positioned and scaled entirely in these units, and a child told nothing can
// draw nothing.
func (t *TUIBackend) CellPixelSize() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.outerCellSizeOK && t.outerCellW > 0 && t.outerCellH > 0 {
		return t.outerCellW, t.outerCellH
	}
	// The escape query went unanswered. Ask the kernel instead: TIOCGWINSZ
	// carries the window's PIXEL size beside its cell size, and a cell is one
	// divided by the other. See ttyPixelSize for why this fails differently
	// and is worth trying — an unanswered CSI 16 t is common (a multiplexer
	// swallows the reply, a terminal ignores what it does not know) and used
	// to leave a hosted child with no geometry at all, which is not a failure
	// it can recover from: no image can be sized without it.
	if t.cols > 0 && t.rows > 0 {
		if wPx, hPx := ttyPixelSizeFn(t.fd); wPx > 0 && hPx > 0 {
			if cw, ch := wPx/t.cols, hPx/t.rows; cw > 0 && ch > 0 {
				return cw, ch
			}
		}
	}
	return 0, 0
}
