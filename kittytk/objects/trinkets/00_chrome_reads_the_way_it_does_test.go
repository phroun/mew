package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/style"
)

// inkRecorder remembers where a trinket put each glyph, so a test can say
// which side of its own box a piece of chrome landed on.
type inkRecorder struct {
	core.RenderBackend
	cells []struct {
		x, y core.Unit
		ch   rune
	}
	texts []struct {
		x, y core.Unit
		s    string
	}
}

func (r *inkRecorder) DrawCell(x, y core.Unit, ch rune, s style.CellStyle) {
	r.cells = append(r.cells, struct {
		x, y core.Unit
		ch   rune
	}{x, y, ch})
	r.RenderBackend.DrawCell(x, y, ch, s)
}

func (r *inkRecorder) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	r.texts = append(r.texts, struct {
		x, y core.Unit
		s    string
	}{x, y, text})
	return r.RenderBackend.DrawText(x, y, text, s, font)
}

// cellAt is where a glyph was drawn, and whether it was drawn at all.
func (r *inkRecorder) cellAt(ch rune) (core.Unit, bool) {
	for _, c := range r.cells {
		if c.ch == ch {
			return c.x, true
		}
	}
	return 0, false
}

// glyphAt is the last glyph drawn on the cell at x, and whether one was.
func (r *inkRecorder) glyphAt(x core.Unit) (rune, bool) {
	ch, ok := rune(0), false
	for _, c := range r.cells {
		if c.x == x {
			ch, ok = c.ch, true
		}
	}
	return ch, ok
}

// textAt is where a string was drawn.
func (r *inkRecorder) textAt(s string) (core.Unit, bool) {
	for _, t := range r.texts {
		if t.s == s {
			return t.x, true
		}
	}
	return 0, false
}

// span is the leftmost and rightmost x a glyph was drawn at.
func (r *inkRecorder) span(ch rune) (lo, hi core.Unit, n int) {
	first := true
	for _, c := range r.cells {
		if c.ch != ch {
			continue
		}
		n++
		if first || c.x < lo {
			lo = c.x
		}
		if first || c.x > hi {
			hi = c.x
		}
		first = false
	}
	return lo, hi, n
}

func newInk(t *testing.T) *inkRecorder {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(600, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)
	return &inkRecorder{RenderBackend: px}
}

// A checkbox's box sits on the side its direction reads from, with the caption
// running away from it. The three cells keep their order inside the group: the
// brackets are a pair, and a pair drawn backwards is "]x[".
func TestACheckboxPutsItsBoxOnTheLeadingSide(t *testing.T) {
	const w = core.Unit(320)

	place := func(d core.Direction) (open, close_, caption core.Unit) {
		ink := newInk(t)
		c := NewCheckbox("Ready")
		c.SetDirection(d)
		c.SetBounds(core.UnitRect{Width: w, Height: 16})
		c.Paint(core.NewPainter(ink))
		o, ok := ink.cellAt('[')
		if !ok {
			t.Fatalf("%v: the checkbox drew no box", d)
		}
		cl, ok := ink.cellAt(']')
		if !ok {
			t.Fatalf("%v: the checkbox drew half a box", d)
		}
		cap, ok := ink.textAt("Ready")
		if !ok {
			t.Fatalf("%v: the checkbox drew no caption", d)
		}
		return o, cl, cap
	}

	open, close_, caption := place(core.DirLTR)
	if open != 0 {
		t.Errorf("left to right: the box opens at x=%d, want the left edge", open)
	}
	if !(open < close_ && close_ < caption) {
		t.Errorf("left to right: [ at %d, ] at %d, caption at %d; want them in that order",
			open, close_, caption)
	}

	open, close_, caption = place(core.DirRTL)
	if open <= caption {
		t.Errorf("right to left: the box opens at x=%d and the caption is at %d; the box should be past it",
			open, caption)
	}
	if open >= close_ {
		t.Errorf("right to left: [ at %d and ] at %d; the pair was drawn backwards", open, close_)
	}
	// Three cells wide, hard against the far edge.
	m := core.DefaultCellMetrics()
	if want := w - m.UnitsPerCellWidth*3; open != want {
		t.Errorf("right to left: the box opens at x=%d, want %d -- three cells short of the far edge", open, want)
	}
}

