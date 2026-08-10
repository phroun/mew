package keys

import (
	"reflect"
	"testing"
)

// A Glyph chord (the AltGr/Level3 modifier, prefix "G-") with no binding
// unrolls to inserting the composed character it carries, so an
// international user's AltGr keystrokes still type by default.
func TestGlyphChordUnrollsToInsert(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{})
	sp.ProcessKey("G-€")
	want := []string{"G-€→insert '€'"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v — an unbound Glyph chord must self-insert its glyph", h.calls, want)
	}
}

// A binding on the Glyph chord wins over the self-insert fallback, so a user
// can remap an AltGr key — e.g. to spell out "EUR" in a 7-bit ASCII document.
func TestGlyphChordBindingOverridesInsert(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"G-€": "insert 'EUR'",
	})
	sp.ProcessKey("G-€")
	want := []string{"G-€→insert 'EUR'"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("dispatched %v, want %v — a Glyph binding must outrank the self-insert fallback", h.calls, want)
	}
}
