package trinkets

import (
	"reflect"
	"testing"
)

// A title marks its accelerator with &, escapes a literal one as &&, and may
// mark SEVERAL letters — which reads as a preference list rather than a
// mistake. The parser used to keep only the first and discard the rest, so a
// backup letter could be written but never reached.
func TestParseAcceleratorTitleCollectsEveryCandidate(t *testing.T) {
	for _, c := range []struct {
		raw     string
		display string
		accels  []acceleratorCandidate
	}{
		{"&File", "File", []acceleratorCandidate{{'f', 0}}},
		{"E&xit", "Exit", []acceleratorCandidate{{'x', 1}}},
		{"&Hel&p", "Help", []acceleratorCandidate{{'h', 0}, {'p', 3}}},
		{"&A&B&C", "ABC", []acceleratorCandidate{{'a', 0}, {'b', 1}, {'c', 2}}},

		// A literal ampersand marks nothing, and does not swallow the letter
		// after it.
		{"Save && Exit", "Save & Exit", nil},
		{"R&&D &Report", "R&D Report", []acceleratorCandidate{{'r', 4}}},

		// Case is folded for matching but kept for display.
		{"&Window", "Window", []acceleratorCandidate{{'w', 0}}},

		// Degenerate markup: a trailing & is dropped rather than kept.
		{"Help&", "Help", nil},
		{"", "", nil},
	} {
		display, accels := parseAcceleratorTitle(c.raw)
		if display != c.display {
			t.Errorf("%q: display = %q, want %q", c.raw, display, c.display)
		}
		if !reflect.DeepEqual(accels, c.accels) {
			t.Errorf("%q: accels = %v, want %v", c.raw, accels, c.accels)
		}
	}
}

// The leading candidate is what a title means before anything has claimed its
// letters, which is what every caller wants at construction time.
func TestFirstAccelerator(t *testing.T) {
	_, accels := parseAcceleratorTitle("&Hel&p")
	if ch, pos := firstAccelerator(accels); ch != 'h' || pos != 0 {
		t.Errorf("firstAccelerator = (%q, %d), want ('h', 0)", ch, pos)
	}
	if ch, pos := firstAccelerator(nil); ch != 0 || pos != -1 {
		t.Errorf("firstAccelerator(nil) = (%q, %d), want (0, -1)", ch, pos)
	}
}

// Marking several letters must not change what the label reads as: the extra
// markers are instructions to the assigner, not text.
func TestCandidateMarkupIsInvisible(t *testing.T) {
	plain, _ := parseAcceleratorTitle("Help")
	marked, accels := parseAcceleratorTitle("&Hel&p")
	if plain != marked {
		t.Errorf("marking backups changed the display text: %q vs %q", plain, marked)
	}
	if len(accels) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(accels))
	}
	// Each position indexes the display text, not the raw markup.
	runes := []rune(marked)
	for _, a := range accels {
		if a.Pos < 0 || a.Pos >= len(runes) {
			t.Fatalf("candidate %v out of range for %q", a, marked)
		}
		if got := []rune(lowerRune(runes[a.Pos])); got[0] != a.Char {
			t.Errorf("candidate %v points at %q in %q", a, runes[a.Pos], marked)
		}
	}
}

// A menu item and a menu both keep the whole list, so the greedy assignment
// has backups to fall to; the chosen one starts as the first.
func TestItemAndMenuKeepCandidates(t *testing.T) {
	item := NewMenuItem("&Hel&p")
	if len(item.acceleratorCandidates) != 2 {
		t.Errorf("item kept %d candidates, want 2", len(item.acceleratorCandidates))
	}
	if item.acceleratorChar != 'h' || item.acceleratorPos != 0 {
		t.Errorf("item chose (%q, %d), want ('h', 0)", item.acceleratorChar, item.acceleratorPos)
	}

	menu := NewMenu("&Hel&p")
	if len(menu.acceleratorCandidates) != 2 {
		t.Errorf("menu kept %d candidates, want 2", len(menu.acceleratorCandidates))
	}
	if menu.acceleratorChar != 'h' || menu.acceleratorPos != 0 {
		t.Errorf("menu chose (%q, %d), want ('h', 0)", menu.acceleratorChar, menu.acceleratorPos)
	}

	// Renaming re-reads the candidates rather than keeping the old ones.
	item.SetText("&File")
	if len(item.acceleratorCandidates) != 1 || item.acceleratorChar != 'f' {
		t.Errorf("after SetText: %v / %q", item.acceleratorCandidates, item.acceleratorChar)
	}
}

func lowerRune(r rune) string {
	if r >= 'A' && r <= 'Z' {
		return string(r + 32)
	}
	return string(r)
}