// A radio button is the same control with round brackets.
func TestARadioButtonPutsItsBoxOnTheLeadingSide(t *testing.T) {
	const w = core.Unit(320)
	ink := newInk(t)
	r := NewRadioButton("Option")
	r.SetDirection(core.DirRTL)
	r.SetBounds(core.UnitRect{Width: w, Height: 16})
	r.Paint(core.NewPainter(ink))

	open, ok := ink.cellAt('(')
	if !ok {
		t.Fatal("the radio button drew no box")
	}
	caption, ok := ink.textAt("Option")
	if !ok {
		t.Fatal("the radio button drew no caption")
	}
	if open <= caption {
		t.Errorf("the box opens at x=%d and the caption is at %d; the box should be past it", open, caption)
	}
	m := core.DefaultCellMetrics()
	if want := w - m.UnitsPerCellWidth*3; open != want {
		t.Errorf("the box opens at x=%d, want %d", open, want)
	}
}

// A bar fills from the leading edge, so it grows the way its direction reads.
func TestAProgressBarFillsFromTheLeadingEdge(t *testing.T) {
	const w = core.Unit(320)

	fill := func(d core.Direction) (lo, hi core.Unit, n int) {
		ink := newInk(t)
		p := NewProgressBar()
		p.SetDirection(d)
		p.SetValue(25)
		p.SetTextVisible(false)
		p.SetBounds(core.UnitRect{Width: w, Height: 16})
		p.Paint(core.NewPainter(ink))
		return ink.span('▓')
	}

	loL, hiL, nL := fill(core.DirLTR)
	if nL == 0 {
		t.Fatal("a quarter-full bar drew no filled cells")
	}
	if loL != 0 {
		t.Errorf("left to right: the fill starts at x=%d, want the left edge", loL)
	}

	loR, hiR, nR := fill(core.DirRTL)
	if nR != nL {
		t.Errorf("the bar fills %d cells one way and %d the other; only the side turns over", nL, nR)
	}
	m := core.DefaultCellMetrics()
	if want := w - m.UnitsPerCellWidth; hiR != want {
		t.Errorf("right to left: the fill ends at x=%d, want the far edge %d", hiR, want)
	}
	if loR <= hiL {
		t.Errorf("right to left: the fill spans %d..%d, which overlaps where it sat the other way (%d..%d)",
			loR, hiR, loL, hiL)
	}
}

// A combobox reads value then arrow from the side its direction begins on, so
// the arrow and the gap beside it change places together.
func TestAComboBoxPutsItsArrowOnTheTrailingSide(t *testing.T) {
	const w = core.Unit(320)

	place := func(d core.Direction) (arrow core.Unit, glyph string) {
		ink := newInk(t)
		c := NewComboBox()
		c.AddItem("Choice")
		c.SetDirection(d)
		c.SetBounds(core.UnitRect{Width: w, Height: 16})
		c.Paint(core.NewPainter(ink))
		for _, want := range []string{" ▼", "▼ "} {
			if x, ok := ink.textAt(want); ok {
				return x, want
			}
		}
		t.Fatalf("%v: the combobox drew no arrow", d)
		return 0, ""
	}

	arrowL, glyphL := place(core.DirLTR)
	if glyphL != " ▼" {
		t.Errorf("left to right: the arrow reads %q; the gap belongs before it", glyphL)
	}
	if arrowL <= w/2 {
		t.Errorf("left to right: the arrow is at x=%d, want the trailing half", arrowL)
	}

	arrowR, glyphR := place(core.DirRTL)
	if glyphR != "▼ " {
		t.Errorf("right to left: the arrow reads %q; the gap belongs after it", glyphR)
	}
	if arrowR != 0 {
		t.Errorf("right to left: the arrow is at x=%d, want the left edge", arrowR)
	}
}

// scrolled builds a scroll area whose content overflows on both axes.
func scrolledArea(t *testing.T, d core.Direction) *ScrollArea {
	t.Helper()
	s := NewScrollArea()
	s.SetDirection(d)
	s.SetBounds(core.UnitRect{Width: 240, Height: 120})
	content := NewPanel()
	content.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	for i := 0; i < 30; i++ {
		content.AddChild(NewLabel("a row long enough to run past the right edge of the viewport"))
	}
	s.SetContent(content)
	s.Layout()
	if !s.needsVScrollBar() {
		t.Fatal("the content did not overflow; there is no bar to look at")
	}
	return s
}

