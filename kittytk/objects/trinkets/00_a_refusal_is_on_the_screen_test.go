package trinkets

// What a view does with a refusal it was handed.
//
// A view that was refused and drew an empty area said nothing, and an empty area is
// what a source with nothing in it looks like. So the refusal is DRAWN: one line in
// the theme's ErrorMessage colour, taken out of the rows' own area rather than laid
// over the first row, and taken back the moment an answer arrives.
//
// And it is a default, not a policy. A caller with somewhere better to put a refusal
// says so and the line goes away, which is the whole of the discoverability path: a
// programmer meets the refusal on the screen and decides then whether to handle it.

import (
	"errors"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/serval"
)

// sulkySource opens and then refuses what it is asked, which is a scope turned down
// mid-answer: the reason arrives on the answer's own ending.
type sulkySource struct {
	why    string
	rows   int
	reads  int
	then   *serval.ListSource // answered from instead, once it relents
	relent bool
}

func (s *sulkySource) Open(descriptor *serval.DataSetDescriptor) (serval.DataSet, error) {
	return &sulkySet{src: s}, nil
}

type sulkySet struct {
	src   *sulkySource
	child serval.DataSet
}

func (v *sulkySet) Close() {
	if v.child != nil {
		v.child.Close()
	}
}

func (v *sulkySet) RecordCount() serval.RecordCount { return serval.Exactly(v.src.rows) }

func (v *sulkySet) Read(sc *serval.Scope, out serval.Sink) error {
	v.src.reads++
	if v.src.relent && v.src.then != nil {
		if v.child == nil {
			set, err := v.src.then.Open(nil)
			if err != nil {
				return err
			}
			v.child = set
		}
		return v.child.Read(sc, out)
	}
	out.Ordered()
	out.Done(serval.Complete{Error: v.src.why, Total: serval.Exactly(v.src.rows)})
	return nil
}

// shutSource will not state a sequence at all, which is what a name nothing serves
// comes back as: the refusal is the `Open` call's own return.
type shutSource struct{ why string }

func (s *shutSource) Open(descriptor *serval.DataSetDescriptor) (serval.DataSet, error) {
	return nil, errors.New(s.why)
}

// inked is one mark a view put on a surface: what was drawn, where, and in what.
type inked struct {
	at   core.UnitRect
	what string
	s    style.CellStyle
}

// troubleTape records the fills and the aligned text a paint puts down, which is
// between them the whole of a refusal line.
type troubleTape struct {
	core.RenderBackend
	fills []inked
	texts []inked
}

func (r *troubleTape) FillRect(rect core.UnitRect, ch rune, s style.CellStyle) {
	r.fills = append(r.fills, inked{at: rect, what: string(ch), s: s})
	r.RenderBackend.FillRect(rect, ch, s)
}

func (r *troubleTape) DrawTextAligned(bounds core.UnitRect, text string, hSide core.HSide,
	vAlign core.VAlign, s style.CellStyle, font *core.Font) {

	r.texts = append(r.texts, inked{at: bounds, what: text, s: s})
	r.RenderBackend.DrawTextAligned(bounds, text, hSide, vAlign, s, font)
}

// tape paints w as it stands and hands back what it drew.
func tape(t *testing.T, w core.Trinket) *troubleTape {
	t.Helper()
	px, err := raster.New(900, 400)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	rec := &troubleTape{RenderBackend: px}
	w.Paint(core.NewPainter(rec))
	return rec
}

// band finds the fill drawn in the scheme's refusal colour, there being only one
// thing on a view painted in it.
func (r *troubleTape) band(scheme *style.Scheme) (inked, bool) {
	for _, f := range r.fills {
		if f.s.Bg == scheme.GetErrorMessageBG() && f.s.Fg == scheme.GetErrorMessageFG() {
			return f, true
		}
	}
	return inked{}, false
}

// said finds the text drawn in that same colour, and where it was put.
func (r *troubleTape) said(scheme *style.Scheme) (inked, bool) {
	for _, m := range r.texts {
		if m.s.Bg == scheme.GetErrorMessageBG() {
			return m, true
		}
	}
	return inked{}, false
}

