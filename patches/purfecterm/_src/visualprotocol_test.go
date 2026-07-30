package purfecterm

import "testing"

func newBuf(t *testing.T, cols, rows int) *Buffer {
	b := NewBuffer(cols, rows, 100)
	if b == nil {
		t.Fatal("no buffer")
	}
	return b
}

// Standard mode: wide chars carry real widths, visual addressing lands on
// characters, and overwrites preserve column geometry.
func TestStandardContract(t *testing.T) {
	b := newBuf(t, 20, 4)
	for _, r := range "日abc" {
		b.WriteChar(r)
	}
	// Width truth: the wide cell carries 2.0.
	if w := b.GetVisibleCell(0, 0).CellWidth; w != 2.0 {
		t.Fatalf("wide cell width = %v, want 2.0", w)
	}
	// Visual addressing: visual column 3 is 'b' (logical cell 2).
	b.SetCursorVisual(3, 0)
	if x, _ := b.GetCursor(); x != 2 {
		t.Fatalf("SetCursorVisual(3) -> logical %d, want 2", x)
	}
	// Trailing-half clamp: visual column 1 sits inside the wide glyph.
	b.SetCursorVisual(1, 0)
	if x, _ := b.GetCursor(); x != 0 {
		t.Fatalf("trailing half should clamp onto the glyph, got %d", x)
	}

	// Narrow over wide: geometry preserved via an orphan space.
	b.SetCursorVisual(0, 0)
	b.WriteChar('x')
	// Row must now read x, space, a, b, c — 'a' still at visual column 2.
	if c := b.GetVisibleCell(1, 0).Char; c != ' ' && c != 0 {
		t.Fatalf("vacated column should be an orphan space, got %q", c)
	}
	if c := b.GetVisibleCell(2, 0).Char; c != 'a' {
		t.Fatalf("cells right of the edit must not move, got %q", c)
	}

	// Wide over narrow: the following column is swallowed.
	b2 := newBuf(t, 20, 4)
	for _, r := range "abcd" {
		b2.WriteChar(r)
	}
	b2.SetCursorVisual(1, 0)
	b2.WriteChar('日')
	if c := b2.GetVisibleCell(1, 0).Char; c != '日' {
		t.Fatalf("wide write should land at visual 1, got %q", c)
	}
	if c := b2.GetVisibleCell(2, 0).Char; c != 'd' {
		t.Fatalf("'c' should be swallowed leaving d as the next cell, got %q", c)
	}

	// Backspace steps one COLUMN, landing on a wide glyph's cell.
	b3 := newBuf(t, 20, 4)
	b3.WriteChar('日')
	b3.Backspace()
	if x, _ := b3.GetCursor(); x != 0 {
		t.Fatalf("backspace over a wide glyph should land on it, got %d", x)
	}

	// Wrap on accumulated visual width: 3 wide chars fill 6 columns.
	b4 := newBuf(t, 6, 4)
	for _, r := range "日日日水" {
		b4.WriteChar(r)
	}
	if c := b4.GetVisibleCell(0, 1).Char; c != '水' {
		t.Fatalf("4th wide char should wrap to row 1, got %q", c)
	}

	// Flex mode still speaks the logical contract.
	b5 := newBuf(t, 20, 4)
	b5.SetFlexWidthMode(true)
	for _, r := range "日abc" {
		b5.WriteChar(r)
	}
	b5.SetCursorVisual(3, 0) // logical under flex
	if x, _ := b5.GetCursor(); x != 3 {
		t.Fatalf("flex SetCursorVisual should stay logical, got %d", x)
	}
}
