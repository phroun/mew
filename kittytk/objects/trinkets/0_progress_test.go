package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// textPass is one drawing of the bar's text: what was drawn, where, in which
// style, and through which clip.
type textPass struct {
	x     core.Unit
	text  string
	style style.CellStyle
	clip  core.UnitRect
}

// passRecorder remembers every DrawText and the clip in force when it ran.
type passRecorder struct {
	core.RenderBackend
	clip   core.UnitRect
	passes []textPass
}

func (r *passRecorder) SetClip(c core.UnitRect) {
	r.clip = c
	r.RenderBackend.SetClip(c)
}

func (r *passRecorder) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	r.passes = append(r.passes, textPass{x, text, s, r.clip})
	return r.RenderBackend.DrawText(x, y, text, s, font)
}

// barWidth is the bar the tests paint: 40 cells.
var barWidth = core.Unit(40) * core.DefaultCellMetrics().UnitsPerCellWidth

// paintBar paints a bar at value 42 after setup, and returns what its text
// passes drew.
func paintBar(t *testing.T, setup func(p *ProgressBar)) (*ProgressBar, []textPass) {
	t.Helper()
	ink := newInk(t)
	rec := &passRecorder{RenderBackend: ink}
	p := NewProgressBar()
	p.SetValue(42)
	setup(p)
	p.SetBounds(core.UnitRect{Width: barWidth, Height: 16})
	p.Paint(core.NewPainter(rec))
	return p, rec.passes
}

// shownText is the one string every pass drew, or "" when nothing was drawn.
func shownText(t *testing.T, passes []textPass) string {
	t.Helper()
	if len(passes) == 0 {
		return ""
	}
	for _, ps := range passes[1:] {
		if ps.text != passes[0].text || ps.x != passes[0].x {
			t.Fatalf("the passes drew different things: %+v", passes)
		}
	}
	return passes[0].text
}

// With no caption the bar shows its percentage, as it always has.
func TestAnEmptyCaptionShowsThePercentage(t *testing.T) {
	if _, passes := paintBar(t, func(*ProgressBar) {}); shownText(t, passes) != "42%" {
		t.Errorf("an uncaptioned bar at 42 shows %q, want %q", shownText(t, passes), "42%")
	}
}

// A caption is shown exactly as written. Nothing in it is a code: an
// application that wants "2 / 250" sends that string.
func TestACaptionIsShownAsWritten(t *testing.T) {
	for _, caption := range []string{"Downloading", "%v / %m", "100%% done", "%p%"} {
		if _, passes := paintBar(t, func(p *ProgressBar) { p.SetCaption(caption) }); shownText(t, passes) != caption {
			t.Errorf("caption %q is shown as %q", caption, shownText(t, passes))
		}
	}
}

// The text goes to the text engine as one run, marks that ride a letter and
// wide characters included, so it is shaped as text and not cell by cell.
func TestACaptionReachesTheTextEngineWhole(t *testing.T) {
	const caption = "e\u0301t\u00e9 日本 שלום"
	if _, passes := paintBar(t, func(p *ProgressBar) { p.SetCaption(caption) }); shownText(t, passes) != caption {
		t.Errorf("drew %q, want the whole caption %q", shownText(t, passes), caption)
	}
}

// The text is centred over the bar, to within the cell it is snapped to.
func TestACaptionIsCentred(t *testing.T) {
	p, passes := paintBar(t, func(p *ProgressBar) { p.SetCaption("Fetching…") })
	text := shownText(t, passes)
	w := p.MeasureText(p.CellRun(text))
	mid := passes[0].x + w/2
	if d := mid - barWidth/2; d > core.DefaultCellMetrics().UnitsPerCellWidth || -d > core.DefaultCellMetrics().UnitsPerCellWidth {
		t.Errorf("the caption's middle is at x=%d, the bar's at %d", mid, barWidth/2)
	}
}

