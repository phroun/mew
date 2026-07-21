package render

import (
	"regexp"
	"strings"
	"testing"
)

// paint feeds a sequence of MoveCursor/Write operations into a back buffer and
// returns the bytes present() emits. The pen is left where the ops left it (the
// hardware-cursor placement present() appends).
type op struct {
	move [2]int // 1-based x,y when isMove
	text string
	isMv bool
}

func mv(x, y int) op { return op{move: [2]int{x, y}, isMv: true} }
func wr(s string) op { return op{text: s} }

func paint(b *backBuffer, ops ...op) string {
	b.begin()
	for _, o := range ops {
		if o.isMv {
			b.moveTo(o.move[0], o.move[1])
		} else {
			b.writeString(o.text)
		}
	}
	var sb strings.Builder
	b.present(&sb)
	return sb.String()
}

// stripCUPAndCursor removes cursor-position and visibility control so tests can
// assert on painted content. It keeps SGR (…m) sequences.
var cupRe = regexp.MustCompile(`\x1b\[[0-9]+;[0-9]+H|\x1b\[\?25[hl]|\x1b\[2J\x1b\[H`)

func visible(s string) string { return cupRe.ReplaceAllString(s, "") }

// allAnsiRe strips every escape sequence, leaving only the painted glyphs.
var allAnsiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func plain(s string) string { return allAnsiRe.ReplaceAllString(s, "") }

func TestBackBufferFirstFrameEmitsContent(t *testing.T) {
	b := newBackBuffer(10, 3)
	out := paint(b, mv(1, 1), wr("\x1b[0;37;40mhi"))
	if !strings.Contains(out, "\x1b[0;37;40mh") {
		t.Errorf("first frame should emit styled content: %q", out)
	}
	// The first frame paints over the terminal without a clear (matching the
	// original renderer's startup).
	if strings.Contains(out, "\x1b[2J") {
		t.Errorf("first frame should not clear the screen: %q", out)
	}
}

// Default mode: minimal cell-span diffs — only the changed cell (and a cursor
// move to it) is emitted.
func TestBackBufferSkipsUnchangedCells(t *testing.T) {
	b := newBackBuffer(20, 2)
	paint(b, mv(1, 1), wr("\x1b[0;37;40mhello"))
	// Second frame: change only the last character.
	out := paint(b, mv(1, 1), wr("\x1b[0;37;40mhellp"))
	if !strings.Contains(out, "p") {
		t.Errorf("changed cell not emitted: %q", out)
	}
	if strings.Count(visible(out), "h") != 0 {
		t.Errorf("unchanged 'h' should not be re-emitted: %q", visible(out))
	}
	// The cursor should be repositioned to the changed column (5), not column 1.
	if !strings.Contains(out, "\x1b[1;5H") {
		t.Errorf("expected a move to the changed column: %q", out)
	}
}

// Flip mode: the diff granularity is a row — the run-level bidi flip is only
// well-defined over a whole row, so a changed cell re-emits its row in full
// while untouched rows are skipped entirely.
func TestBackBufferFlipRowGranularity(t *testing.T) {
	b := newBackBuffer(20, 3)
	b.flipBidi = true
	paint(b, mv(1, 1), wr("\x1b[0;37;40mhello"), mv(1, 2), wr("\x1b[0;37;40msecond"))
	// Second frame: change only row 1's last character; row 2 unchanged.
	out := paint(b, mv(1, 1), wr("\x1b[0;37;40mhellp"), mv(1, 2), wr("\x1b[0;37;40msecond"))
	if !strings.Contains(out, "p") {
		t.Errorf("changed cell not emitted: %q", out)
	}
	// The changed row is re-emitted in full (its unchanged 'h' included), from
	// column 1.
	if !strings.Contains(visible(out), "h") {
		t.Errorf("the changed row should repaint in full: %q", visible(out))
	}
	if !strings.Contains(out, "\x1b[1;1H") {
		t.Errorf("expected the row to start from column 1: %q", out)
	}
	// The untouched row is NOT re-emitted.
	if strings.Contains(visible(out), "second") {
		t.Errorf("unchanged row should not be re-emitted: %q", visible(out))
	}
}

