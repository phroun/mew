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
	// seq is the paint order when this was queued. Anything stamped LATER on
	// a cell this image covers was painted on top of it — a window, a dialog,
	// a trinket drawn after — and that cell must not show the picture.
	seq uint32
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
	case "iterm.app":
		// iTerm2 RENDERS kitty graphics from 3.5 on, but does not answer the
		// query we probe with — so without this it lands on sixel while
		// speaking the better protocol perfectly well. Version-gated because
		// anything older genuinely cannot, and a wrong guess here prints an
		// escape sequence as text into somebody's terminal.
		if itermAtLeast35(os.Getenv("TERM_PROGRAM_VERSION")) {
			return GraphicsKitty
		}
		return GraphicsNone // DA1 will offer sixel
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
	t.queueImageLocked(t.metrics.UnitsToCellX(x), t.metrics.UnitsToCellY(y), img)
}

// queueImageLocked records a picture at a cell anchor, cropped to the clip in
// force — the same clip setCell applies to text, which images bypassed
// entirely. Without it a picture drawn in a window spills past the window's
// edge and keeps going, because nothing downstream knows where the window
// ended.
func (t *TUIBackend) queueImageLocked(col, row int, img image.Image) {
	cw, ch := t.cellPixelSizeLocked()
	c0, r0, c1, r1 := t.clipCellsLocked()
	b := img.Bounds()
	if cw > 0 && ch > 0 && c1 >= c0 && r1 >= r0 {
		// The cell rectangle the picture wants, intersected with the clip.
		ic1 := col + (b.Dx()+cw-1)/cw - 1
		ir1 := row + (b.Dy()+ch-1)/ch - 1
		vc0, vr0 := max(col, c0), max(row, r0)
		vc1, vr1 := min(ic1, c1), min(ir1, r1)
		if vc1 < vc0 || vr1 < vr0 {
			return // entirely outside the clip
		}
		if vc0 != col || vr0 != row || vc1 != ic1 || vr1 != ir1 {
			sub := t.cropToCellsLocked(placedImage{col: col, row: row, img: img}, vc0, vr0, vc1, vr1)
			if sub == nil {
				return // cannot crop it, and uncropped it would spill
			}
			col, row, img = vc0, vr0, sub
		}
	}
	t.paintSeq++
	t.pendingImages = append(t.pendingImages, placedImage{col: col, row: row, img: img, seq: t.paintSeq})
}