// The vertical bar takes the trailing edge, so the viewport starts a column in
// where the direction reads right to left -- and a press in that column is on
// the bar rather than on the content.
func TestAScrollAreasBarsTakeTheTrailingEdge(t *testing.T) {
	m := core.DefaultCellMetrics()

	ltr := scrolledArea(t, core.DirLTR)
	if got := ltr.viewportBounds().X; got != 0 {
		t.Errorf("left to right: the viewport starts at x=%d, want 0", got)
	}
	if got, want := ltr.vLaneX(), ltr.viewportBounds().Width; got != want {
		t.Errorf("left to right: the lane is at x=%d, want the viewport's far edge %d", got, want)
	}

	rtl := scrolledArea(t, core.DirRTL)
	if got := rtl.vLaneX(); got != 0 {
		t.Errorf("right to left: the lane is at x=%d, want the left edge", got)
	}
	if got := rtl.viewportBounds().X; got != m.UnitsPerCellWidth {
		t.Errorf("right to left: the viewport starts at x=%d, want a column in (%d)",
			got, m.UnitsPerCellWidth)
	}

	// A press in that column reaches the bar -- unscrolled, its thumb is at
	// the top of the track -- and the same press does not in a left-to-right
	// area, where that column is content.
	rtl.HandleMousePress(core.MousePressEvent{X: 1, Y: 4, Button: core.LeftButton})
	if !rtl.vScrollBar.dragging {
		t.Error("right to left: a press on the left column did not reach the vertical bar")
	}
	ltr.HandleMousePress(core.MousePressEvent{X: 1, Y: 4, Button: core.LeftButton})
	if ltr.vScrollBar.dragging {
		t.Error("left to right: a press on the left column reached the vertical bar, which is on the other side")
	}
}

// An area that has not been scrolled across shows the BEGINNING of its
// content, and which end that is follows the direction.
func TestAScrollAreaOpensAtTheStartOfItsContent(t *testing.T) {
	ltr := scrolledArea(t, core.DirLTR)
	if got := ltr.hScrollBar.Value(); got != ltr.hScrollBar.Minimum() {
		t.Errorf("left to right: opens at %d, want the near end %d", got, ltr.hScrollBar.Minimum())
	}

	rtl := scrolledArea(t, core.DirRTL)
	max := rtl.hScrollBar.Maximum()
	if max <= rtl.hScrollBar.Minimum() {
		t.Fatal("the content does not overflow across; there is nothing to seek")
	}
	if got := rtl.hScrollBar.Value(); got != max {
		t.Errorf("right to left: opens at %d, want the far end %d", got, max)
	}

	// Once something has scrolled it across, it stays where it was put.
	rtl.SetScrollX(0)
	rtl.Layout()
	if got := rtl.hScrollBar.Value(); got != 0 {
		t.Errorf("after being scrolled to 0 it seeks back to %d; the start is only sought until it is left", got)
	}
}

// A horizontal splitter's first pane is the one on the leading side, and the
// divider stands its share of the run in from there.
func TestAHorizontalSplitterPutsItsFirstPaneOnTheLeadingSide(t *testing.T) {
	build := func(d core.Direction) (first, second core.UnitRect, divider core.UnitRect) {
		sp := NewSplitter(core.Horizontal)
		sp.SetDirection(d)
		sp.SetFirst(NewPanel())
		sp.SetSecond(NewPanel())
		sp.SetPosition(0.25)
		sp.SetBounds(core.UnitRect{Width: 320, Height: 100})
		sp.Layout()
		f, s := sp.childBounds()
		return f, s, sp.dividerBounds()
	}

	first, second, divider := build(core.DirLTR)
	if first.X != 0 {
		t.Errorf("left to right: the first pane is at x=%d, want the left edge", first.X)
	}
	if !(first.X < divider.X && divider.X < second.X) {
		t.Errorf("left to right: panes at %d and %d with the divider at %d", first.X, second.X, divider.X)
	}
	narrow := first.Width

	first, second, divider = build(core.DirRTL)
	if second.X != 0 {
		t.Errorf("right to left: the second pane is at x=%d, want the left edge", second.X)
	}
	if !(second.X < divider.X && divider.X < first.X) {
		t.Errorf("right to left: panes at %d and %d with the divider at %d", first.X, second.X, divider.X)
	}
	if first.Width != narrow {
		t.Errorf("right to left: the first pane is %d wide against %d the other way; position is its share either way",
			first.Width, narrow)
	}
	if got := first.X + first.Width; got != 320 {
		t.Errorf("right to left: the first pane ends at %d, want the far edge 320", got)
	}
}

