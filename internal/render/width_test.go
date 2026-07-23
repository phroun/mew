package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripAnsi(s string) string { return ansiRe.ReplaceAllString(s, "") }

const (
	combAcute = "\u0301" // combining acute accent (zero width)
	zwsp      = "\u200B" // zero-width space
	zwj       = "\u200D" // zero-width joiner
	bom       = "\uFEFF" // zero-width no-break space
	hebrewDot = "\u0597" // hebrew accent revia (combining)
	repl      = "\uFFFD" // replacement character
)

func testRenderer() (*ScreenRenderer, *window.Window) {
	wm := window.NewManager()
	lm := window.NewLayoutManager(wm)
	sr := NewScreenRenderer(wm, lm)
	w := &window.Window{Type: window.DocWindow}
	return sr, w
}

func widthOf(sr *ScreenRenderer, w *window.Window, s string) int {
	total := 0
	for _, r := range s {
		total += sr.getRuneVisualWidth(r, total, w)
	}
	return total
}

func TestWidthClassification(t *testing.T) {
	sr, w := testRenderer()
	cases := []struct {
		name string
		s    string
		want int
	}{
		{"ascii", "abc", 3},
		{"combining acute", "e" + combAcute, 1},
		{"hebrew combining accent", "a" + hebrewDot, 1},
		{"zero-width space", "a" + zwsp + "b", 2},
		{"zwj", "a" + zwj + "b", 2},
		{"bom", bom + "hi", 2},
		{"wide cjk", "日本", 4},
		{"replacement char", repl, 1},
		{"control char", "\x01", 2},
		{"garbled sample", "p" + repl + "~1F" + hebrewDot + repl + "ɠk" + repl + repl + repl + "^", 12},
	}
	for _, c := range cases {
		if got := widthOf(sr, w, c.s); got != c.want {
			t.Errorf("%s: width(%q) = %d, want %d", c.name, c.s, got, c.want)
		}
	}
}

// Rendered line width must equal the requested width exactly, whatever mix
// of combining, zero-width, wide, control, and tab content the line holds.
func TestPrepareLineWidthExact(t *testing.T) {
	sr, w := testRenderer()
	lines := []string{
		"plain ascii text",
		"pe" + combAcute + "che" + combAcute + " marks",
		"a" + zwsp + "b zero width",
		"日本語 wide",
		"p" + repl + "~1F" + hebrewDot + repl + "ɠk" + repl + repl + repl + "^",
		"mixed 日x" + combAcute + zwsp + "本",
		"tabs\tand\tmore",
		"ctrl\x01chars",
	}
	for _, line := range lines {
		for _, width := range []int{3, 4, 10, 40} {
			for _, offset := range []int{0, 1, 2} {
				out := stripAnsi(sr.prepareLineForDisplay(line, "\n", width, offset, w, 0, selectionRange{}, nil, nil))
				if got := widthOf(sr, w, out); got != width {
					t.Errorf("line %q width %d offset %d: rendered %d columns (%q)",
						line, width, offset, got, out)
				}
			}
		}
	}
}

func TestPrepareLineKeepsCombiningMarks(t *testing.T) {
	sr, w := testRenderer()
	out := stripAnsi(sr.prepareLineForDisplay("e"+combAcute+"x", "\n", 10, 0, w, 0, selectionRange{}, nil, nil))
	if !strings.Contains(out, "e"+combAcute) {
		t.Errorf("combining mark lost: %q", out)
	}
}

// A trailing combining mark on the last visible cell must not fake a
// truncation indicator.
func TestNoFalseTruncationFromTrailingMark(t *testing.T) {
	sr, w := testRenderer()
	out := stripAnsi(sr.prepareLineForDisplay("abe"+combAcute, "\n", 3, 0, w, 0, selectionRange{}, nil, nil))
	if strings.Contains(out, sr.indicators.TruncationRight) {
		t.Errorf("false truncation for %q", out)
	}
	if !strings.Contains(out, "e"+combAcute) {
		t.Errorf("trailing mark lost: %q", out)
	}
}

// A wide char that only half-fits at the right edge renders a placeholder,
// never an overflowing row.
func TestWideCharRightEdge(t *testing.T) {
	sr, w := testRenderer()
	out := stripAnsi(sr.prepareLineForDisplay("ab日", "\n", 3, 0, w, 0, selectionRange{}, nil, nil))
	if got := widthOf(sr, w, out); got != 3 {
		t.Errorf("row overflow: %d columns (%q)", got, out)
	}
	if strings.Contains(out, "日") {
		t.Errorf("half-fitting wide char should not render: %q", out)
	}
}

