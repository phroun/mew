package text

import "testing"

// A per-face baseline correction is keyed by FACE, so it applies however the
// face is reached: named directly, or through any alias in the ui tree that
// resolves to it. That is the whole point of keying by family rather than by
// alias — one entry, every route.
func TestBaselineAdjustFollowsEveryRoute(t *testing.T) {
	e := NewEngine()
	if e.BaselineAdjust("Noto Kufi Arabic") != 0 {
		t.Fatal("no correction should be recorded yet")
	}

	e.SetBaselineAdjust("Noto Kufi Arabic", -6)

	for _, name := range []string{
		"Noto Kufi Arabic",    // the face itself
		"noto-kufi-arabic",    // spelling variants must not miss
		"NotoKufiArabic",      //
		"ui-term-arabic-sans", // the leaf alias
		"ui-text-arabic-sans", // the other root's leaf, same family
	} {
		if got := e.BaselineAdjust(name); got != -6 {
			t.Errorf("BaselineAdjust(%q) = %d, want -6", name, got)
		}
	}

	// A face with no correction is untouched, including one reached through
	// the sibling alias.
	for _, name := range []string{"Noto Naskh Arabic", "ui-term-arabic-serif", "ui-term"} {
		if got := e.BaselineAdjust(name); got != 0 {
			t.Errorf("BaselineAdjust(%q) = %d, want 0", name, got)
		}
	}

	// Zero clears it.
	e.SetBaselineAdjust("Noto Kufi Arabic", 0)
	if got := e.BaselineAdjust("ui-term-arabic-sans"); got != 0 {
		t.Errorf("cleared correction still reports %d", got)
	}
}

// Recording a correction invalidates cached rasterizations: masks drawn at the
// old placement must not survive it.
func TestBaselineAdjustBumpsEpoch(t *testing.T) {
	e := NewEngine()
	before := e.Epoch()
	e.SetBaselineAdjust("Noto Kufi Arabic", -3)
	if e.Epoch() == before {
		t.Error("recording a correction should bump the engine epoch")
	}
}
