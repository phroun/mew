package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// An isolated combining mark is conventionally shown on a DOTTED CIRCLE
// (U+25CC) standing in for the base it lacks. For that to be one glyph rather
// than two, the circle and the mark have to shape as a single CLUSTER — which
// means one face has to cover both.
//
// The primary term face does not: it carries the circle, the script face
// carries the mark, and per-rune fallback then SPLITS the shaping run between
// them. A split is not a cosmetic difference. The shaper positions each run
// independently, so the mark comes out as its own run at the pen position
// AFTER the circle, with no attachment offset — beyond the right edge of a
// one-cell mask, where it is clipped away and the reader sees a bare circle.
//
// Naming the mark's script face instead puts both in one run and the shaper
// composes them, which is what the caller (PurfecTerm.cellFamily) relies on.
func TestAnchoredMarkComposesInItsScriptFace(t *testing.T) {
	e := NewEngine()

	// shape reports how many runs the string took and whether any glyph was
	// positioned onto another (a non-zero offset is the shaper attaching a mark).
	shape := func(family, s string) (runs, glyphs int, attached bool) {
		sp := e.ShapeRun(&core.Font{Name: family, Size: 12}, s)
		for _, l := range sp.Lines {
			runs += len(l.Runs)
			for _, r := range l.Runs {
				glyphs += len(r.raw.Glyphs)
				for _, g := range r.raw.Glyphs {
					if g.XOffset != 0 || g.YOffset != 0 {
						attached = true
					}
				}
			}
		}
		return
	}

	for _, c := range []struct{ name, family, text string }{
		{"Hebrew point on the anchor", "ui-term-hebrew", "◌ִ"},
		{"Arabic harakat on the anchor", "ui-term-arabic", "◌ً"},
	} {
		runs, glyphs, attached := shape(c.family, c.text)
		if runs != 1 {
			t.Errorf("%s: %d shaping runs in %s, want 1 — a split run cannot compose",
				c.name, runs, c.family)
		}
		if glyphs != 2 {
			t.Errorf("%s: %d glyphs, want 2 (the circle and the mark)", c.name, glyphs)
		}
		if !attached {
			t.Errorf("%s: no glyph carries a positioning offset, so the mark was not composed onto the circle", c.name)
		}
	}

	// The state of affairs this works around, asserted so it is on the record
	// rather than folklore: in the PRIMARY face the same text splits, and the
	// mark ends up positioned nowhere in particular.
	if runs, _, attached := shape("ui-term", "◌ִ"); runs == 1 && attached {
		t.Log("the primary face now composes the anchor too; cellFamily's special case may be removable")
	}
}
