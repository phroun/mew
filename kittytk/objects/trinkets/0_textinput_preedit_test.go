//go:build sdl

package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// preeditBackend records what the composition overstrike actually draws:
// the clipped re-color (which carries the color the composition is shown
// in) and the pixel fills (the underline rules and the caret bar).
type preeditBackend struct {
	*raster.Backend
	runs    []preeditRun
	clipped []preeditRun
	fills   []preeditFill
}

type preeditRun struct {
	xPx  int
	s    string
	fg   style.Color
	attr style.TextStyle
}

type preeditFill struct {
	xPx, yPx, wPx, hPx int
	bg                 style.Color
}

func (b *preeditBackend) DrawTextPx(xPx, yPx int, s string, st style.CellStyle, f *core.Font) int {
	adv := b.Backend.DrawTextPx(xPx, yPx, s, st, f)
	if s != "" {
		b.runs = append(b.runs, preeditRun{xPx: xPx, s: s, fg: st.Fg, attr: st.Attrs})
	}
	return adv
}

func (b *preeditBackend) DrawTextPxClipped(xPx, yPx int, s string, st style.CellStyle, f *core.Font, clip0, clip1 int) int {
	adv := b.Backend.DrawTextPxClipped(xPx, yPx, s, st, f, clip0, clip1)
	if s != "" {
		b.clipped = append(b.clipped, preeditRun{xPx: clip0, s: s, fg: st.Fg, attr: st.Attrs})
	}
	return adv
}

func (b *preeditBackend) FillRectPx(xPx, yPx, wPx, hPx int, st style.CellStyle) {
	b.fills = append(b.fills, preeditFill{xPx: xPx, yPx: yPx, wPx: wPx, hPx: hPx, bg: st.Bg})
	b.Backend.FillRectPx(xPx, yPx, wPx, hPx, st)
}

// composeFixture is a focused text field over a real raster backend,
// repaintable so a test can check what changed after an event.
type composeFixture struct {
	ti  *TextInput
	b   *raster.Backend
	rec *preeditBackend
	p   *core.Painter
}

// repaint draws the field again into a fresh recorder.
func (f *composeFixture) repaint() *preeditBackend {
	rec := &preeditBackend{Backend: f.b}
	f.b.Clear(style.DefaultStyle())
	f.p = core.NewPainter(rec)
	f.ti.Paint(f.p)
	f.rec = rec
	return rec
}

// composingInput builds a focused text field with a live composition and
// paints it once.
func composingInput(t *testing.T, text string, cursor int, ev core.TextEditingEvent) *composeFixture {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.NewScaled(800, 40, 2)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(10)
	core.SetTextMeasurer(b)

	ti := NewTextInput()
	ti.SetText(text)
	ti.SetBounds(core.UnitRect{Width: 600, Height: b.LineHeight(ti.EffectiveFont())})
	ti.SetFocus()
	ti.SetCursorPosition(cursor)
	if !ti.HandleTextEditing(ev) {
		t.Fatal("the field refused the composition")
	}

	f := &composeFixture{ti: ti, b: b}
	f.repaint()
	return f
}

// A composition is NOT the field's text. It paints, but Text() must keep
// reporting only what has been committed - anything reading the field
// mid-composition (a validator, an onTextChanged consumer, the
// accessibility layer) would otherwise see characters the user has not
// agreed to yet.
func TestCompositionIsNotCommittedText(t *testing.T) {
	f := composingInput(t, "ab", 1, core.TextEditingEvent{Text: "きょう", Start: -1, Length: -1})
	if got := f.ti.Text(); got != "ab" {
		t.Errorf("Text() = %q during composition, want the committed %q", got, "ab")
	}
}

// The composition paints spliced into the run at the caret, as ONE run
// with the committed text - so it shapes with its neighbours exactly as
// it will once it commits, instead of jumping when it does.
func TestCompositionPaintsAtTheCaretInOneRun(t *testing.T) {
	rec := composingInput(t, "ab", 1, core.TextEditingEvent{Text: "xyz", Start: -1, Length: -1}).rec
	if len(rec.runs) != 1 {
		t.Fatalf("%d text runs, want the text and its composition as one: %+v", len(rec.runs), rec.runs)
	}
	if want := "axyzb"; rec.runs[0].s != want {
		t.Errorf("painted %q, want %q (composition spliced at the caret)", rec.runs[0].s, want)
	}
}