// readsLike checks that the line says what the source said, in the source's own
// words, cut to the room rather than wrapped or rewritten.
//
// A colour and NO reason is the failure worth naming here: it looks like a refusal
// line from a distance and tells a reader nothing, which is the bug this whole
// mechanism exists to close, drawn in red.
func readsLike(t *testing.T, shown, why string) {
	t.Helper()
	if shown == "" {
		t.Error("the line was drawn empty: a colour and no reason says nothing")
		return
	}
	if !strings.HasPrefix(why, strings.TrimSuffix(shown, core.Ellipsis)) {
		t.Errorf("the line reads %q, which is not what the source said", shown)
	}
}

const rowUnit = core.Unit(2) * cell // one row at the base metrics

// **A refusal is a row of the trinket's own, and it says why.**
func TestARefusalTakesARowAndSaysWhy(t *testing.T) {
	const why = "sort={ name desc } on a column nothing is keyed by"
	l := listOver(t, &sulkySource{why: why, rows: 20}, 8)

	if l.visibleCount() != 8 || l.rowsTop() != 0 {
		t.Fatalf("before anything is asked: %d rows from %d", l.visibleCount(), l.rowsTop())
	}

	// The question goes out, and what comes back is a reason rather than records.
	if l.Item(0) != nil {
		t.Fatal("a refused row answered with a record")
	}
	if got := l.Trouble(); got.Reason != why {
		t.Fatalf("the view holds %+v, want the reason it was given", got)
	}

	// It is a row taken OUT of the rows, not one laid over them: seven rows where
	// there were eight, and the first of them a row lower down.
	if l.visibleCount() != 7 {
		t.Errorf("a refused list shows %d rows of 8, want 7 -- the line takes one",
			l.visibleCount())
	}
	if l.rowsTop() != rowUnit {
		t.Errorf("the rows begin at %d, want %d -- one row down", l.rowsTop(), rowUnit)
	}

	scheme := l.GetScheme()
	marks := tape(t, l)
	line, ok := marks.band(scheme)
	if !ok {
		t.Fatal("nothing was painted in the refusal colour")
	}
	if line.at.Y != 0 || line.at.Height != rowUnit {
		t.Errorf("the line is at y=%d h=%d, want the first row", line.at.Y, line.at.Height)
	}
	if line.at.Width != l.Bounds().Width {
		t.Errorf("the line is %d wide of %d -- it is a ROW", line.at.Width, l.Bounds().Width)
	}
	shown, ok := marks.said(scheme)
	if !ok {
		t.Fatal("nothing was written on the line")
	}
	readsLike(t, shown.what, why)
	// Written ON the line: the words go in the row the colour was put down in, or
	// they are somewhere nobody is looking.
	if shown.at != line.at {
		t.Errorf("the reason was written at %+v and the line is at %+v", shown.at, line.at)
	}
}

// A view whose rows begin a row down PAINTS them a row down: the first row would
// otherwise be drawn under the line and read as missing.
func TestTheRowsAreDrawnBelowTheRefusal(t *testing.T) {
	rows := plainRows(20)
	sulky := &sulkySource{why: "not yet", rows: 20, then: rows}
	l := listOver(t, sulky, 8)
	l.Item(0) // refused

	marks := tape(t, l)
	scheme := l.GetScheme()
	for _, f := range marks.fills {
		if f.s.Bg == scheme.GetErrorMessageBG() {
			continue
		}
		if f.at.Height == rowUnit && f.at.Y == 0 {
			t.Fatalf("a row was drawn over the refusal line (%+v)", f)
		}
	}
}

// **An answer is the refusal no longer being true**, and the row it took comes back.
func TestAnAnswerTakesTheRefusalBack(t *testing.T) {
	rows := plainRows(20)
	sulky := &sulkySource{why: "not yet", rows: 20, then: rows}
	l := listOver(t, sulky, 8)

	l.Item(0)
	if !l.Trouble().Any() {
		t.Fatal("the first question was refused and the view did not notice")
	}

	// The refusal left the spine holding nothing, so the reader asks again -- which
	// is the whole of how it recovers: nothing is retried, the question is simply
	// still outstanding.
	sulky.relent = true
	if got := l.Item(0); got == nil || got.Text != "row00000" {
		t.Fatalf("the answer did not arrive: %v", got)
	}
	if got := l.Trouble(); got.Any() {
		t.Errorf("records arrived and the view still holds %+v", got)
	}
	if l.visibleCount() != 8 || l.rowsTop() != 0 {
		t.Errorf("after the answer: %d rows from %d, want 8 from 0",
			l.visibleCount(), l.rowsTop())
	}
	if marks := tape(t, l); func() bool { _, ok := marks.band(l.GetScheme()); return ok }() {
		t.Error("the line is still painted")
	}
}

