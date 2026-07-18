package bidi

import "testing"

// visual renders the permutation as a string for easy comparison.
func visual(s string, baseRTL bool) string {
	runes := []rune(s)
	l := Compute(runes, baseRTL)
	if l == nil {
		return s
	}
	out := make([]rune, 0, len(runes))
	for _, li := range l.Perm {
		out = append(out, runes[li])
	}
	return string(out)
}

func TestPureLTRIsIdentity(t *testing.T) {
	if l := Compute([]rune("hello world"), false); l != nil {
		t.Fatal("pure LTR under LTR base should need no layout")
	}
}

func TestHebrewRunReverses(t *testing.T) {
	// "abc שלום xyz": the Hebrew word reverses in place.
	got := visual("abc שלום xyz", false)
	want := "abc םולש xyz"
	if got != want {
		t.Fatalf("visual: %q, want %q", got, want)
	}
}

func TestNumberInsideRTLRegionMirrors(t *testing.T) {
	// Hebrew, number, Hebrew: the whole region mirrors around the number,
	// digits keep their order. Logical: אבג 123 דהו
	got := visual("x אבג 123 דהו y", false)
	// Visual: x [והד 123 גבא] y — the second word first, digits intact.
	want := "x והד 123 גבא y"
	if got != want {
		t.Fatalf("visual: %q, want %q", got, want)
	}
}

func TestTwoSeparateRTLWords(t *testing.T) {
	// RTL words separated by LTR text mirror independently.
	got := visual("אב cd גד", false)
	// Base LTR: each Hebrew word reverses in place... but the space-separated
	// pair around LTR text forms two separate regions.
	want := "בא cd דג"
	if got != want {
		t.Fatalf("visual: %q, want %q", got, want)
	}
}

func TestBaseRTLWholeLineMirrors(t *testing.T) {
	// RTL base: the line reads from the right; the LTR word stays intact as
	// a unit. Logical: אבג abc דהו
	got := visual("אבג abc דהו", true)
	want := "והד abc גבא"
	if got != want {
		t.Fatalf("visual: %q, want %q", got, want)
	}
}

func TestCombiningMarkStaysWithBase(t *testing.T) {
	// A Hebrew letter with a combining point: the mark must still follow its
	// base in the visual sequence so terminals compose it.
	s := "אְב" // alef + sheva, bet
	runes := []rune(s)
	l := Compute(runes, false)
	if l == nil {
		t.Fatal("expected a layout")
	}
	// Visual: bet first, then alef followed by its mark.
	want := []int{2, 0, 1}
	for i, li := range l.Perm {
		if li != want[i] {
			t.Fatalf("perm: %v, want %v", l.Perm, want)
		}
	}
}

func TestRTLAt(t *testing.T) {
	runes := []rune("ab שלום cd")
	if RTLAt(runes, 0, false) {
		t.Fatal("'a' is not RTL")
	}
	if !RTLAt(runes, 4, false) {
		t.Fatal("hebrew rune should report RTL")
	}
	if RTLAt([]rune(""), 0, false) {
		t.Fatal("empty line under LTR base is not RTL")
	}
	if !RTLAt([]rune(""), 0, true) {
		t.Fatal("empty line under RTL base is RTL")
	}
}

func TestMirrorBrackets(t *testing.T) {
	if Mirror('(') != ')' || Mirror(')') != '(' || Mirror('[') != ']' {
		t.Fatal("bracket mirroring failed")
	}
	if Mirror('a') != 'a' {
		t.Fatal("non-bracket must be unchanged")
	}
}

// markedVisual renders a marked layout as a string, with marker slots as
// their glyphs.
func markedVisual(s string, baseRTL bool) string {
	runes := []rune(s)
	l := ComputeMarked(runes, baseRTL)
	if l == nil {
		return s
	}
	out := make([]rune, 0, len(l.Perm))
	for _, li := range l.Perm {
		switch li {
		case MarkerLTR:
			out = append(out, '>')
		case MarkerRTL:
			out = append(out, '<')
		case MarkerEnd:
			out = append(out, '|')
		default:
			out = append(out, runes[li])
		}
	}
	return string(out)
}

// Direction markers sit at each fragment's leading edge: "<" at the RIGHT of
// an RTL fragment, ">" at the LEFT of a returning LTR fragment. The
// line-initial natural fragment is unmarked.
func TestMarkedLayout(t *testing.T) {
	if got := markedVisual("abc שלום xyz", false); got != "abc |םולש<> xyz|" {
		t.Fatalf("marked visual: %q", got)
	}
	// Line STARTING with a foreign fragment: it gets a marker too.
	if got := markedVisual("שלום abc", false); got != "|םולש<> abc|" {
		t.Fatalf("foreign-start marked visual: %q", got)
	}
	// Pure natural line: no layout at all.
	if ComputeMarked([]rune("plain text"), false) != nil {
		t.Fatal("pure LTR line must not produce a marked layout")
	}
	// RTL base: the line has THREE fragments (the spaces join the RTL runs
	// under an RTL base). The line-initial RTL fragment is natural and
	// unmarked; the embedded LTR word gets ">" at its left (leading) edge;
	// the SECOND RTL fragment is non-initial, so it too is marked — "<" at
	// its right (leading) edge, just left of the LTR word.
	if got := markedVisual("אבג abc דהו", true); got != "|והד <>abc| גבא" {
		t.Fatalf("rtl-base marked visual: %q", got)
	}
}

// A fragment led by an explicit direction-control character gets no synthetic
// marker — the control itself represents the transition (and renders as the
// marker glyph, one column wide, at the fragment's leading edge).
func TestMarkedLayoutExplicitControl(t *testing.T) {
	s := "ab‏אב cd" // RLM leads the RTL fragment
	runes := []rune(s)
	l := ComputeMarked(runes, false)
	if l == nil {
		t.Fatal("expected a layout")
	}
	// No synthetic marker for the RTL fragment; the RLM (logical 2) is its
	// own marker, emitted at the fragment's rightmost slot.
	sawRTLMarker := false
	for _, li := range l.Perm {
		if li == MarkerRTL {
			sawRTLMarker = true
		}
	}
	if sawRTLMarker {
		t.Fatal("control-led fragment must not get a synthetic RTL marker")
	}
	if !IsDirectionControl(runes[2]) {
		t.Fatal("RLM should be a direction control")
	}
	// The returning LTR fragment still gets its synthetic marker.
	sawLTR := false
	for _, li := range l.Perm {
		if li == MarkerLTR {
			sawLTR = true
		}
	}
	if !sawLTR {
		t.Fatal("the returning LTR fragment should still be marked")
	}
}

// Arabic content shapes to presentation forms; a full render never emits the
// raw base letters for a shaped run (that would show isolated forms).
func TestShapeRenderReplacesBase(t *testing.T) {
	g := Shape([]rune("سلام"))
	for _, r := range g {
		if r >= 0x0600 && r <= 0x064A {
			// only a base letter would land here; all four should be FExx
			t.Fatalf("shaped output still contains a base letter U+%04X", r)
		}
	}
}