// The text is drawn twice, once in each colour, and each copy is clipped to
// its side of the fill's edge: the filled colour over the fill, the empty
// colour over the rest. The edge sits where the fill ends, on the leading
// side the bar reads from.
func TestTheTextChangesColourAtTheFillsEdge(t *testing.T) {
	scheme := NewProgressBar().GetScheme()
	full, empty := scheme.GetProgressFullText(), scheme.GetProgressEmptyText()
	edge := core.Unit(40*42/100) * core.DefaultCellMetrics().UnitsPerCellWidth
	for _, tc := range []struct {
		dir                              core.Direction
		fillX0, fillX1, emptyX0, emptyX1 core.Unit
	}{
		{core.DirLTR, 0, edge, edge, barWidth},
		{core.DirRTL, barWidth - edge, barWidth, 0, barWidth - edge},
	} {
		_, passes := paintBar(t, func(p *ProgressBar) { p.SetDirection(tc.dir) })
		if len(passes) != 2 {
			t.Fatalf("%v: %d text passes, want 2", tc.dir, len(passes))
		}
		for _, ps := range passes {
			x0, x1 := ps.clip.X, ps.clip.X+ps.clip.Width
			switch ps.style {
			case full:
				if x0 != tc.fillX0 || x1 != tc.fillX1 {
					t.Errorf("%v: the filled-colour copy is clipped to %d..%d, want the fill %d..%d", tc.dir, x0, x1, tc.fillX0, tc.fillX1)
				}
			case empty:
				if x0 != tc.emptyX0 || x1 != tc.emptyX1 {
					t.Errorf("%v: the empty-colour copy is clipped to %d..%d, want %d..%d", tc.dir, x0, x1, tc.emptyX0, tc.emptyX1)
				}
			default:
				t.Errorf("%v: a pass drew in neither text colour", tc.dir)
			}
		}
	}
}

// An empty bar draws only the empty colour and a full one only the filled
// colour: there is no other side to slice off.
func TestAnEmptyOrFullBarDrawsOneColour(t *testing.T) {
	scheme := NewProgressBar().GetScheme()
	for _, tc := range []struct {
		value int
		want  style.CellStyle
	}{{0, scheme.GetProgressEmptyText()}, {100, scheme.GetProgressFullText()}} {
		_, passes := paintBar(t, func(p *ProgressBar) { p.SetValue(tc.value) })
		if len(passes) != 1 || passes[0].style != tc.want {
			t.Errorf("at %d%%: %d passes, want one in the one colour there is", tc.value, len(passes))
		}
	}
}

// A caption longer than the bar is cut to fit and says so, rather than
// running past the bar's end.
func TestALongCaptionIsElidedToTheBar(t *testing.T) {
	long := strings.Repeat("abcdefghij", 10)
	p, passes := paintBar(t, func(p *ProgressBar) { p.SetCaption(long) })
	text := shownText(t, passes)
	if !strings.Contains(text, core.Ellipsis) {
		t.Errorf("a cut caption shows %q with no ellipsis", text)
	}
	if w := p.MeasureText(p.CellRun(text)); w > barWidth {
		t.Errorf("the cut caption is %d wide on a %d-wide bar", w, barWidth)
	}
}

// Turning the text off hides the caption and the percentage alike.
func TestTextVisibleOffShowsNoText(t *testing.T) {
	for _, caption := range []string{"", "Downloading"} {
		_, passes := paintBar(t, func(p *ProgressBar) {
			p.SetCaption(caption)
			p.SetTextVisible(false)
		})
		if len(passes) != 0 {
			t.Errorf("caption %q with text off still drew %q", caption, shownText(t, passes))
		}
	}
}

// The accessible value is the percentage whatever the caption says, and the
// range is reported as numbers.
func TestAProgressBarReportsItsValueAndRangeAsNumbers(t *testing.T) {
	p := NewProgressBar()
	p.SetMaximum(250)
	p.SetValue(125)
	p.SetCaption("125 / 250")
	info := p.AccessibleInfo()
	if info.Value != "50%" {
		t.Errorf("accessible value %q, want %q", info.Value, "50%")
	}
	if info.ValueMin != "0" || info.ValueMax != "250" {
		t.Errorf("accessible range %q..%q, want 0..250", info.ValueMin, info.ValueMax)
	}
}

// caption and text_visible reach the bar over the wire, and text_visible is
// on unless a client turns it off.
func TestCaptionAndTextVisibleArriveOverTheWire(t *testing.T) {
	build := func(src string) *ProgressBar {
		t.Helper()
		f := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
		script, err := protocol.Parse(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := protocol.NewSession().Execute(script, f); err != nil {
			t.Fatalf("execute %q: %v", src, err)
		}
		for _, target := range f.targets {
			if p, ok := target.(*ProgressBar); ok {
				return p
			}
		}
		t.Fatalf("%q built no progress bar", src)
		return nil
	}
	p := build(`new progress value=10 caption="%v of %m"`)
	if p.Caption() != "%v of %m" || !p.IsTextVisible() {
		t.Errorf("caption %q, text visible %v; want the caption as sent and text on", p.Caption(), p.IsTextVisible())
	}
	if p := build(`new progress !text_visible`); p.IsTextVisible() {
		t.Error("!text_visible left the text on")
	}
}
