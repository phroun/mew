package text

import (
	"image"
	"image/color"
	"reflect"
	"testing"

	"golang.org/x/image/font/gofont/gomono"

	"github.com/phroun/kittytk/core"
)

func sans(size int) *core.Font { return &core.Font{Name: "Noto Sans", Size: size} }
func mono(size int) *core.Font { return &core.Font{Name: "Noto Sans Mono", Size: size} }
func boldSans(size int) *core.Font {
	return &core.Font{Name: "Noto Sans", Size: size, Style: core.FontStyleBold}
}

func TestMeasureProportionalVsMono(t *testing.T) {
	e := NewEngine()

	// Real proportional metrics: 'iii' is narrower than 'MMM'.
	narrow := e.Measure(sans(12), "iii")
	wide := e.Measure(sans(12), "MMM")
	if narrow <= 0 || wide <= 0 {
		t.Fatalf("zero measurements: iii=%d MMM=%d", narrow, wide)
	}
	if narrow >= wide {
		t.Errorf("sans should be proportional: iii=%d MMM=%d", narrow, wide)
	}

	// Monospace: identical advances.
	if a, b := e.Measure(mono(12), "iii"), e.Measure(mono(12), "MMM"); a != b {
		t.Errorf("mono should be fixed-pitch: iii=%d MMM=%d", a, b)
	}

	// The TUI-era names alias onto the embedded families.
	if a, b := e.Measure(&core.Font{Name: "Monday", Size: 12}, "xyz"), e.Measure(mono(12), "xyz"); a != b {
		t.Errorf("Monday alias broken: %d != %d", a, b)
	}
}

func TestMeasureDeterministic(t *testing.T) {
	e := NewEngine()
	const s = "The quick brown fox jumps over the lazy dog"
	first := e.Measure(sans(12), s)
	for i := 0; i < 3; i++ {
		if again := e.Measure(sans(12), s); again != first {
			t.Fatalf("measurement not deterministic: %d then %d", first, again)
		}
	}
	// Two engines agree (D5: layout must not depend on environment).
	if other := NewEngine().Measure(sans(12), s); other != first {
		t.Fatalf("engines disagree: %d vs %d", first, other)
	}
}

func TestLineHeightScalesWithSize(t *testing.T) {
	e := NewEngine()
	h12 := e.LineHeight(sans(12))
	h24 := e.LineHeight(sans(24))
	if h12 <= 0 || h24 <= 0 {
		t.Fatalf("non-positive line heights: %d %d", h12, h24)
	}
	if h24 <= h12 {
		t.Errorf("line height should grow with size: 12pt=%d 24pt=%d", h12, h24)
	}
}

func TestWrapParagraph(t *testing.T) {
	e := NewEngine()
	const s = "The quick brown fox jumps over the lazy dog and keeps on running"
	width := core.Unit(160)
	sp := e.ShapeParagraph(Paragraph{Text: s, Font: sans(12)}, width)

	if len(sp.Lines) < 2 {
		t.Fatalf("expected wrapping, got %d line(s)", len(sp.Lines))
	}
	pos := 0
	var lastBaseline core.Unit = -1
	for i, line := range sp.Lines {
		if line.Width > width {
			t.Errorf("line %d overflows: %d > %d", i, line.Width, width)
		}
		if line.Runes.Start > pos {
			t.Errorf("line %d skips runes: starts %d, expected <= %d", i, line.Runes.Start, pos)
		}
		pos = line.Runes.End
		if line.Baseline <= lastBaseline {
			t.Errorf("line %d baseline %d not below previous %d", i, line.Baseline, lastBaseline)
		}
		lastBaseline = line.Baseline
	}
	// Every rune is accounted for (trailing whitespace may be trimmed
	// from widths but stays in the rune ranges).
	if pos < len(sp.Text) {
		t.Errorf("lost text: last line ends at %d of %d", pos, len(sp.Text))
	}
	if sp.Height() <= 0 {
		t.Error("paragraph height not positive")
	}
}