// **Somebody else taking the refusal takes the line away.** A view that both drew
// the line and told a handler would be saying it twice, and the caller with a status
// bar put it there to have it in one place.
func TestSomebodyElseTakingItTakesTheLineAway(t *testing.T) {
	const why = "no such source: flies"
	var told []Trouble
	l := listOver(t, &shutSource{why: why}, 8)
	l.SetOnTrouble(func(tr Trouble) { told = append(told, tr) })

	l.Item(0)
	if len(told) != 1 || told[0].Reason != why {
		t.Fatalf("the handler was told %v, want the one refusal", told)
	}
	// A sequence that could not be stated was not asked about a POSITION.
	if told[0].At != -1 {
		t.Errorf("the refusal is about row %d, want -1 -- there is no sequence", told[0].At)
	}

	// The view still holds it, for anyone who asks -- and does not draw it.
	if got := l.Trouble(); got.Reason != why {
		t.Errorf("the view holds %+v, want the refusal it handed on", got)
	}
	if l.visibleCount() != 8 || l.rowsTop() != 0 {
		t.Errorf("a handled refusal took %d units of the rows' area", l.rowsTop())
	}
	if marks := tape(t, l); func() bool { _, ok := marks.band(l.GetScheme()); return ok }() {
		t.Error("the line was drawn as well as handed on")
	}

	// **The same refusal twice is not news.** The view asks again, is refused again,
	// and a handler told every time would be told once a frame.
	l.Item(3)
	l.Item(4)
	if len(told) != 1 {
		t.Errorf("the handler was told %d times about one refusal", len(told))
	}
}

// A press on the line is not a press on the row it stands in front of.
func TestAPressOnTheRefusalLineChoosesNothing(t *testing.T) {
	l := listOver(t, &sulkySource{why: "refused", rows: 20}, 8)
	l.Item(0)
	l.SetCurrentIndex(-1)

	if l.HandleMousePress(core.MousePressEvent{
		X: 4 * cell, Y: rowUnit / 2, Button: core.LeftButton,
	}) {
		t.Error("a press on the refusal line was taken as a press on the list")
	}
	if l.CurrentIndex() != -1 {
		t.Errorf("it chose row %d off a press on the line", l.CurrentIndex())
	}

	// And the row BELOW it is the first row, which is what the line moved down.
	if !l.HandleMousePress(core.MousePressEvent{
		X: 4 * cell, Y: rowUnit + rowUnit/2, Button: core.LeftButton,
	}) {
		t.Fatal("a press on the first row was not taken")
	}
	if l.CurrentIndex() != 0 {
		t.Errorf("the row under the line is row %d, want the first", l.CurrentIndex())
	}
}

// **In a tree the line stands BELOW the header**: the header says what the columns
// are, which is still true, and the refusal says what the rows are not.
func TestARefusalStandsUnderATreesHeader(t *testing.T) {
	const why = "no such source: fils"
	tv := newColumnsTree(60, 10)
	tv.SetSource(&shutSource{why: why})

	tv.rowCount() // the question goes out and is refused
	if got := tv.Trouble(); got.Reason != why {
		t.Fatalf("the tree holds %+v, want the refusal", got)
	}
	header := tv.headerHeight()
	if header == 0 {
		t.Fatal("this tree was built with a header")
	}
	if tv.rowsTop() != header+rowUnit {
		t.Errorf("the rows begin at %d, want %d -- under the header AND the line",
			tv.rowsTop(), header+rowUnit)
	}
	if tv.visibleCount() != 8 {
		t.Errorf("a refused tree shows %d rows of 9, want 8", tv.visibleCount())
	}

	scheme := tv.GetScheme()
	marks := tape(t, tv)
	line, ok := marks.band(scheme)
	if !ok {
		t.Fatal("nothing was painted in the refusal colour")
	}
	if line.at.Y != header || line.at.Height != rowUnit {
		t.Errorf("the line is at y=%d h=%d, want one row at %d",
			line.at.Y, line.at.Height, header)
	}
	shown, ok := marks.said(scheme)
	if !ok {
		t.Fatal("nothing was written on the line")
	}
	readsLike(t, shown.what, why)
	// Written ON the line: the words go in the row the colour was put down in, or
	// they are somewhere nobody is looking.
	if shown.at != line.at {
		t.Errorf("the reason was written at %+v and the line is at %+v", shown.at, line.at)
	}
}

