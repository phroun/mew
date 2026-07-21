package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/phroun/kittytk/style"
)

// newTestTUI builds a backend painting into a buffer, without Init (no real
// terminal, no keyboard handler).
func newTestTUI(cols, rows int) (*TUIBackend, *bytes.Buffer) {
	var out bytes.Buffer
	opts := DefaultTUIOptions()
	opts.Output = &out
	b := NewTUIBackend(opts)
	b.cols, b.rows = cols, rows
	b.allocateBuffers()
	return b, &out
}

func cellX(b *TUIBackend, col int) int { return int(b.metrics.CellToUnitsX(col)) }

// A wide glyph and the char after it emit contiguously: the continuation cell
// is never addressed (no CUP into the glyph's right half, which would clobber
// it), and no SGR splits the same-styled run.
func TestTUIWideGlyphContiguous(t *testing.T) {
	b, out := newTestTUI(20, 2)
	s := style.DefaultStyle().Bold()

	b.BeginFrame()
	b.DrawText(0, 0, "日X", s, nil)
	b.EndFrame()
	got := out.String()
	if !strings.Contains(got, "日X") {
		t.Fatalf("wide glyph and successor should be contiguous, got %q", got)
	}
	if strings.Contains(got, "\033[1;2H") {
		t.Fatalf("the continuation column must never be addressed, got %q", got)
	}

	// An identical second frame emits nothing.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 0, "日X", s, nil)
	b.EndFrame()
	if got := out.String(); got != "" {
		t.Fatalf("identical frame should emit nothing, got %q", got)
	}
}

// Combining marks ride the base cell (never their own column) and emit
// attached to their glyph.
func TestTUICombiningMarkRidesBase(t *testing.T) {
	b, out := newTestTUI(20, 2)
	s := style.DefaultStyle()

	b.BeginFrame()
	b.DrawText(0, 0, "éZ", s, nil)
	b.EndFrame()
	if got := out.String(); !strings.Contains(got, "éZ") {
		t.Fatalf("combining mark should ride its base glyph, got %q", got)
	}
	// The mark consumed no cell: Z sits in column 1.
	if b.backBuffer[0][1].Char != 'Z' {
		t.Fatalf("Z should occupy the cell right after the base, got %q", b.backBuffer[0][1].Char)
	}
}

// A same-styled run emits its SGR exactly once.
func TestTUISGRCoalesced(t *testing.T) {
	b, out := newTestTUI(20, 2)
	s := style.DefaultStyle().Bold()

	b.BeginFrame()
	b.DrawText(0, 0, "abc", s, nil)
	b.EndFrame()
	if n := strings.Count(out.String(), s.Code()); n != 1 {
		t.Fatalf("same-styled run should emit one SGR, got %d in %q", n, out.String())
	}
}

// A row fully owned by one DWL group emits as a real DEC double-width line:
// ESC#6 and the carrier glyphs only (no doubled spacing between them).
func TestTUIDWLUniformRowRealMode(t *testing.T) {
	b, out := newTestTUI(8, 2)
	s := style.DefaultStyle()

	b.BeginFrame()
	for i, ch := range []rune{'A', 'B', ' ', ' '} {
		b.DrawCellDWL(b.metrics.CellToUnitsX(2*i), 0, ch, "", s, '6')
	}
	b.EndFrame()
	got := out.String()
	if !strings.Contains(got, "\033#6") {
		t.Fatalf("uniform DWL row should emit ESC#6, got %q", got)
	}
	if !strings.Contains(got, "AB") {
		t.Fatalf("real DEC mode emits carriers only (AB adjacent), got %q", got)
	}
	if strings.Contains(got, "A B") {
		t.Fatalf("real DEC mode must not double-space, got %q", got)
	}
}

// A wide (CJK) glyph inside a DWL line occupies a 4-column group; real-mode
// emission writes just the glyph.
func TestTUIDWLWideGlyphGroup(t *testing.T) {
	b, out := newTestTUI(4, 1)
	s := style.DefaultStyle()

	b.BeginFrame()
	if consumed := b.DrawCellDWL(0, 0, '日', "", s, '6'); consumed != 4 {
		t.Fatalf("a wide DWL cell should consume 4 columns, got %d", consumed)
	}
	b.EndFrame()
	got := out.String()
	if !strings.Contains(got, "\033#6") || !strings.Contains(got, "日") {
		t.Fatalf("DWL wide glyph should emit in real DEC mode, got %q", got)
	}
	if strings.Contains(got, "日 ") {
		t.Fatalf("fillers must not be emitted in real DEC mode, got %q", got)
	}
}

// A row shared between a DWL group and ordinary content stays a normal line:
// no ESC#6, DWL cells render double-spaced, and nothing shifts.
func TestTUIDWLMixedRowDoubleSpaced(t *testing.T) {
	b, out := newTestTUI(8, 2)
	s := style.DefaultStyle()

	b.BeginFrame()
	b.DrawCellDWL(b.metrics.CellToUnitsX(0), 0, 'A', "", s, '6')
	b.DrawCellDWL(b.metrics.CellToUnitsX(2), 0, 'B', "", s, '6')
	b.DrawText(b.metrics.CellToUnitsX(4), 0, "zz", s, nil)
	b.EndFrame()
	got := out.String()
	if strings.Contains(got, "\033#6") {
		t.Fatalf("a mixed row must not switch to DEC double-width, got %q", got)
	}
	if !strings.Contains(got, "A B zz") {
		t.Fatalf("mixed row should double-space the DWL cells in place, got %q", got)
	}
}

// When a row stops being uniformly DWL, the terminal line reverts to single
// width (ESC#5) and repaints.
func TestTUIDWLReversion(t *testing.T) {
	b, out := newTestTUI(4, 1)
	s := style.DefaultStyle()

	b.BeginFrame()
	b.DrawCellDWL(0, 0, 'A', "", s, '6')
	b.DrawCellDWL(b.metrics.CellToUnitsX(2), 0, 'B', "", s, '6')
	b.EndFrame()
	if !strings.Contains(out.String(), "\033#6") {
		t.Fatalf("precondition: DWL row should engage, got %q", out.String())
	}

	out.Reset()
	b.BeginFrame() // cleared to ordinary cells
	b.EndFrame()
	if !strings.Contains(out.String(), "\033#5") {
		t.Fatalf("leaving DWL should emit ESC#5, got %q", out.String())
	}
}
