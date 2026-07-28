package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The embedded Arabic faces must CURSIVELY JOIN under this engine's shaper:
// shaping a run must substitute contextual forms, not paint isolated glyphs
// side by side. This guards the font choice itself — the current Noto Arabic
// releases implement the dotted "tooth" letters (ba/noon/yeh…) through
// chained contextual GSUB that go-text/typesetting does not execute, leaving
// the middle of every word isolated; the embedded archive (phase-2) builds
// use classic init/medi/fina substitutions that do work. If these faces are
// ever swapped, this test fails before any screenshot has to.
func TestEmbeddedArabicFacesJoin(t *testing.T) {
	e := NewEngine()
	for _, fam := range []string{"Noto Naskh Arabic", "Noto Kufi Arabic"} {
		f := &core.Font{Name: fam, Size: 12}
		ids := func(s string) []uint32 {
			sp := e.ShapeRun(f, s)
			var out []uint32
			for li := range sp.Lines {
				for ri := range sp.Lines[li].Runs {
					for _, g := range sp.Lines[li].Runs[ri].raw.Glyphs {
						out = append(out, uint32(g.GlyphID))
					}
				}
			}
			return out
		}
		run := ids("ليك") // lam + yeh + kaf: initial, MEDIAL, final
		if len(run) != 3 {
			t.Errorf("%s: run %v should be 3 contextual glyphs", fam, run)
			continue
		}
		iso := map[uint32]bool{}
		for _, s := range []string{"ل", "ي", "ك"} {
			for _, id := range ids(s) {
				iso[id] = true
			}
		}
		for i, id := range run {
			if iso[id] {
				t.Errorf("%s: run glyph %d (id %d) is an ISOLATED form — cursive joining is not being applied", fam, i, id)
			}
		}
	}
}
