package text

import (
	"testing"

	gtfont "github.com/go-text/typesetting/font"

	"github.com/phroun/kittytk/core"
)

// The terminal's per-cell Arabic shaper emits precomposed Presentation-Forms-B
// codepoints (isolated/initial/medial/final + the lam-alef ligature), one per
// cell. This locks in that the engine's fallback chain resolves those forms to
// a face that actually HAS them — deterministically, via the embedded Noto
// Naskh Arabic — so joined Arabic never depends on a system face being present
// or winning fallback (the regression seen once the gfx path moved onto the
// shared engine + LoadMacMenuFont).
//
// It doubles as a diagnostic: run with -v to print which family resolves each
// form. On a host WITH system fonts loaded (call LoadSystemFallbacks first) the
// log shows whether a system face would otherwise shadow the embedded one.
func TestArabicPresentationFormsResolve(t *testing.T) {
	db := newFontDB()
	// The terminal primary is the monospace face (ui-term default); it has no
	// Arabic, so resolution must fall through to the Arabic face.
	primary := db.resolve(&core.Font{Name: "Noto Sans Mono"})
	fm := fallbackMap{db: db, primary: primary}

	forms := []struct {
		r    rune
		name string
	}{
		{0xFEE1, "meem isolated"}, {0xFEFC, "lam-alef ligature"},
		{0xFEB4, "seen medial"}, {0xFEDF, "lam initial"}, {0xFE8D, "alef isolated"},
		{0xFEE4, "meem initial"}, {0xFEF4, "yeh medial"}, {0xFE97, "teh initial"},
	}
	for _, f := range forms {
		face := fm.ResolveFace(f.r)
		if face == nil {
			t.Errorf("U+%04X (%s): no face resolved", f.r, f.name)
			continue
		}
		if _, ok := face.NominalGlyph(f.r); !ok {
			t.Errorf("U+%04X (%s): resolved face %q lacks the glyph (would render .notdef/isolated)",
				f.r, f.name, familyName(face))
			continue
		}
		t.Logf("U+%04X (%s) -> %s", f.r, f.name, familyName(face))
	}

	// Ordering guarantee: even after system fonts join the chain (the shared
	// raster engine also runs LoadSystemFallbacks + LoadMacMenuFont), the
	// EMBEDDED Arabic — registered first — still wins, so a modern system face
	// that renders precomposed presentation forms isolated can't shadow it.
	eng := NewEngine()
	eng.LoadSystemFallbacks()
	fm2 := fallbackMap{db: eng.db, primary: eng.db.resolve(&core.Font{Name: "Noto Sans Mono"})}
	if got := familyName(fm2.ResolveFace(0xFEDF)); got != "Noto Naskh Arabic" {
		t.Errorf("with system fallbacks loaded, U+FEDF resolved to %q, want Noto Naskh Arabic "+
			"(embedded must win over system faces)", got)
	}
}

// Both Arabic styles are embedded: Naskh is the default (asserted above), Kufi
// is addressable by name for a geometric/display look. Coverage is asserted on
// the BASE letters: the renderer shapes base characters through the font's own
// GSUB (the legacy presentation-form block is not required — the embedded
// archive Kufi build omits it).
func TestArabicKufiAvailableByName(t *testing.T) {
	db := newFontDB()
	face := db.resolve(&core.Font{Name: "Noto Kufi Arabic"})
	if got := familyName(face); got != "Noto Kufi Arabic" {
		t.Fatalf("resolve by name = %q, want Noto Kufi Arabic", got)
	}
	for _, r := range []rune{0x0644, 0x064A, 0x0643, 0x0640} { // lam yeh kaf tatweel
		if _, ok := face.NominalGlyph(r); !ok {
			t.Errorf("Noto Kufi Arabic lacks base U+%04X", r)
		}
	}
}

// Hebrew: Serif is the default fallback (more legible, clearest niqqud), Sans
// stays addressable by name. Covers a base letter (alef) and a niqqud mark.
func TestHebrewDefaultsSerif(t *testing.T) {
	db := newFontDB()
	primary := db.resolve(&core.Font{Name: "Noto Sans Mono"}) // no Hebrew
	fm := fallbackMap{db: db, primary: primary}
	for _, r := range []rune{0x05D0 /* alef */, 0x05B4 /* hiriq niqqud */} {
		if got := familyName(fm.ResolveFace(r)); got != "Noto Serif Hebrew" {
			t.Errorf("U+%04X fell back to %q, want Noto Serif Hebrew (the default)", r, got)
		}
	}
	if got := familyName(db.resolve(&core.Font{Name: "Noto Sans Hebrew"})); got != "Noto Sans Hebrew" {
		t.Errorf("Noto Sans Hebrew should still resolve by name, got %q", got)
	}
}

func familyName(f *gtfont.Face) string {
	if f == nil {
		return "<nil>"
	}
	return f.Describe().Family
}
