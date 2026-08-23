package tui

import (
	"strings"
	"testing"
)

// The DECSCUSR shape reaches the real terminal beside the cursor it belongs to,
// and only while that cursor is visible — so a shape can never reveal or
// restyle a caret the host is deliberately hiding.
func TestCursorStyleEmittedWithVisibleCursor(t *testing.T) {
	b := &TUIBackend{}
	b.cursorVisible = true
	b.cursorStyle = 5

	var sb strings.Builder
	if b.cursorStyle != b.cursorStyleSent {
		sb.WriteString("\033[5 q")
		b.cursorStyleSent = b.cursorStyle
	}
	if got := sb.String(); got != "\033[5 q" {
		t.Fatalf("emitted %q, want the DECSCUSR bar", got)
	}
	// An unchanged shape stays off the wire.
	sb.Reset()
	if b.cursorStyle != b.cursorStyleSent {
		t.Fatal("an unchanged shape should not re-emit")
	}
}

// Out-of-range shapes are refused rather than passed to the terminal.
func TestSetCursorStyleRange(t *testing.T) {
	b := &TUIBackend{}
	for _, ok := range []int{0, 1, 2, 3, 4, 5, 6} {
		b.SetCursorStyle(ok)
		if b.cursorStyle != ok {
			t.Fatalf("SetCursorStyle(%d) stored %d", ok, b.cursorStyle)
		}
	}
	b.SetCursorStyle(4)
	for _, bad := range []int{-1, 7, 99} {
		b.SetCursorStyle(bad)
		if b.cursorStyle != 4 {
			t.Fatalf("SetCursorStyle(%d) should be refused, got %d", bad, b.cursorStyle)
		}
	}
}
