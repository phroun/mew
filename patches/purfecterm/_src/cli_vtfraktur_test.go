package cli

import (
	"strings"
	"testing"
)

// The CLI renderer emits real SGR 20 (fraktur) for a font-slot-10 (VTFRAKTUR)
// cell, so a VT100 fraktur request passes through to the host terminal. A
// non-fraktur render must not emit it. (A 5x1 grid keeps cursor coordinates
// single-digit, so "20" can only come from the fraktur SGR.)
func TestCLIFrakturSGR20(t *testing.T) {
	term, err := New(Options{Cols: 5, Rows: 1, Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	term.FeedString("\x1b[20mF")
	if out := NewRenderer(term).RenderToString(); !strings.Contains(out, "20") {
		t.Errorf("fraktur cell should emit SGR 20, got %q", out)
	}

	plain, _ := New(Options{Cols: 5, Rows: 1, Embedded: true})
	plain.FeedString("F")
	if out := NewRenderer(plain).RenderToString(); strings.Contains(out, "20") {
		t.Errorf("plain cell must not emit SGR 20, got %q", out)
	}
}
