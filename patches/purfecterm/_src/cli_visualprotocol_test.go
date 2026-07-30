package cli

import (
	"strings"
	"testing"
)

// The CLI renderer writes into a REAL terminal, which is always visual: a
// standard-mode wide cell (one logical cell, width 2) must place its
// successors at accumulated visual columns, and the hardware cursor likewise.
func TestCLIRendererVisualColumns(t *testing.T) {
	term, err := New(Options{Cols: 10, Rows: 2, Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	term.FeedString("日abc")

	out := NewRenderer(term).RenderToString()
	// 日 emits at column 1; 'a' must land at visual column 3 (not logical 2).
	if !strings.Contains(out, "\033[1;3H") {
		t.Fatalf("'a' should emit at visual column 3, got %q", out)
	}
	// Nothing may address column 2 on row 1 — that is 日's right half.
	if strings.Contains(out, "\033[1;2H") {
		t.Fatalf("the wide glyph's right half must never be addressed, got %q", out)
	}
	// Hardware cursor after "日abc" sits at visual column 6 (2+1+1+1 -> col 6).
	if !strings.Contains(out, "\033[1;6H") {
		t.Fatalf("cursor should park at visual column 6, got %q", out)
	}
}