func TestBidiRunStructure(t *testing.T) {
	e := NewEngine()
	// Latin, then Hebrew (strongly RTL), then Latin. The embedded Go
	// fonts have no Hebrew glyphs (.notdef renders), but direction
	// resolution is bidi's job, not the font's - the structure must
	// still be correct.
	s := "abc " + "אבג" + " def"
	sp := e.ShapeParagraph(Paragraph{Text: s, Font: sans(12)}, 0)
	if len(sp.Lines) != 1 {
		t.Fatalf("expected one line, got %d", len(sp.Lines))
	}
	line := sp.Lines[0]
	if len(line.Runs) < 3 {
		t.Fatalf("expected >= 3 runs (LTR/RTL/LTR), got %d", len(line.Runs))
	}

	var sawRTL bool
	prevX := core.Unit(-1)
	for _, r := range line.Runs {
		if r.X <= prevX {
			t.Errorf("runs not in visual order: X %d after %d", r.X, prevX)
		}
		prevX = r.X
		if r.RTL {
			sawRTL = true
			// The RTL run must cover the Hebrew range.
			if r.Runes.Start > 4 || r.Runes.End < 7 {
				t.Errorf("RTL run range %+v does not cover Hebrew [4,7)", r.Runes)
			}
		}
	}
	if !sawRTL {
		t.Error("no RTL run resolved for Hebrew text")
	}

	// Base direction auto-detection: Hebrew-first text is RTL.
	rtlFirst := e.ShapeParagraph(Paragraph{Text: "אב abc", Font: sans(12)}, 0)
	latinRun := rtlFirst.Lines[0].runFor(3)
	if latinRun == nil {
		t.Fatal("no run for Latin segment")
	}
	// In an RTL paragraph the logically-later Latin sits to the LEFT
	// of the Hebrew opening.
	hebrewRun := rtlFirst.Lines[0].runFor(0)
	if hebrewRun == nil {
		t.Fatal("no run for Hebrew segment")
	}
	if latinRun.X >= hebrewRun.X {
		t.Errorf("RTL base: Latin run at X=%d should be left of Hebrew at X=%d", latinRun.X, hebrewRun.X)
	}
}

func TestCaretLTR(t *testing.T) {
	e := NewEngine()
	sp := e.ShapeParagraph(Paragraph{Text: "abc", Font: sans(12)}, 0)
	line := &sp.Lines[0]

	if x := line.CaretX(0); x != 0 {
		t.Errorf("caret before 'a' at %d, want 0", x)
	}
	prev := core.Unit(-1)
	for i := 0; i <= 3; i++ {
		x := line.CaretX(i)
		if x <= prev {
			t.Errorf("caret not monotonic at %d: %d after %d", i, x, prev)
		}
		prev = x
	}
	if end := line.CaretX(3); end != line.Width {
		t.Errorf("caret after 'c' at %d, want line width %d", end, line.Width)
	}

	// RuneForX inverts CaretX at every boundary.
	for i := 0; i <= 3; i++ {
		if got := line.RuneForX(line.CaretX(i)); got != i {
			t.Errorf("RuneForX(CaretX(%d)) = %d", i, got)
		}
	}
	// Far left / far right clamp to the edges.
	if got := line.RuneForX(-50); got != 0 {
		t.Errorf("far left = %d, want 0", got)
	}
	if got := line.RuneForX(line.Width + 50); got != 3 {
		t.Errorf("far right = %d, want 3", got)
	}
}

func TestCaretRTL(t *testing.T) {
	e := NewEngine()
	sp := e.ShapeParagraph(Paragraph{Text: "אבג", Font: sans(12), Direction: DirectionRTL}, 0)
	line := &sp.Lines[0]

	// Logical start sits at the RIGHT edge, logical end at the left.
	if x := line.CaretX(0); x != line.Width {
		t.Errorf("RTL caret before first rune at %d, want width %d", x, line.Width)
	}
	if x := line.CaretX(3); x != 0 {
		t.Errorf("RTL caret after last rune at %d, want 0", x)
	}
	// Monotonically decreasing.
	prev := line.Width + 1
	for i := 0; i <= 3; i++ {
		x := line.CaretX(i)
		if x >= prev {
			t.Errorf("RTL caret not decreasing at %d: %d after %d", i, x, prev)
		}
		prev = x
	}
	for i := 0; i <= 3; i++ {
		if got := line.RuneForX(line.CaretX(i)); got != i {
			t.Errorf("RTL RuneForX(CaretX(%d)) = %d", i, got)
		}
	}
}

