package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// An isolated shin dot or sin dot anchored on a dotted circle is emitted with
// the point BEFORE the circle on a plain terminal. U+25CC is East Asian
// ambiguous; a terminal that draws it at the wide advance hangs the point a
// full cell to the right of the circle (iTerm2). Leading with the point pulls
// it back over the circle. The cell mew stores is unchanged — only the wire
// bytes reorder — so the flex-width host, which composes the cluster itself,
// keeps the natural circle-then-point order.
func TestShinSinDotAnchorReversedOnPlainTerminal(t *testing.T) {
	const circle = string(textwidth.MarkAnchor) // ◌
	cases := []struct {
		name string
		mark rune
	}{
		{"shin dot", 0x05C1},
		{"sin dot", 0x05C2},
	}
	for _, c := range cases {
		anchored := circle + string(c.mark)

		// Plain terminal: point before circle.
		b := newBackBuffer(10, 2)
		out := plain(paint(b, mv(1, 1), wr("\x1b[33m"+anchored)))
		wantPlain := string(c.mark) + circle
		if !strings.Contains(out, wantPlain) {
			t.Errorf("%s plain: wire %q, want the point before the circle (%q)", c.name, out, wantPlain)
		}
		if strings.Contains(out, anchored) {
			t.Errorf("%s plain: wire kept circle-then-point %q", c.name, out)
		}

		// Flex-width host (2027): natural order, untouched.
		bf := newBackBuffer(10, 2)
		bf.logicalCUP = true
		outf := plain(paint(bf, mv(1, 1), wr("\x1b[33m"+anchored)))
		if !strings.Contains(outf, anchored) {
			t.Errorf("%s flex: wire %q, want the natural circle-then-point (%q)", c.name, outf, anchored)
		}
	}
}

// The reorder is scoped to the two corner-sitting dots. A vowel point that sits
// below its base (hiriq) shows no visible horizontal misplacement, so its
// anchored cluster is emitted in the natural order on every surface.
func TestBelowBaseAnchorNotReversed(t *testing.T) {
	const circle = string(textwidth.MarkAnchor)
	const hiriq = 0x05B4
	anchored := circle + string(rune(hiriq))

	b := newBackBuffer(10, 2)
	out := plain(paint(b, mv(1, 1), wr("\x1b[33m"+anchored)))
	if !strings.Contains(out, anchored) {
		t.Errorf("hiriq anchor: wire %q, want the natural circle-then-point (%q)", out, anchored)
	}
}

// The stored cell is identical either way — the reorder is a wire-only concern,
// so the diff still recognises an unchanged anchored cell and re-emits nothing
// on a quiet frame.
func TestReversedAnchorCellIsStableAcrossFrames(t *testing.T) {
	const circle = string(textwidth.MarkAnchor)
	anchored := circle + string(rune(0x05C1))

	b := newBackBuffer(10, 2)
	paint(b, mv(1, 1), wr("\x1b[33m"+anchored)) // first frame
	out := plain(paint(b, mv(1, 1), wr("\x1b[33m"+anchored)))
	if strings.ContainsRune(out, textwidth.MarkAnchor) || strings.ContainsRune(out, 0x05C1) {
		t.Errorf("an unchanged anchored cell should re-emit no glyphs, got %q", out)
	}
}