// activeColor is what a composition with no clause is painted in: ALL of it is
// the material the input method is working on, so all of it wears the active
// color. Only a composition that reports a clause has anything to dim.
func activeColor(t *testing.T) style.Color {
	t.Helper()
	return NewTextInput().GetScheme().GetFocusedEditBoxIMEActiveClause().Fg
}

// The user-visible point of the whole feature: composed text is shown in
// its own color, not the text color, because it belongs to the input
// method rather than to the document.
func TestCompositionPaintsInItsOwnColor(t *testing.T) {
	f := composingInput(t, "ab", 1, core.TextEditingEvent{Text: "xyz", Start: -1, Length: -1})
	rec := f.rec
	composed := activeColor(t)
	textColor := rec.runs[0].fg

	if composed == textColor {
		t.Skip("this scheme composes in the text color; nothing to tell apart")
	}
	var found bool
	for _, r := range rec.clipped {
		if r.fg == composed {
			found = true
		}
	}
	if !found {
		t.Errorf("no composition run in the composing color %v; clipped runs: %+v",
			composed, rec.clipped)
	}
}

// And the other half of the convention: an underline under the composed
// span, in that same color. Two signals rather than one, since color alone
// is a poor sole cue.
func TestCompositionIsUnderlined(t *testing.T) {
	f := composingInput(t, "ab", 1, core.TextEditingEvent{Text: "xyz", Start: -1, Length: -1})

	if countRules(f, activeColor(t)) == 0 {
		t.Errorf("no underline rule under the composition; fills: %+v", f.rec.fills)
	}
}

// countRules counts the underline rules drawn in one color: a wide, thin
// fill near the bottom of the line, as opposed to the caret, which is a
// tall thin bar.
func countRules(f *composeFixture, c style.Color) int {
	p := f.p
	lineH := p.UnitSpanPxY(0, f.ti.EffectiveCellMetrics().UnitsPerCellHeight)
	n := 0
	for _, fill := range f.rec.fills {
		if fill.bg == c && fill.hPx <= p.DeviceScale() && fill.wPx > fill.hPx &&
			fill.yPx >= lineH/2 {
			n++
		}
	}
	return n
}

// Reporting a clause is what DIMS the rest of the composition.
//
// A Japanese candidate list converts one clause at a time and leaves the
// others as they were typed, so those need telling apart from the segment the
// candidate keys are acting on. Nothing else reports a clause at all, and for
// everything else the whole composition is the active material and is painted
// as such.
func TestAClauseIsWhatDimsTheRestOfTheComposition(t *testing.T) {
	scheme := NewTextInput().GetScheme()
	active, dimmed := scheme.GetFocusedEditBoxIMEActiveClause().Fg, scheme.GetFocusedEditBoxIMEInactive().Fg
	if active == dimmed {
		t.Skip("this scheme paints a clause and the rest alike")
	}
	hasRun := func(f *composeFixture, c style.Color) bool {
		for _, r := range f.rec.clipped {
			if r.fg == c {
				return true
			}
		}
		return false
	}

	// A composition that names no clause is ALL the active span, so it wears
	// the active treatment whole - the color and the thick rule alike, drawn
	// exactly as a clause is.
	plain := composingInput(t, "", 0, core.TextEditingEvent{Text: "きょうは", Start: -1, Length: -1})
	if hasRun(plain, dimmed) {
		t.Errorf("part of a clauseless composition was dimmed: %+v", plain.rec.clipped)
	}
	if got := countRules(plain, active); got != 2 {
		t.Errorf("%d rules under a clauseless composition, want the two that "+
			"make the thick one: %+v", got, plain.rec.fills)
	}

	clause := composingInput(t, "", 0, core.TextEditingEvent{Text: "きょうは", Start: 0, Length: 2})
	if !hasRun(clause, dimmed) {
		t.Errorf("the clauses not being converted were not dimmed: %+v", clause.rec.clipped)
	}
	if !hasRun(clause, active) {
		t.Errorf("no clause run in %v; clipped runs: %+v", active, clause.rec.clipped)
	}
	// Two of them, stacked: that IS the thicker rule, drawn as two thin ones
	// so it shares the composition's baseline.
	if got := countRules(clause, active); got != 2 {
		t.Errorf("%d clause rules, want the two that make the thick one: %+v",
			got, clause.rec.fills)
	}
}