func TestClusterSnapping(t *testing.T) {
	e := NewEngine()
	// 'e' + combining acute forms one cluster (two runes). The caret
	// cannot sit between them: index 1 snaps to the cluster start.
	sp := e.ShapeParagraph(Paragraph{Text: "éx", Font: sans(12)}, 0)
	line := &sp.Lines[0]

	if x0, x1 := line.CaretX(0), line.CaretX(1); x1 != x0 {
		t.Errorf("caret inside combining cluster: CaretX(1)=%d, want snap to CaretX(0)=%d", x1, x0)
	}
	if x2 := line.CaretX(2); x2 <= line.CaretX(0) {
		t.Errorf("caret after cluster should advance: CaretX(2)=%d", x2)
	}
}

func TestSpansSplitRuns(t *testing.T) {
	e := NewEngine()
	sp := e.ShapeParagraph(Paragraph{
		Text:  "plain bold plain",
		Font:  sans(12),
		Spans: []Span{{Start: 6, End: 10, Font: boldSans(12)}},
	}, 0)
	line := sp.Lines[0]
	if len(line.Runs) < 3 {
		t.Fatalf("bold span should split runs: got %d", len(line.Runs))
	}
	// Bold is wider than regular for the same text in the Go family.
	plain := e.Measure(sans(12), "bold")
	bold := e.Measure(boldSans(12), "bold")
	if bold <= plain {
		t.Errorf("bold not wider: %d <= %d", bold, plain)
	}
}

func TestEmptyText(t *testing.T) {
	e := NewEngine()
	sp := e.ShapeParagraph(Paragraph{Text: "", Font: sans(12)}, 0)
	if len(sp.Lines) != 1 {
		t.Fatalf("empty text should yield one empty line, got %d", len(sp.Lines))
	}
	if h := sp.Height(); h <= 0 {
		t.Errorf("empty line still has font height, got %d", h)
	}
	if w := sp.Width(); w != 0 {
		t.Errorf("empty line width = %d", w)
	}
	if x := sp.Lines[0].CaretX(0); x != 0 {
		t.Errorf("caret in empty line at %d", x)
	}
}

func TestShapeParagraphDeterministic(t *testing.T) {
	e := NewEngine()
	p := Paragraph{Text: "shape me twice", Font: sans(12)}
	a := e.ShapeParagraph(p, 100)
	b := e.ShapeParagraph(p, 100)
	if !reflect.DeepEqual(a, b) {
		t.Error("identical inputs shaped differently")
	}
}

func TestRenderProducesInk(t *testing.T) {
	e := NewEngine()
	sp := e.ShapeParagraph(Paragraph{Text: "Hello, text engine", Font: sans(12)}, 0)

	for _, scale := range []int{1, 2} {
		img := image.NewRGBA(image.Rect(0, 0, int(sp.Width())*scale+4, int(sp.Height())*scale+4))
		Render(img, sp, 0, 0, float64(scale), color.RGBA{255, 255, 255, 255})

		ink := 0
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					ink++
				}
			}
		}
		if ink < 50*scale {
			t.Errorf("scale %d: almost no ink (%d px)", scale, ink)
		}
	}
}

func TestRegisterFontExtendsFallback(t *testing.T) {
	e := NewEngine()
	// Unknown family falls back to the default sans.
	if a, b := e.Measure(&core.Font{Name: "NoSuchFamily", Size: 12}, "abc"), e.Measure(sans(12), "abc"); a != b {
		t.Errorf("unknown family should resolve to default: %d != %d", a, b)
	}
}

func TestShapeCacheReusesAndInvalidates(t *testing.T) {
	e := NewEngine()
	a := e.ShapeRun(sans(12), "cached string")
	b := e.ShapeRun(sans(12), "cached string")
	if a != b {
		t.Error("identical spanless requests should return the cached shape")
	}

	// Registering a font may change fallback resolution: cache flushes.
	epoch := e.Epoch()
	if err := e.RegisterFont("Extra", Aspect{}, gomonoTTFForTest()); err != nil {
		t.Fatal(err)
	}
	if e.Epoch() == epoch {
		t.Error("epoch should advance on RegisterFont")
	}
	c := e.ShapeRun(sans(12), "cached string")
	if c == a {
		t.Error("cache should be invalidated by RegisterFont")
	}
	if c.Width() != a.Width() {
		t.Errorf("reshape changed measurement: %d != %d", c.Width(), a.Width())
	}
}

func gomonoTTFForTest() []byte { return gomono.TTF }
