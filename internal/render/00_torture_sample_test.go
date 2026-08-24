package render

import (
	"os"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/viewport"
)

// Render the generated torture sample and assert the two invariants the
// reported bugs broke: no row overruns the width it was given, and no
// control character reaches the terminal as itself.
func TestTortureSample(t *testing.T) {
	// testdata/lorem-torture.txt is lorem ipsum with a high density of every
	// class that broke rendering; testdata/generate.py rebuilds it.
	data, err := os.ReadFile("testdata/lorem-torture.txt")
	if err != nil {
		t.Fatalf("torture sample missing: %v", err)
	}

	defective, controls, rows := 0, 0, 0
	for _, line := range strings.Split(string(data), "\n") {
		runes := []rune(line)
		for i, r := range runes {
			if textwidth.IsControl(r) {
				controls++
			}
			if textwidth.IsMark(r) {
				var base rune
				for j := i - 1; j >= 0; j-- {
					if !textwidth.IsMark(runes[j]) {
						base = runes[j]
						break
					}
				}
				if textwidth.DefectiveMark(base, r) {
					defective++
				}
			}
		}
		for _, cfg := range []struct {
			name string
			mut  func(w *viewport.Viewport)
		}{
			{"default", func(w *viewport.Viewport) {}},
			{"invisibles", func(w *viewport.Viewport) { w.ViewState.ShowInvisibles = true }},
			{"rtl", func(w *viewport.Viewport) { w.ViewState.Direction = "rtl" }},
			{"bidi", func(w *viewport.Viewport) { w.ViewState.ShowBidi = true }},
		} {
			for _, width := range []int{20, 40, 80} {
				for _, voff := range []int{0, 5, 17} {
					sr, w := testRenderer()
					cfg.mut(w)
					out := sr.prepareLineForDisplay(line, "\n", width, voff, w, 0, selectionRange{}, nil, nil)
					plain := stripAnsi(out)
					rows++
					if got := termCols(plain); got != width {
						t.Errorf("%s w=%d off=%d: row occupies %d columns: %q",
							cfg.name, width, voff, got, plain)
					}
					for _, r := range plain {
						if r == '\t' {
							continue
						}
						if textwidth.IsControl(r) {
							t.Errorf("%s w=%d off=%d: RAW control U+%04X reached the terminal",
								cfg.name, width, voff, r)
						}
					}
				}
			}
		}
	}
	t.Logf("rendered %d rows; sample holds %d control chars and %d defective marks",
		rows, controls, defective)
}