func TestBackBufferIdenticalFrameEmitsNoContent(t *testing.T) {
	b := newBackBuffer(20, 2)
	paint(b, mv(1, 1), wr("\x1b[0;37;40mhello"))
	out := paint(b, mv(1, 1), wr("\x1b[0;37;40mhello"))
	if v := visible(out); v != "" {
		t.Errorf("identical frame should emit no painted content, got %q", v)
	}
}

// present() coalesces SGR: a run of same-styled cells emits its style once and
// keeps the glyphs contiguous (no escape injected between them, which a terminal
// needs for Arabic shaping / grapheme joining); a style change re-emits.
func TestBackBufferCoalescesRunSGR(t *testing.T) {
	b := newBackBuffer(10, 2)
	out := paint(b, mv(1, 1), wr("\x1b[0;37;40mabc"))
	if n := strings.Count(out, "\x1b[0;37;40m"); n != 1 {
		t.Errorf("same-style run should emit its SGR once, got %d: %q", n, out)
	}
	if !strings.Contains(out, "\x1b[0;37;40mabc") {
		t.Errorf("same-style glyphs must stay contiguous after one SGR: %q", out)
	}

	// Arabic run: the two letters must be adjacent in the stream (one SGR ahead
	// of both), so the terminal can join them.
	b2 := newBackBuffer(10, 2)
	out2 := paint(b2, mv(1, 1), wr("\x1b[0;37;40mلا"))
	if !strings.Contains(out2, "\x1b[0;37;40mلا") {
		t.Errorf("adjacent Arabic letters must not be split by an SGR: %q", out2)
	}

	// A color change still re-emits the style at the boundary.
	b3 := newBackBuffer(10, 2)
	out3 := paint(b3, mv(1, 1), wr("\x1b[0;37;40ma\x1b[0;91;40mb"))
	if !strings.Contains(out3, "\x1b[0;37;40ma\x1b[0;91;40mb") {
		t.Errorf("a color change must re-emit the SGR: %q", out3)
	}
}

// Under logical addressing (flex-width terminals, purfecterm ?2027) a changed
// row is re-emitted WHOLE from column 1 and its logical remainder truncated
// with EL. Mid-row span updates are unsound there: overwriting content whose
// width profile changed (narrow chars over a wide cell, as when lines shift)
// consumes a different number of logical cells than it replaced, silently
// shifting everything preserved to its right — the duplicated-fragment /
// stale-tail corruption.
func TestBackBufferLogicalCUPRowGranularity(t *testing.T) {
	b := newBackBuffer(10, 2)
	b.logicalCUP = true
	paint(b, mv(1, 1), wr("\x1b[0m日abc"))

	// Change one cell: the whole row re-emits from column 1, no mid-row CUP.
	out := paint(b, mv(1, 1), wr("\x1b[0m日aXc"))
	if !strings.Contains(out, "\x1b[1;1H") || !strings.Contains(out, "日aXc") {
		t.Errorf("logical mode should re-emit the whole changed row, got %q", out)
	}
	if strings.Contains(out, "\x1b[1;3H") || strings.Contains(out, "\x1b[1;4H") {
		t.Errorf("logical mode must not address mid-row, got %q", out)
	}
	if !strings.Contains(out, "\x1b[0K") {
		t.Errorf("logical mode should truncate the row's logical tail with EL, got %q", out)
	}

	// Narrow content replacing a wide glyph (the line-shift case): the full
	// row including the visually-unchanged tail is rewritten, so the logical
	// grid can't end up with 'a' swallowed and "bc" shifted.
	out = paint(b, mv(1, 1), wr("\x1b[0mxxaXc"))
	if !strings.Contains(out, "xxaXc") {
		t.Errorf("narrow-over-wide must rewrite the whole row, got %q", out)
	}

	// Logical addressing off: classic minimal span at the visual column.
	b2 := newBackBuffer(10, 2)
	paint(b2, mv(1, 1), wr("\x1b[0m日abc"))
	out2 := paint(b2, mv(1, 1), wr("\x1b[0m日aXc"))
	if !strings.Contains(out2, "\x1b[1;4H") {
		t.Errorf("visual CUP should address visual column 4, got %q", out2)
	}
}