// clipCellsLocked is the clip rectangle in whole cells. An empty clip means
// none is set and everything is in bounds.
func (t *TUIBackend) clipCellsLocked() (c0, r0, c1, r1 int) {
	r := t.clipRect
	if r.Width <= 0 || r.Height <= 0 {
		return 0, 0, t.cols - 1, t.rows - 1
	}
	c0 = t.metrics.UnitsToCellX(r.X)
	r0 = t.metrics.UnitsToCellY(r.Y)
	c1 = t.metrics.UnitsToCellX(r.X+r.Width) - 1
	r1 = t.metrics.UnitsToCellY(r.Y+r.Height) - 1
	return c0, r0, c1, r1
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
	t.queueImageLocked(xPx/cw, yPx/ch, img)
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
func (t *TUIBackend) flushImagesLocked() {
	proto := t.graphics
	imgs := t.pendingImages
	t.pendingImages = nil

	// What would actually go on screen: each picture minus whatever was
	// painted over it. This is the unit of comparison, NOT the placement list.
	//
	// Comparing placements instead was wrong in a way only the app showed: open
	// a menu over an unchanged picture and the placement list is identical, so
	// the flush skipped — leaving the previous full-size placement on screen,
	// on top of the menu it was supposed to be under. Occlusion is part of what
	// is drawn, so it has to be part of what is compared.
	var blocks []placedImage
	for _, p := range imgs {
		blocks = append(blocks, t.visibleBlocksLocked(p)...)
	}
	prev := t.shownImages
	t.shownImages = blocks

	if proto == GraphicsNone || (len(blocks) == 0 && len(prev) == 0) {
		return
	}

	// Which blocks are NEW to the screen. seq is deliberately left out: it is
	// an ordering stamp, not part of what the viewer sees, and comparing it
	// would call every frame a change.
	same := func(a, b placedImage) bool {
		return a.col == b.col && a.row == b.row && sameImage(a.img, b.img)
	}
	fresh := make([]bool, len(blocks))
	setChanged := len(blocks) != len(prev)
	for i := range blocks {
		seen := false
		for j := range prev {
			if same(blocks[i], prev[j]) {
				seen = true
				break
			}
		}
		fresh[i] = !seen
		if !seen {
			setChanged = true
		}
	}

	// KITTY: a placement lives until it is deleted. Text painted over those
	// cells composites against it rather than destroying it — the same
	// persistence that makes an image survive a `clear` — so text is no reason
	// to send anything and an unchanged screen costs nothing at all.
	//
	// SIXEL: there is no placement, only pixels that became screen content.
	// Text painted over them erases that part, so a block goes again when the
	// diff touched the cells it covers. A change elsewhere leaves it whole.
	var sb strings.Builder
	switch proto {
	case GraphicsKitty:
		if !setChanged {
			return
		}
		sb.WriteString("\0337")
		if len(prev) > 0 {
			sb.WriteString("\033_Ga=d,d=A\033\\") // delete every placement
		}
		for _, v := range blocks {
			sb.WriteString(fmt.Sprintf("\033[%d;%dH", v.row+1, v.col+1))
			writeKittyImage(&sb, v.img)
		}
	case GraphicsSixel:
		for i, v := range blocks {
			if !fresh[i] {
				c1, r1 := t.imageCellExtentLocked(v)
				dc0, dr0, dc1, dr1, any := t.damagedRectLocked(v.col, v.row, c1, r1)
				if !any {
					continue // still on screen, and nothing painted over it
				}
				// Sixel pixels are CELL CONTENT — an expensive glyph. The
				// repair is the damaged cells, not the whole picture.
				if sub := t.cropToCellsLocked(v, dc0, dr0, dc1, dr1); sub != nil {
					v = placedImage{col: dc0, row: dr0, img: sub, seq: v.seq}
				}
			}
			if sb.Len() == 0 {
				sb.WriteString("\0337")
			}
			sb.WriteString(fmt.Sprintf("\033[%d;%dH", v.row+1, v.col+1))
			writeSixelImage(&sb, v.img)
		}
	}
	if sb.Len() == 0 {
		return
	}
	// The caret was placed by the text diff; putting a picture anywhere means
	// moving it, and leaving it parked on the last image draws the terminal's
	// cursor block in the corner of that image.
	sb.WriteString("\0338")
	t.write(sb.String())
}

// imageCellExtentLocked is the last cell an image covers, from its anchor. The
// cell size may be unknown before the terminal answers, in which case the
// picture is treated as covering its anchor row alone rather than guessing.
func (t *TUIBackend) imageCellExtentLocked(p placedImage) (c1, r1 int) {
	cw, ch := t.cellPixelSizeLocked()
	b := p.img.Bounds()
	if cw <= 0 || ch <= 0 {
		return p.col, p.row
	}
	return p.col + (b.Dx()+cw-1)/cw - 1, p.row + (b.Dy()+ch-1)/ch - 1
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
	return t.cellPixelSizeLocked()
}

// cellPixelSizeLocked is CellPixelSize for callers that already hold t.mu —
// the frame flush does, and sync.Mutex is not reentrant.
func (t *TUIBackend) cellPixelSizeLocked() (int, int) {
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

// itermAtLeast35 reports whether an iTerm2 TERM_PROGRAM_VERSION is 3.5 or
// newer, the release that added kitty graphics. Anything unparseable is
// treated as older: the cost of guessing high is an escape sequence printed
// as text, and the cost of guessing low is sixel, which still works.
func itermAtLeast35(v string) bool {
	major, rest, ok := strings.Cut(strings.TrimSpace(v), ".")
	if !ok {
		return false
	}
	maj, err := strconv.Atoi(major)
	if err != nil {
		return false
	}
	if maj != 3 {
		return maj > 3
	}
	minorStr, _, _ := strings.Cut(rest, ".")
	minor, err := strconv.Atoi(minorStr)
	return err == nil && minor >= 5
}

// cropToCellsLocked returns the part of a placement covering cells
// [dc0,dc1] x [dr0,dr1], or nil when it cannot be cropped (an unknown cell
// size, or an image with no SubImage) and the whole thing must go instead.
//
// The crop is in the image's own pixels, clamped to its edges — the last cell
// a picture touches is usually a partial one, since nothing makes an image an
// exact multiple of the cell.
func (t *TUIBackend) cropToCellsLocked(p placedImage, dc0, dr0, dc1, dr1 int) image.Image {
	cw, ch := t.cellPixelSizeLocked()
	if cw <= 0 || ch <= 0 {
		return nil
	}
	sub, ok := p.img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil
	}
	b := p.img.Bounds()
	x0 := b.Min.X + (dc0-p.col)*cw
	y0 := b.Min.Y + (dr0-p.row)*ch
	x1 := b.Min.X + (dc1-p.col+1)*cw
	y1 := b.Min.Y + (dr1-p.row+1)*ch
	r := image.Rect(x0, y0, x1, y1).Intersect(b)
	if r.Empty() {
		return nil
	}
	return sub.SubImage(r)
}

// cellRun is an inclusive span of columns on one row.
type cellRun struct{ c0, c1 int }

// visibleBlocksLocked splits a placement into the rectangles of it that nothing
// painted over, largest blocks first.
//
// Trinkets paint back to front into one cell plane, so "on top" is simply
// "written later" — and an image, queued rather than composited, has to ask
// after the fact. A cell stamped with a higher paint order than the image
// carries something drawn above it: a window, a dialog, a menu. Sending the
// picture for that cell would put it over the thing that covers it, which is
// what an un-occluded image looks like on screen.
//
// Rows with identical visible runs are merged, so the ordinary case — a window
// lying across one corner — costs two rectangles rather than one per row.
// Returns the whole placement unsplit when nothing covers it.
func (t *TUIBackend) visibleBlocksLocked(p placedImage) []placedImage {
	if p.col < 0 || p.row < 0 {
		return nil
	}
	c1, r1 := t.imageCellExtentLocked(p)
	rowRuns := make([][]cellRun, 0, r1-p.row+1)
	covered := false
	for y := p.row; y <= r1; y++ {
		var runs []cellRun
		cur := cellRun{c0: -1}
		for x := p.col; x <= c1; x++ {
			over := y >= 0 && y < len(t.cellSeq) && x >= 0 && x < len(t.cellSeq[y]) &&
				t.cellSeq[y][x] > p.seq
			if over {
				covered = true
				if cur.c0 >= 0 {
					runs = append(runs, cur)
					cur = cellRun{c0: -1}
				}
				continue
			}
			if cur.c0 < 0 {
				cur = cellRun{c0: x, c1: x}
			} else {
				cur.c1 = x
			}
		}
		if cur.c0 >= 0 {
			runs = append(runs, cur)
		}
		rowRuns = append(rowRuns, runs)
	}
	if !covered {
		return []placedImage{p} // nothing on top: one placement, as before
	}

	var out []placedImage
	for i := 0; i < len(rowRuns); {
		if len(rowRuns[i]) == 0 {
			i++
			continue
		}
		// Merge the rows below that are covered identically.
		j := i + 1
		for j < len(rowRuns) && sameRuns(rowRuns[i], rowRuns[j]) {
			j++
		}
		for _, r := range rowRuns[i] {
			sub := t.cropToCellsLocked(p, r.c0, p.row+i, r.c1, p.row+j-1)
			if sub == nil {
				continue
			}
			out = append(out, placedImage{col: r.c0, row: p.row + i, img: sub, seq: p.seq})
		}
		i = j
	}
	return out
}

func sameRuns(a, b []cellRun) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// imgIdentity says WHICH PIXELS an image refers to, rather than which wrapper
// object refers to them.
//
// Cropping produces a new SubImage every time it is called, and a clipped
// picture is re-cropped on every frame — so comparing the wrapper answers
// "different" forever, and an unchanged picture is re-transmitted for as long
// as it is on screen. A crop shares its parent's backing array, so the address
// of the first byte together with the rectangle and stride identifies the
// pixels themselves and is stable across frames.
type imgIdentity struct {
	pix    *uint8 // first byte of the shared backing array
	rect   image.Rectangle
	stride int
}

func identityOf(img image.Image) (imgIdentity, bool) {
	switch m := img.(type) {
	case *image.RGBA:
		if len(m.Pix) == 0 {
			return imgIdentity{}, false
		}
		return imgIdentity{pix: &m.Pix[0], rect: m.Rect, stride: m.Stride}, true
	case *image.NRGBA:
		if len(m.Pix) == 0 {
			return imgIdentity{}, false
		}
		return imgIdentity{pix: &m.Pix[0], rect: m.Rect, stride: m.Stride}, true
	}
	return imgIdentity{}, false
}

// sameImage reports whether two images are the same pixels. It answers by
// identity, never by content: pixels rewritten in place under an unchanged
// wrapper read as unchanged here, which is why the thing that rewrites them
// hands over a fresh buffer instead (see the trinket's image cache).
func sameImage(a, b image.Image) bool {
	if a == b {
		return true
	}
	ia, ok := identityOf(a)
	if !ok {
		return false
	}
	ib, ok := identityOf(b)
	return ok && ia == ib
}