// A combining mark whose base is scrolled off the left edge is dropped with
// its base rather than attaching to the first visible character.
func TestScrolledOffMarkDropped(t *testing.T) {
	sr, w := testRenderer()
	out := stripAnsi(sr.prepareLineForDisplay("e"+combAcute+"xyz", "\n", 3, 1, w, 0, selectionRange{}, nil, nil))
	if strings.Contains(out, combAcute) {
		t.Errorf("orphaned combining mark: %q", out)
	}
	if !strings.HasPrefix(out, "xyz") {
		t.Errorf("scrolled content wrong: %q", out)
	}
}

// A message-bar line (notification/window title bar) that is longer than the
// bar is wide must be ellipsized to exactly the width — never emitted at full
// length, which would wrap onto and blank the row below in the terminal.
func TestMessageBarEllipsizesOverlongContent(t *testing.T) {
	const width = 40
	long := "fsi=first strong isolate, lri=left-to-right isolate, rli=right-to-left-isolate, pdi=pop directional isolate"

	got := stripAnsi(composeMessageBar(long, "", "", width))
	if calculateAnsiAwareLength(got) != width {
		t.Fatalf("bar width = %d, want %d (%q)", calculateAnsiAwareLength(got), width, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("over-long bar should end with an ellipsis: %q", got)
	}
	if !strings.HasPrefix(got, "fsi=first strong") {
		t.Errorf("bar should keep the leading text: %q", got)
	}
}

// A message that already fits is emitted verbatim, padded to the full width.
func TestMessageBarFittingContentUnchanged(t *testing.T) {
	const width = 40
	got := stripAnsi(composeMessageBar("hello", "", "", width))
	if calculateAnsiAwareLength(got) != width {
		t.Fatalf("bar width = %d, want %d (%q)", calculateAnsiAwareLength(got), width, got)
	}
	if strings.Contains(got, "…") {
		t.Errorf("fitting bar should not be ellipsized: %q", got)
	}
	if strings.TrimRight(got, " ") != "hello" {
		t.Errorf("fitting bar content changed: %q", got)
	}
}

// truncateToWidth counts display columns (wide runes = 2) and never splits an
// ANSI escape sequence.
func TestTruncateToWidth(t *testing.T) {
	cases := []struct {
		s       string
		max     int
		wantEll bool
	}{
		{"abcdef", 10, false}, // fits
		{"abcdef", 4, true},   // cut to 3 chars + ellipsis
		{"日本語", 4, true},      // 日(2) + ellipsis = 3 cols, never splitting a wide rune
		{"日本語", 6, false},     // fits exactly (6 cols)
		{"", 5, false},
	}
	for _, c := range cases {
		got := truncateToWidth(c.s, c.max)
		if w := calculateAnsiAwareLength(got); w > c.max {
			t.Errorf("truncateToWidth(%q,%d) = %q width %d exceeds max", c.s, c.max, got, w)
		}
		if c.wantEll && !strings.HasSuffix(got, "…") {
			t.Errorf("truncateToWidth(%q,%d) = %q, expected ellipsis", c.s, c.max, got)
		}
		if !c.wantEll && strings.Contains(got, "…") {
			t.Errorf("truncateToWidth(%q,%d) = %q, unexpected ellipsis", c.s, c.max, got)
		}
	}
}

// The truncation indicator replaces the last cell and pads to the exact
// width even when that cell was wider than one column (wide rune or tab).
func TestTruncationIndicatorPadsWideCell(t *testing.T) {
	sr, w := testRenderer()
	out := stripAnsi(sr.prepareLineForDisplay("日本語 wide", "\n", 4, 0, w, 0, selectionRange{}, nil, nil))
	if got := widthOf(sr, w, out); got != 4 {
		t.Errorf("truncated row width %d, want 4 (%q)", got, out)
	}
	if !strings.Contains(out, sr.indicators.TruncationRight) {
		t.Errorf("expected truncation indicator: %q", out)
	}
}

// Under flipBidiForHost, selection on a line CARRYING COMBINING MARKS uses
// the ride-safe (foreground+bold, no background) style — the bar drifts on
// such lines in a bidi-applying terminal. Mark-free lines (English; Arabic,
// which mew pre-shapes to single presentation forms) keep the real bar, and
// the bar is always used when flip is off.
func TestFlipSelectionRideSafeOnMarkedLines(t *testing.T) {
	const (
		bar     = "\x1b[0;30;47m" // normal selection (bg fill)
		flipSel = "\x1b[0;1;93m"  // ride-safe selection (fg+bold)
	)
	sr, w := testRenderer()
	whole := selectionRange{startLine: 0, endLine: 0, startRune: 0, endRune: 50, exists: true}

	render := func(line string, flip bool) string {
		sr.frame.flipBidi = flip
		return sr.prepareLineForDisplay(line, "\n", 40, 0, w, 0, whole, nil, nil)
	}

	marked := "a" + hebrewDot + "b" // a base + a combining mark + b
	plain := "abc"

	// flip + marks -> ride-safe fg+bold, never the bar.
	out := render(marked, true)
	if !strings.Contains(out, flipSel) {
		t.Errorf("flip+marks should use the ride-safe selection: %q", out)
	}
	if strings.Contains(out, bar) {
		t.Errorf("flip+marks must NOT emit the background bar: %q", out)
	}

	// flip + NO marks -> the real bar (English keeps its selection).
	out = render(plain, true)
	if !strings.Contains(out, bar) {
		t.Errorf("flip without marks should keep the bar: %q", out)
	}
	if strings.Contains(out, flipSel) {
		t.Errorf("flip without marks must not use the ride-safe style: %q", out)
	}

	// flip OFF + marks -> the real bar (every non-flip terminal unchanged).
	out = render(marked, false)
	if !strings.Contains(out, bar) {
		t.Errorf("no-flip should keep the bar even with marks: %q", out)
	}
	if strings.Contains(out, flipSel) {
		t.Errorf("no-flip must never use the ride-safe style: %q", out)
	}
}

// lineHasZeroWidth flags combining marks (and zero-width joiners) but not
// plain text, control characters, or wide glyphs.
func TestLineHasZeroWidth(t *testing.T) {
	cases := map[string]bool{
		"abc":               false,
		"日本語":               false,
		"ctrl\x01char":      false, // control -> ^X (two cells), not zero width
		"e" + combAcute:     true,
		"a" + hebrewDot:     true,
		"x" + zwj + "y":     true,
	}
	for s, want := range cases {
		if got := lineHasZeroWidth(s); got != want {
			t.Errorf("lineHasZeroWidth(%q) = %v, want %v", s, got, want)
		}
	}
}

// rtlCombining off (ViewState.SuppressRTLCombining) drops combining marks
// that ride RTL letters from the DISPLAY, so pointed RTL renders unpointed;
// and with the marks gone the flip-mode selection uses the real bar, not
// the ride-safe fallback. LTR combining marks are never suppressed.
func TestSuppressRTLCombining(t *testing.T) {
	sr, w := testRenderer()
	// A Hebrew base + niqqud (qamats, Mn) then a Latin base + acute (Mn).
	line := "שָ" + "e" + combAcute // shin+qamats, e+acute
	whole := selectionRange{startLine: 0, endLine: 0, startRune: 0, endRune: 50, exists: true}

	render := func() string {
		return stripAnsi(sr.prepareLineForDisplay(line, "\n", 40, 0, w, 0, whole, nil, nil))
	}

	// Marks shown (default): both marks present.
	w.ViewState.SuppressRTLCombining = false
	out := render()
	if !strings.Contains(out, "ָ") {
		t.Errorf("default should keep the Hebrew niqqud: %q", out)
	}

	// Suppressed: the RTL niqqud is gone, the LTR acute stays, the bases stay.
	w.ViewState.SuppressRTLCombining = true
	out = render()
	if strings.Contains(out, "ָ") {
		t.Errorf("rtlCombining off must drop the Hebrew niqqud: %q", out)
	}
	if !strings.Contains(out, "ש") {
		t.Errorf("the Hebrew base letter must remain: %q", out)
	}
	if !strings.Contains(out, "́") {
		t.Errorf("an LTR combining mark must NOT be suppressed: %q", out)
	}
}

// With rtlCombining off, the flip-mode selection reverts to the real bar
// (the marks are stripped, so no drift).
func TestFlipSelectionBarWhenCombiningSuppressed(t *testing.T) {
	const (
		bar     = "\x1b[0;30;47m"
		flipSel = "\x1b[0;1;93m"
	)
	sr, w := testRenderer()
	sr.frame.flipBidi = true
	line := "שָם" // pointed Hebrew (has an RTL combining mark)
	whole := selectionRange{startLine: 0, endLine: 0, startRune: 0, endRune: 50, exists: true}

	// rtlCombining ON -> ride-safe selection (marks emitted, bar would drift).
	w.ViewState.SuppressRTLCombining = false
	if out := sr.prepareLineForDisplay(line, "\n", 40, 0, w, 0, whole, nil, nil); !strings.Contains(out, flipSel) {
		t.Errorf("flip + marks shown should use the ride-safe selection: %q", out)
	}

	// rtlCombining OFF -> the real bar (marks suppressed, no drift).
	w.ViewState.SuppressRTLCombining = true
	out := sr.prepareLineForDisplay(line, "\n", 40, 0, w, 0, whole, nil, nil)
	if !strings.Contains(out, bar) {
		t.Errorf("flip + marks suppressed should use the real selection bar: %q", out)
	}
	if strings.Contains(out, flipSel) {
		t.Errorf("suppressed marks must not trigger the ride-safe selection: %q", out)
	}
}