func TestBackBufferWideRune(t *testing.T) {
	b := newBackBuffer(10, 2)
	out := paint(b, mv(1, 1), wr("\x1b[0m日x"))
	if !strings.Contains(out, "日") {
		t.Fatalf("wide rune missing: %q", out)
	}
	// The wide rune occupies columns 1-2; 'x' must land at column 3.
	// Repaint replacing the wide rune with two narrow runes.
	out = paint(b, mv(1, 1), wr("\x1b[0mabx"))
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Errorf("narrow replacement missing: %q", out)
	}
}

func TestBackBufferCombiningMark(t *testing.T) {
	b := newBackBuffer(10, 2)
	// base 'e' + combining acute (U+0301) should share one cell.
	out := paint(b, mv(1, 1), wr("\x1b[0méx"))
	if !strings.Contains(out, "é") {
		t.Errorf("combining mark should attach to its base cell: %q", out)
	}
	// The 'x' must be at column 2 (the mark took no column).
	if !strings.Contains(out, "\x1b[1;1H") {
		t.Errorf("content should start at column 1: %q", out)
	}
}

func TestBackBufferStyleAccumulatorReverse(t *testing.T) {
	b := newBackBuffer(10, 2)
	// A color followed by a non-reset SGR (reverse) accumulates; the glyph's
	// cell carries both, so "\x1b[7m|" appears (reverse immediately before it).
	out := paint(b, mv(1, 1), wr("\x1b[0;37;40m\x1b[7m|"))
	if !strings.Contains(out, "\x1b[7m|") {
		t.Errorf("reverse should precede the glyph: %q", out)
	}
	// A reset sequence replaces the accumulator.
	out2 := paint(b, mv(1, 1), wr("\x1b[0;37;40mX"))
	if strings.Contains(out2, "\x1b[7m") {
		t.Errorf("reset should have cleared reverse: %q", out2)
	}
}

func TestBackBufferForceRedraw(t *testing.T) {
	b := newBackBuffer(20, 2)
	paint(b, mv(1, 1), wr("\x1b[0;37;40mhello"))
	// Identical frame normally emits nothing…
	if v := visible(paint(b, mv(1, 1), wr("\x1b[0;37;40mhello"))); v != "" {
		t.Fatalf("expected empty diff, got %q", v)
	}
	// …but after forceRedraw the whole frame repaints (and clears).
	b.forceRedraw()
	out := paint(b, mv(1, 1), wr("\x1b[0;37;40mhello"))
	if !strings.Contains(out, "\x1b[2J\x1b[H") {
		t.Errorf("force redraw should clear: %q", out)
	}
	if !strings.HasPrefix(plain(out), "hello") {
		t.Errorf("force redraw should repaint content: %q", plain(out))
	}
}

func TestBackBufferCornerCutNeverEmitted(t *testing.T) {
	b := newBackBuffer(5, 2)
	// Paint the entire bottom row including the last column.
	out := paint(b, mv(1, 2), wr("\x1b[0mABCDE"))
	// The bottom-right cell (row 2, col 5) must never be written, so 'E' (the
	// 5th char of the bottom row) is not emitted.
	if strings.Contains(visible(out), "E") {
		t.Errorf("bottom-right corner cell should not be emitted: %q", visible(out))
	}
	if !strings.Contains(visible(out), "D") {
		t.Errorf("the rest of the bottom row should paint: %q", visible(out))
	}
}

