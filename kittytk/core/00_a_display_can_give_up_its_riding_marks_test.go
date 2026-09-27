package core

import "testing"

// A display that wants its selection bars, its highlights and its gutter more
// than its vowels gives up the marks that ride a right-to-left letter: pointed
// Hebrew renders one codepoint per cell, the way pre-shaped Arabic already
// does, and nothing is left on the line for a reordering terminal to miscount a
// fill over.
//
// A folding rtl-mark mode keeps the points even so, folded into their letters,
// so only the vowels and accents go.
func TestACellRunCanGiveUpItsRidingMarks(t *testing.T) {
	t.Cleanup(func() {
		SetRtlCombining(true)
		SetRtlMarkMode("")
	})
	const (
		vowel  = "ְ" // sheva: nothing to fold into
		dagesh = "ּ" // a point: folds into its letter
	)
	// One Hebrew word, pointed. Read back in the order it is drawn.
	const text = "ש" + dagesh + vowel + "ם"

	// Shown by default: every mark is there.
	SetRtlCombining(true)
	if got := CellRun(text, DirLTR); len([]rune(got)) != 4 {
		t.Errorf("with the marks shown the run lost some: %q", got)
	}

	// Given up, with nothing to fold into: the bare letters.
	SetRtlCombining(false)
	SetRtlMarkMode("")
	got := CellRun(text, DirLTR)
	if want := "םש"; got != want {
		t.Errorf("with the marks given up: %q, want the bare letters %q", got, want)
	}

	// Given up under folding: the point folds into its letter and survives as
	// one glyph, and only the vowel goes.
	SetRtlMarkMode("compose")
	got = CellRun(text, DirLTR)
	if want := "םשּ"; got != want {
		t.Errorf("under folding: %q, want the point folded in %q", got, want)
	}
}

// A caller that asked WHERE each rune went still gets an answer for every one
// of them: a mark that is not drawn reports no cell, the way an absorbed
// ligature does.
func TestAMarkGivenUpReportsNoCell(t *testing.T) {
	t.Cleanup(func() { SetRtlCombining(true) })
	SetRtlCombining(false)

	runes := []rune("שְם")
	run, where := CellRunMapped(string(runes), DirLTR)
	if len(where) != len(runes) {
		t.Fatalf("got %d answers for %d runes", len(where), len(runes))
	}
	if where[1] != -1 {
		t.Errorf("the mark reported cell %d, want none", where[1])
	}
	for _, i := range []int{0, 2} {
		if where[i] < 0 || where[i] >= len(run) {
			t.Errorf("rune %d reported cell %d, outside the run of %d",
				i, where[i], len(run))
		}
	}
}

// And a pixel target is untouched: it composes the marks properly itself, and
// there is no terminal at the end of it to disagree with.
func TestAPixelTargetKeepsItsMarks(t *testing.T) {
	t.Cleanup(func() {
		SetRtlCombining(true)
		SetTextMeasurer(nil)
	})
	SetRtlCombining(false)
	SetTextMeasurer(pixelMeasurer{})

	const text = "שְם"
	if got := CellRun(text, DirLTR); got != text {
		t.Errorf("a pixel target lost marks it draws correctly: %q", got)
	}
}

// pixelMeasurer stands in for a graphical target's own metrics: its presence is
// what says a shaper is drawing rather than a cell grid.
type pixelMeasurer struct{}

func (pixelMeasurer) MeasureText(*Font, string) Unit { return 0 }
