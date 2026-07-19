package trinkets

import (
	"reflect"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

func TestWrapTextMondayWordBoundaries(t *testing.T) {
	// Monday: every character is 8 units. 80 units = 10 characters.
	got := wrapText("hello world again", 80, core.FontMonday12)
	want := []string{"hello", "world", "again"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapTextMondayFitsTwoWords(t *testing.T) {
	// "to be" = 5 chars = 40 units, fits in 48.
	got := wrapText("to be or", 48, core.FontMonday12)
	want := []string{"to be", "or"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapTextTuesdayMeasuresDoubleWidth(t *testing.T) {
	// Tuesday: letters are 16 units, space is 8. "hello world" needs
	// 80 + 8 + 80 = 168 units; at 160 it must break between the words.
	got := wrapText("hello world", 160, core.FontTuesday12)
	want := []string{"hello", "world"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	// The same call in Monday (88 units total) fits on one line at 160.
	got = wrapText("hello world", 160, core.FontMonday12)
	want = []string{"hello world"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Monday control: got %q, want %q", got, want)
	}
}

func TestWrapTextBreaksOverlongWordByCharacters(t *testing.T) {
	// Monday, 40 units = 5 characters per line.
	got := wrapText("abcdefghijkl", 40, core.FontMonday12)
	want := []string{"abcde", "fghij", "kl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapTextPreservesExplicitNewlines(t *testing.T) {
	got := wrapText("alpha\n\nbeta", 800, core.FontMonday12)
	want := []string{"alpha", "", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapTextZeroWidth(t *testing.T) {
	if got := wrapText("anything", 0, core.FontMonday12); got != nil {
		t.Errorf("got %q, want nil", got)
	}
}

func TestLabelHeightForWidth(t *testing.T) {
	l := NewLabel("hello world again")

	if l.HasHeightForWidth() {
		t.Fatal("HasHeightForWidth should be false without word wrap")
	}
	l.SetWordWrap(true)
	if !l.HasHeightForWidth() {
		t.Fatal("HasHeightForWidth should be true with word wrap")
	}

	// Monday at 160 units: "hello world again" fits on one line (136).
	if got := l.HeightForWidth(160); got != 16 {
		t.Errorf("Monday at 160: got %d, want 16", got)
	}
	// Monday at 80 units: wraps to three lines.
	if got := l.HeightForWidth(80); got != 48 {
		t.Errorf("Monday at 80: got %d, want 48", got)
	}

	// Tuesday at 160 units: letters are double width, three lines.
	l.SetFont(core.FontTuesday12)
	if got := l.HeightForWidth(160); got != 48 {
		t.Errorf("Tuesday at 160: got %d, want 48", got)
	}
}

func TestCheckboxHeightForWidth(t *testing.T) {
	c := NewCheckbox("hello world")

	if c.HasHeightForWidth() {
		t.Fatal("HasHeightForWidth should be false without word wrap")
	}
	c.SetWordWrap(true)

	// Indicator chrome is 4 cells (32 units). At width 120 the text
	// area is 88 units: "hello world" (88) fits on one line.
	if got := c.HeightForWidth(120); got != 16 {
		t.Errorf("at 120: got %d, want 16", got)
	}
	// At width 112 the text area is 80 units: wraps to two lines.
	if got := c.HeightForWidth(112); got != 32 {
		t.Errorf("at 112: got %d, want 32", got)
	}
}

func TestEffectiveCellMetricsInheritance(t *testing.T) {
	l := NewLabel("hi")
	p := NewPanel()
	p.AddChild(l)

	// No override anywhere: default 8x16.
	if got := l.EffectiveCellMetrics(); got != core.DefaultCellMetrics() {
		t.Errorf("default: got %+v", got)
	}

	// Container override is inherited by the child.
	dense := core.CellMetrics{CellWidth: 8, CellHeight: 32}
	p.SetCellMetrics(&dense)
	if got := l.EffectiveCellMetrics(); got != dense {
		t.Errorf("inherited: got %+v, want %+v", got, dense)
	}

	// The child's SizeHint follows the inherited grid (row height 32).
	if got := l.SizeHint().Height; got != 32 {
		t.Errorf("SizeHint height: got %d, want 32", got)
	}

	// A trinket-level override wins over the container's.
	own := core.CellMetrics{CellWidth: 16, CellHeight: 16}
	l.SetCellMetrics(&own)
	if got := l.EffectiveCellMetrics(); got != own {
		t.Errorf("own override: got %+v, want %+v", got, own)
	}

	// Clearing the override restores inheritance.
	l.SetCellMetrics(nil)
	if got := l.EffectiveCellMetrics(); got != dense {
		t.Errorf("cleared: got %+v, want %+v", got, dense)
	}
}

func TestDenominationInvariance(t *testing.T) {
	c := NewCheckbox("hi")
	p := NewPanel()
	p.AddChild(c)
	p.SetLayoutManager(layout.NewBoxLayout(core.Vertical))

	// Baseline: one row, expressed in the default 16-unit denomination.
	base := p.SizeHint().Height
	if base != 16 {
		t.Fatalf("baseline: got %d, want 16", base)
	}

	// Re-denominate the panel's interior: one row = 32 units. The
	// checkbox still occupies exactly one row, so the panel's hint in
	// its OUTER currency must not change — re-denomination is visually
	// invariant; only the numbers inside the panel change meaning.
	dense := core.CellMetrics{CellWidth: 8, CellHeight: 32}
	p.SetCellMetrics(&dense)

	if got := c.SizeHint().Height; got != 32 {
		t.Errorf("interior hint: got %d, want 32 (one 32-unit row)", got)
	}
	if got := p.SizeHint().Height; got != base {
		t.Errorf("outer hint changed under re-denomination: got %d, want %d", got, base)
	}
}

func TestPanelPropagatesHeightForWidth(t *testing.T) {
	l := NewLabel("hello world again")
	l.SetWordWrap(true)

	p := NewPanel()
	p.AddChild(l)
	p.SetLayoutManager(layout.NewBoxLayout(core.Vertical))

	if !p.HasHeightForWidth() {
		t.Fatal("panel should propagate HasHeightForWidth from wrapped label")
	}

	// Labels are inline trinkets: a vertical box insets them one cell
	// (8 units) per side, so at panel width 96 the label gets 80 and
	// wraps to three lines.
	if got := p.HeightForWidth(96); got != 48 {
		t.Errorf("got %d, want 48", got)
	}
}
