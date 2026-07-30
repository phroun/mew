package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// mew cannot predict how wide the terminal will draw an anchored defective
// mark: the dotted circle is East Asian Ambiguous, and the mark riding it may
// have no glyph and come back as a spacing .notdef. So the cursor must be
// RE-ADDRESSED after such a cell instead of assumed to have advanced one
// column — otherwise every later cell in the span is misplaced and the row's
// tail is left showing the terminal's own background.
func TestAnchoredCellReanchorsCursor(t *testing.T) {
	b := &backBuffer{}
	b.reshape(40, 1)
	b.begin()
	b.moveTo(0, 0)
	b.writeString("ab" + string(textwidth.MarkAnchor) + "߭" + "cdef")

	var sb strings.Builder
	b.present(&sb)
	out := sb.String()

	i := strings.IndexRune(out, textwidth.MarkAnchor)
	if i < 0 {
		t.Fatalf("the anchor should have been emitted; got %q", out)
	}
	rest := out[i:]
	// Between the anchored pair and the next ordinary glyph there must be a
	// cursor address.
	cup := regexp.MustCompile(`\x1b\[\d+;\d+H`)
	loc := cup.FindStringIndex(rest)
	c := strings.IndexRune(rest, 'c')
	if loc == nil || c < 0 || loc[0] > c {
		t.Fatalf("expected a CUP between the anchored cell and the next glyph; got %q", rest)
	}
}

// An ordinary row still emits as one contiguous span — the re-anchoring must
// not fragment normal text, which would break Arabic shaping and grapheme
// joining across the seam.
func TestPlainRowStaysOneSpan(t *testing.T) {
	b := &backBuffer{}
	b.reshape(40, 1)
	b.begin()
	b.moveTo(0, 0)
	b.writeString("abcdefghij")

	var sb strings.Builder
	b.present(&sb)
	out := sb.String()
	// The glyph run itself must be unbroken: no escape may be injected
	// between adjacent letters, which the terminal needs for Arabic shaping
	// and grapheme joining.
	if !strings.Contains(out, "abcdefghij") {
		t.Fatalf("plain row should emit as one contiguous glyph run; got %q", out)
	}
}