// When a later write lands on one half of a wide glyph, the other half must be
// cleared so it does not linger on screen. Reproduces the ghost cursor leaving
// "dead copies" over CJK text: a narrow overlay lands on the tail of a wide
// glyph, then the wide glyph is repainted and must fully redraw.
func TestBackBufferWideGlyphNoDeadHalf(t *testing.T) {
	b := newBackBuffer(10, 2)
	// Frame 1: a wide glyph occupying columns 1-2, then 'z'.
	paint(b, mv(1, 1), wr("\x1b[0m日z"))

	// Frame 2: repaint the same, then overlay a narrow '|' on the glyph's TAIL
	// (column 2) — the ghost-cursor case.
	out := paint(b, mv(1, 1), wr("\x1b[0m日z"), mv(2, 1), wr("\x1b[0m|"))
	// The glyph's head (column 1) must be cleared (blanked), not left half-drawn.
	if !strings.Contains(out, "\x1b[1;1H") {
		t.Errorf("head cell should be repainted when its tail is overwritten: %q", out)
	}
	if !strings.Contains(out, "|") {
		t.Errorf("overlay glyph should be emitted: %q", out)
	}

	// Frame 3: the overlay is gone; the wide glyph repaints and must fully
	// redraw over both cells (no stale '|' left behind).
	out = paint(b, mv(1, 1), wr("\x1b[0m日z"))
	if !strings.Contains(out, "日") {
		t.Errorf("wide glyph should fully redraw after the overlay clears: %q", plain(out))
	}
}

// The mirror case: a narrow overlay lands on the HEAD of a wide glyph; its tail
// cell must be cleared.
func TestBackBufferWideGlyphHeadOverwrite(t *testing.T) {
	b := newBackBuffer(10, 2)
	paint(b, mv(1, 1), wr("\x1b[0m日z"))
	// Overlay '|' on the head (column 1); the tail (column 2) must not stay a
	// dangling continuation.
	paint(b, mv(1, 1), wr("\x1b[0m日z"), mv(1, 1), wr("\x1b[0m|"))
	// Frame 3: repaint; the tail must have been a real blank, so the glyph
	// redraws cleanly.
	out := paint(b, mv(1, 1), wr("\x1b[0m日z"))
	if !strings.Contains(out, "日") {
		t.Errorf("wide glyph should redraw after a head overwrite: %q", plain(out))
	}
}

// flipBidi re-emits an RTL run in logical order (for terminals that apply
// their own bidi), leaving LTR content untouched.
func TestBackBufferFlipBidi(t *testing.T) {
	// The renderer paints Hebrew in VISUAL order: "shalom" reversed is
	// "םולש". With flipBidi the wire carries logical order "שלום".
	b := newBackBuffer(30, 2)
	b.flipBidi = true
	out := paint(b, mv(1, 1), wr("\x1b[0mabc םולש xyz"))
	p := plain(out)
	if !strings.Contains(p, "שלום") {
		t.Errorf("flip should emit the run in logical order: %q", p)
	}
	if strings.Contains(p, "םולש") {
		t.Errorf("flip must not emit visual order: %q", p)
	}
	// LTR content stays put.
	if !strings.Contains(p, "abc") || !strings.Contains(p, "xyz") {
		t.Errorf("LTR content mangled: %q", p)
	}
}

// The flip restores mirrored brackets: the renderer paints an RTL-context "("
// as ")" (visual mirroring); flipping back to logical order un-mirrors it.
// Logical "א(ב)" resolves (UAX#9, base LTR) to visual cells "ב)א)" — the run
// [א(ב] reversed with its interior paren mirrored, the trailing neutral paren
// staying in base direction. The flip must reproduce the logical form.
func TestBackBufferFlipBidiUnmirrors(t *testing.T) {
	b := newBackBuffer(30, 2)
	b.flipBidi = true
	out := paint(b, mv(1, 1), wr("\x1b[0mב)א)"))
	p := plain(out)
	if !strings.Contains(p, "א(ב)") {
		t.Errorf("flip should restore logical order with unmirrored brackets: %q", p)
	}
}

