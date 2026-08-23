package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A trailing space occupies its width in a run being MEASURED.
//
// go-text zeroes the advance of a line's trailing whitespace by default, which
// is what a wrapped column wants — whitespace at a soft break must not push the
// line past its width — and not what a measurement wants. A caret goes where a
// run ends, and a run ending in a space ends after the space.
//
// It cost exactly one space, and only where the run had a script change in it:
// "ab " measured wider than "ab", but "a日 " measured the same as "a日", so the
// caret after that space was painted before it instead.
func TestATrailingSpaceIsMeasuredWhenNothingIsWrapping(t *testing.T) {
	e := NewEngine()
	f := &core.Font{Name: "Monday", Size: 12}

	for _, base := range []string{"ab", "a日", "日a", "日"} {
		bare := e.Measure(f, base)
		spaced := e.Measure(f, base+" ")
		if spaced <= bare {
			t.Errorf("%q measures %v and %q measures %v — the trailing space "+
				"takes no width, so a caret after it has nowhere to sit",
				base, bare, base+" ", spaced)
		}
	}
}

// Each space takes its own width, so two are twice one. Whole units cannot say
// that exactly when a space is a fraction of one, hence the tolerance; the
// defect this guards was a whole space wide.
func TestEverySpaceIsMeasuredNotJustTheFirst(t *testing.T) {
	e := NewEngine()
	f := &core.Font{Name: "Monday", Size: 12}

	for _, base := range []string{"ab", "a日", "日"} {
		one := e.Measure(f, base+" ") - e.Measure(f, base)
		two := e.Measure(f, base+"  ") - e.Measure(f, base)
		if two < 2*one-1 || two > 2*one+1 {
			t.Errorf("%q: one space adds %v and two add %v, want about twice",
				base, one, two)
		}
	}
}

// Measuring in pixels rounds ONCE, at the pixel. Measuring in whole units and
// scaling afterwards rounds twice, and where a space is about two and a half
// units — as it is beside CJK — the second one came out a unit shorter than the
// first, enough to read as a caret sitting before a space rather than after it.
func TestPixelMeasurementDoesNotRoundThroughUnits(t *testing.T) {
	e := NewEngine()
	f := &core.Font{Name: "Monday", Size: 12}
	const ppu = 2.0

	base := e.MeasurePx(f, "日", ppu)
	var steps []int
	prev := base
	for _, s := range []string{"日 ", "日  ", "日   ", "日    "} {
		at := e.MeasurePx(f, s, ppu)
		steps = append(steps, at-prev)
		prev = at
	}
	lo, hi := steps[0], steps[0]
	for _, step := range steps {
		if step < lo {
			lo = step
		}
		if step > hi {
			hi = step
		}
	}
	if hi-lo > 1 {
		t.Errorf("spaces advanced by %v — equal characters can differ only by "+
			"the one pixel a whole-pixel position costs", steps)
	}
}