// And a tree's rows are drawn BELOW that line, header and all: a row painted at the
// header's own foot is the row the line stands in front of, drawn over.
func TestATreesRowsAreDrawnBelowTheRefusal(t *testing.T) {
	tv := newColumnsTree(60, 10)
	tv.SetSource(&sulkySource{why: "refused mid-answer", rows: 20})

	if tv.rowCount() == 0 {
		t.Fatal("this tree was meant to know it has rows and not what is in them")
	}
	if !tv.Trouble().Any() {
		t.Fatal("the read was refused and the tree did not notice")
	}
	// The first row is the one chosen, a chosen row being the one row a tree fills
	// right across -- which is what makes it findable on the tape.
	tv.SetCurrentIndex(0)

	scheme := tv.GetScheme()
	marks := tape(t, tv)
	if _, ok := marks.band(scheme); !ok {
		t.Fatal("nothing was painted in the refusal colour")
	}
	header := tv.headerHeight()
	drew := false
	for _, f := range marks.fills {
		if f.s.Bg == scheme.GetErrorMessageBG() || f.at.Height != rowUnit {
			continue
		}
		if f.at.Y == header {
			t.Fatalf("a row was drawn over the refusal line (%+v)", f)
		}
		if f.at.Y == tv.rowsTop() {
			drew = true
		}
	}
	// Something WAS drawn below it, or the check above passes on an empty tree.
	if !drew {
		t.Errorf("no row was drawn at %d, where the rows begin", tv.rowsTop())
	}
}

// **A name nothing serves is a refusal the trinket says itself.**
//
// This is the misspelling the whole mechanism is here for. The statement is refused
// -- so whoever is assembling the bundle is told too -- and until now that was the
// only place it went: a `source:` off by one letter left a window that never filled
// and a complaint nobody read. The trinket a reader is looking at now holds it.
func TestANameNothingServesIsSaidByTheTrinketItself(t *testing.T) {
	for _, c := range []struct {
		what   string
		script string
		hold   func(any) (Trouble, bool)
	}{
		{"listview", `lv=new listview source="source:fils"`, func(v any) (Trouble, bool) {
			l, ok := v.(*ListView)
			if !ok {
				return Trouble{}, false
			}
			return l.Trouble(), true
		}},
		{"treeview", `tv=new treeview source="source:fils"`, func(v any) (Trouble, bool) {
			tv, ok := v.(*TreeView)
			if !ok {
				return Trouble{}, false
			}
			return tv.Trouble(), true
		}},
	} {
		script, err := protocol.Parse(c.script)
		if err != nil {
			t.Fatalf("%s: parse: %v", c.what, err)
		}
		f := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
		if _, err := protocol.NewSession().Execute(script, f); err == nil {
			t.Fatalf("%s: the statement was taken, and nothing serves that name", c.what)
		}
		if len(f.targets) == 0 {
			t.Fatalf("%s: nothing was built", c.what)
		}
		got, ok := c.hold(f.targets[0])
		if !ok {
			t.Fatalf("%s: built a %T", c.what, f.targets[0])
		}
		if !got.Any() {
			t.Errorf("%s: the refusal went up and the trinket holds nothing", c.what)
			continue
		}
		if !strings.Contains(got.Reason, "fils") {
			t.Errorf("%s: it holds %q, which does not name what was asked for",
				c.what, got.Reason)
		}
		// There is no sequence, so there is no position it is about.
		if got.At != -1 {
			t.Errorf("%s: the refusal is about row %d, want -1", c.what, got.At)
		}
	}
}
