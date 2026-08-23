package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The registered fallback chain must cover the glyphs trinket chrome
// draws (tree/list markers, scrollbar runes) and the scripts the
// bundled faces promise (Hebrew) - a rune nothing covers renders as
// .notdef tofu.
func TestFallbackCoversUIGlyphsAndHebrew(t *testing.T) {
	db := newFontDB()
	primary := db.resolve(&core.Font{Name: "ui-text", Size: 12})
	m := fallbackMap{db: db, primary: primary}
	for _, r := range []rune{
		'▸', '▶', '▼', '·', // tree/list/menu markers
		'✓', 'ℹ', '⚠', '✖', // menu check and dialog icons
		'│', '░', '█', // scrollbar and divider runes
		'א', 'ש', 'ת', // Hebrew
	} {
		face := m.ResolveFace(r)
		if _, ok := face.NominalGlyph(r); !ok {
			t.Errorf("no registered font covers %q (U+%04X)", string(r), r)
		}
	}
}
