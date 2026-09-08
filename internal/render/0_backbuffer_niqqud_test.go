package render

import "testing"

// TestFrameHasUncomposedNiqqud checks the per-frame scan that drives the Kitty
// force_ltr nudge: it fires on a Hebrew base carrying a real combining mark, and
// only when that mark survives the active rtlMarkMode fold.
func TestFrameHasUncomposedNiqqud(t *testing.T) {
	// Plain ASCII: nothing to warn about.
	b := newBackBuffer(10, 2)
	paint(b, mv(1, 1), wr("hello"))
	if b.frameHasUncomposedNiqqud() {
		t.Errorf("ASCII text should report no uncomposed niqqud")
	}

	// Alef + hiriq (U+05B4 has no presentation form): a real combining mark that
	// no fold removes, so it counts in every mode.
	b = newBackBuffer(10, 2)
	b.rtlMarkMode = "normal"
	paint(b, mv(1, 1), wr("אִ"))
	if !b.frameHasUncomposedNiqqud() {
		t.Errorf("alef+hiriq should count as uncomposed niqqud")
	}

	// Shin + shin-dot folds to a single presentation form (U+FB2A) under a
	// folding mode -> no surviving combining mark.
	b = newBackBuffer(10, 2)
	b.rtlMarkMode = "compose"
	paint(b, mv(1, 1), wr("שׁ"))
	if b.frameHasUncomposedNiqqud() {
		t.Errorf("shin+shin-dot should fold away under compose mode")
	}

	// The same cluster under normal mode is emitted as base + combining mark.
	b = newBackBuffer(10, 2)
	b.rtlMarkMode = "normal"
	paint(b, mv(1, 1), wr("שׁ"))
	if !b.frameHasUncomposedNiqqud() {
		t.Errorf("shin+shin-dot stays a combining mark under normal mode")
	}
}