// The content stands where the viewport does, and says so in its BOUNDS.
//
// A popup is placed by walking bounds up to the screen (MapToScreen), so a
// content pane that paints a column in but reports x=0 opens every drop-down
// inside it a column off the control it belongs to.
func TestScrolledContentReportsWhereItPaints(t *testing.T) {
	for _, c := range []struct {
		dir  core.Direction
		name string
	}{{core.DirLTR, "left to right"}, {core.DirRTL, "right to left"}} {
		s := scrolledArea(t, c.dir)
		s.Paint(core.NewPainter(newInk(t)))

		viewport := s.viewportBounds()
		if got := s.content.Bounds().X; got != viewport.X {
			t.Errorf("%s: the content reports x=%d but the viewport starts at %d",
				c.name, got, viewport.X)
		}
	}
}

// A control in a right-to-left scroll area is not squeezed by the bar beside
// it: what it draws stays clear of the lane.
func TestAControlInAScrolledColumnClearsTheLane(t *testing.T) {
	ink := newInk(t)
	s := NewScrollArea()
	s.SetDirection(core.DirRTL)
	s.SetBounds(core.UnitRect{Width: 240, Height: 120})
	content := NewPanel()
	content.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	const caption = "Radio option with longer text"
	for i := 0; i < 30; i++ {
		content.AddChild(NewRadioButton(caption))
	}
	s.SetContent(content)
	s.Layout()
	s.Paint(core.NewPainter(ink))

	lane := core.DefaultCellMetrics().UnitsPerCellWidth
	if s.vLaneX() != 0 {
		t.Fatalf("the lane is at x=%d; this test is about the case where it takes the left", s.vLaneX())
	}
	at, ok := ink.textAt(caption)
	if !ok {
		t.Fatal("no caption was drawn")
	}
	if at < lane {
		t.Errorf("a caption is drawn at x=%d, inside the lane that ends at %d", at, lane)
	}
}

// Content narrower than the viewport sits against the LEADING edge.
//
// A scroll area does not stretch its content by default, so a panel that asks
// for less than the area gives leaves room over. That room belongs behind the
// content, on the side the direction ends at -- parking it at the left in both
// directions puts a right-to-left panel against the wrong edge with a gap where
// the eye starts.
func TestNarrowContentSitsAgainstTheLeadingEdge(t *testing.T) {
	build := func(d core.Direction) (*ScrollArea, core.UnitRect) {
		s := NewScrollArea()
		s.SetDirection(d)
		s.SetBounds(core.UnitRect{Width: 400, Height: 120})
		content := NewPanel()
		content.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
		// Narrow, and tall enough to keep the vertical bar in play.
		for i := 0; i < 30; i++ {
			content.AddChild(NewLabel("short"))
		}
		s.SetContent(content)
		s.Layout()
		s.Paint(core.NewPainter(newInk(t)))
		return s, content.Bounds()
	}

	ltr, at := build(core.DirLTR)
	if at.Width >= ltr.viewportBounds().Width {
		t.Fatalf("the content is %d wide against a viewport of %d; there is no room left over to place",
			at.Width, ltr.viewportBounds().Width)
	}
	if got := ltr.viewportBounds().X; at.X != got {
		t.Errorf("left to right: the content starts at x=%d, want the viewport's own %d", at.X, got)
	}

	rtl, at := build(core.DirRTL)
	viewport := rtl.viewportBounds()
	if want := viewport.X + viewport.Width - at.Width; at.X != want {
		t.Errorf("right to left: the content starts at x=%d, want %d -- flush with the far edge",
			at.X, want)
	}
	if got := at.X + at.Width; got != viewport.X+viewport.Width {
		t.Errorf("right to left: the content ends at %d, want the viewport's far edge %d",
			got, viewport.X+viewport.Width)
	}
}

// An area that stretches its content to the viewport has no room left over to
// place, so it does not shift it: the content is already as wide as the room.
func TestAResizedTrinketIsNotShiftedAsWell(t *testing.T) {
	s := NewScrollArea()
	s.SetDirection(core.DirRTL)
	s.SetTrinketResizable(true)
	s.SetBounds(core.UnitRect{Width: 400, Height: 120})
	content := NewPanel()
	content.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	for i := 0; i < 30; i++ {
		content.AddChild(NewLabel("short"))
	}
	s.SetContent(content)
	s.Layout()
	s.Paint(core.NewPainter(newInk(t)))

	viewport := s.viewportBounds()
	at := content.Bounds()
	if at.Width != viewport.Width {
		t.Fatalf("the content is %d wide against a viewport of %d; it was not stretched",
			at.Width, viewport.Width)
	}
	if at.X != viewport.X {
		t.Errorf("the content starts at x=%d, want the viewport's own %d -- there is no slack to place it in",
			at.X, viewport.X)
	}
}