// Committing ends the composition. The commit arrives as ordinary typed
// characters, so the field must not need the separate end-of-composition
// event to stop painting the preedit - platforms send the two in either
// order, and the overlap would double the text on screen.
func TestCommitEndsTheComposition(t *testing.T) {
	f := composingInput(t, "ab", 1, core.TextEditingEvent{Text: "きょう", Start: -1, Length: -1})
	f.ti.HandleKeyPress(core.KeyPressEvent{Key: "今", Text: "今"})

	if got := f.ti.Text(); got != "a今b" {
		t.Errorf("Text() = %q after commit, want %q", got, "a今b")
	}
	for _, r := range f.repaint().runs {
		if strings.Contains(r.s, "きょう") {
			t.Errorf("the composition is still painted after committing: %q", r.s)
		}
	}
}

// An empty update ends the composition, which is also how a cancelled
// one arrives (Escape during conversion).
func TestEmptyUpdateCancelsTheComposition(t *testing.T) {
	f := composingInput(t, "ab", 1, core.TextEditingEvent{Text: "きょう", Start: -1, Length: -1})
	f.ti.HandleTextEditing(core.TextEditingEvent{Text: "", Start: -1, Length: -1})

	rec := f.repaint()
	if len(rec.runs) != 1 || rec.runs[0].s != "ab" {
		t.Errorf("painted %+v after cancelling, want just the committed \"ab\"", rec.runs)
	}
}

// A read-only field declines rather than showing text it could never
// accept. The caller is then free to drop it.
func TestReadOnlyFieldDeclinesCompositions(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("locked")
	ti.SetReadOnly(true)
	if ti.HandleTextEditing(core.TextEditingEvent{Text: "きょう"}) {
		t.Error("a read-only field accepted a composition")
	}
}

// A password field masks the composition too. Masking only what has
// already been committed would show the next word in the clear for as
// long as it took to compose it - which for a CJK input method is the
// whole word.
func TestPasswordFieldMasksTheComposition(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.NewScaled(800, 40, 2)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(10)
	core.SetTextMeasurer(b)

	ti := NewTextInput()
	ti.SetEchoMode(EchoPassword)
	ti.SetText("ab")
	ti.SetBounds(core.UnitRect{Width: 600, Height: b.LineHeight(ti.EffectiveFont())})
	ti.SetFocus()
	ti.SetCursorPosition(2)
	ti.HandleTextEditing(core.TextEditingEvent{Text: "ひみつ", Start: -1, Length: -1})

	rec := &preeditBackend{Backend: b}
	b.Clear(style.DefaultStyle())
	ti.Paint(core.NewPainter(rec))
	for _, r := range rec.runs {
		if strings.ContainsAny(r.s, "ひみつ") {
			t.Errorf("the composition painted unmasked in a password field: %q", r.s)
		}
	}
}

// Focus leaving abandons the composition: the input method will start a
// fresh one wherever typing resumes, and provisional characters left
// behind in an unfocused field read as committed text.
func TestFocusOutDropsTheComposition(t *testing.T) {
	f := composingInput(t, "ab", 1, core.TextEditingEvent{Text: "きょう", Start: -1, Length: -1})
	f.ti.HandleFocusOut()

	for _, r := range f.repaint().runs {
		if strings.Contains(r.s, "きょう") {
			t.Errorf("the composition survived focus loss: %q", r.s)
		}
	}
}