// Without the flip (default), the wire carries the visual order unchanged.
func TestBackBufferNoFlipKeepsVisual(t *testing.T) {
	b := newBackBuffer(30, 2)
	out := paint(b, mv(1, 1), wr("\x1b[0mabc םולש xyz"))
	if !strings.Contains(plain(out), "םולש") {
		t.Errorf("default emission should keep visual order: %q", plain(out))
	}
}

func TestBackBufferCornerFilledWithEL(t *testing.T) {
	b := newBackBuffer(5, 2)
	// Paint the bottom row up to (but the corner is never a glyph). The cell left
	// of the corner carries a blue background.
	out := paint(b, mv(1, 2), wr("\x1b[0;44mABCD"))
	// The corner is filled via EL, not a glyph: expect a CUP to the corner
	// followed by a style and \x1b[K, and no 5th glyph.
	if !strings.Contains(out, "\x1b[2;5H") {
		t.Errorf("expected a move to the corner cell: %q", out)
	}
	if !strings.Contains(out, "\x1b[K") {
		t.Errorf("corner should be filled with EL (\\x1b[K): %q", out)
	}
	// The blue background should be active for the corner fill.
	i := strings.Index(out, "\x1b[K")
	if i < 0 || !strings.Contains(out[:i], "\x1b[0;44m") {
		t.Errorf("corner EL should carry the neighbour's background: %q", out)
	}
}

func TestBackBufferCursorPlacementAndVisibility(t *testing.T) {
	b := newBackBuffer(10, 3)
	// Paint something, then leave the pen at (col 4, row 2); hide the cursor.
	b.begin()
	b.writeString("\x1b[0mhi")
	b.moveTo(4, 2)
	b.curVisible = false
	var sb strings.Builder
	b.present(&sb)
	out := sb.String()
	if !strings.HasSuffix(strings.TrimSuffix(out, "\x1b[?25l"), "\x1b[2;4H") {
		// The final control before visibility must place the cursor at (2,4).
		if !strings.Contains(out, "\x1b[2;4H") {
			t.Errorf("cursor not placed at pen: %q", out)
		}
	}
	if !strings.Contains(out, "\x1b[?25l") {
		t.Errorf("hidden cursor should emit hide: %q", out)
	}
}

func TestBackBufferReshapeInvalidates(t *testing.T) {
	b := newBackBuffer(10, 2)
	paint(b, mv(1, 1), wr("\x1b[0mhi"))
	b.reshape(12, 3)
	if b.w != 12 || b.h != 3 {
		t.Fatalf("reshape dims wrong: %dx%d", b.w, b.h)
	}
	out := paint(b, mv(1, 1), wr("\x1b[0mhi"))
	if !strings.Contains(out, "\x1b[2J\x1b[H") {
		t.Errorf("reshape should force a clear+repaint: %q", out)
	}
}

func TestApplySGRResetSemantics(t *testing.T) {
	b := newBackBuffer(4, 1)
	b.applySGR("\x1b[0;31m")
	if b.penStyle != "\x1b[0;31m" {
		t.Errorf("reset color should set accumulator: %q", b.penStyle)
	}
	b.applySGR("\x1b[7m")
	if b.penStyle != "\x1b[0;31m\x1b[7m" {
		t.Errorf("non-reset should append: %q", b.penStyle)
	}
	b.applySGR("\x1b[0m")
	if b.penStyle != "\x1b[0m" {
		t.Errorf("reset should replace accumulator: %q", b.penStyle)
	}
	b.applySGR("\x1b[m") // bare == reset
	if b.penStyle != "\x1b[m" {
		t.Errorf("bare SGR should reset accumulator: %q", b.penStyle)
	}
}
